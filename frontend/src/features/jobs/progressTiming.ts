import { useEffect, useState } from "react";
import type { JobProgress, JobState } from "../../lib/generated/contracts";
import type { JobProgressSample, JobProgressTiming } from "./model";

const FAST_TASK_MS = 1_000;
const ETA_MIN_ELAPSED_MS = 3_000;
const ETA_MIN_PROGRESS = 8;
const MAX_SAMPLES = 8;
const ROLLING_WINDOW_MS = 20_000;
const MIN_PERCENT_DELTA = 0.5;
const MIN_RATE_WINDOW_MS = 250;
const RATE_EMA_ALPHA = 0.35;
const BASE_RECENT_RATE_WEIGHT = 0.72;
const RECENT_RATE_DECAY_START_MS = 4_000;
const RECENT_RATE_DECAY_END_MS = 14_000;
const STALL_HIDE_ETA_MS = 12_000;
const MIN_MEASURED_SAMPLES_FOR_ETA = 2;

export type ProgressDisplayMode =
  | "hidden"
  | "simple"
  | "detailed"
  | "indeterminate";

export type EtaConfidence = "none" | "low" | "medium" | "high";

export interface ProgressPresentation {
  mode: ProgressDisplayMode;
  elapsedMs: number | null;
  remainingMs: number | null;
  percentLabel: string | null;
  etaLabel: string | null;
  detailLabel: string | null;
  showEta: boolean;
  indeterminate: boolean;
  confidence: EtaConfidence;
  stalled: boolean;
}

export interface ProgressSnapshotLike {
  state: JobState;
  progress: JobProgress;
  timing: JobProgressTiming | null;
  fromCache: boolean;
}

interface RateEstimate {
  confidence: EtaConfidence;
  effectiveRate: number | null;
  overallRate: number | null;
  recentRate: number | null;
  recentWeight: number;
  remainingMs: number | null;
  staleMs: number | null;
  stalled: boolean;
}

export function isTerminalJobState(state: JobState): boolean {
  return state === "completed" || state === "failed" || state === "cancelled";
}

export function isPendingJobState(state: JobState): boolean {
  return state === "queued" || state === "running" || state === "cancelling";
}

export function advanceJobProgressTiming(
  previous: JobProgressTiming | null,
  snapshot: Pick<ProgressSnapshotLike, "state" | "progress" | "fromCache">,
  nowMs = Date.now(),
): JobProgressTiming | null {
  if (snapshot.fromCache) {
    return null;
  }

  if (!isPendingJobState(snapshot.state)) {
    return previous
      ? {
          ...previous,
          lastUpdatedAtMs: nowMs,
        }
      : null;
  }

  const percent = clampPercent(snapshot.progress.percent);
  const base = previous ?? {
    startedAtMs: nowMs,
    lastUpdatedAtMs: nowMs,
    lastProgressAtMs: nowMs,
    firstMeasuredSample: percent > 0 ? { atMs: nowMs, percent } : null,
    measuredSampleCount: percent > 0 ? 1 : 0,
    smoothedRate: null,
    samples: [{ atMs: nowMs, percent }],
  };
  const lastSample = base.samples[base.samples.length - 1];

  if (!lastSample) {
    return {
      startedAtMs: base.startedAtMs,
      lastUpdatedAtMs: nowMs,
      lastProgressAtMs: nowMs,
      firstMeasuredSample: percent > 0 ? { atMs: nowMs, percent } : null,
      measuredSampleCount: percent > 0 ? 1 : 0,
      smoothedRate: null,
      samples: [{ atMs: nowMs, percent }],
    };
  }

  if (percent + MIN_PERCENT_DELTA < lastSample.percent) {
    return {
      startedAtMs: base.startedAtMs,
      lastUpdatedAtMs: nowMs,
      lastProgressAtMs: nowMs,
      firstMeasuredSample: percent > 0 ? { atMs: nowMs, percent } : null,
      measuredSampleCount: percent > 0 ? 1 : 0,
      smoothedRate: null,
      samples: [{ atMs: nowMs, percent }],
    };
  }

  if (Math.abs(lastSample.percent - percent) < MIN_PERCENT_DELTA) {
    return {
      ...base,
      lastUpdatedAtMs: nowMs,
    };
  }

  const deltaMs = nowMs - lastSample.atMs;
  const deltaPercent = percent - lastSample.percent;
  const nextSample = { atMs: nowMs, percent };
  const measuredRate =
    lastSample.percent > 0 &&
    deltaMs >= MIN_RATE_WINDOW_MS &&
    deltaPercent >= MIN_PERCENT_DELTA
      ? deltaPercent / deltaMs
      : null;

  return {
    startedAtMs: base.startedAtMs,
    lastUpdatedAtMs: nowMs,
    lastProgressAtMs: nowMs,
    firstMeasuredSample:
      base.firstMeasuredSample ?? (percent > 0 ? nextSample : null),
    measuredSampleCount:
      base.measuredSampleCount + (percent > 0 ? 1 : 0),
    smoothedRate: measuredRate
      ? smoothRate(base.smoothedRate, measuredRate)
      : base.smoothedRate,
    samples: trimSamples([...base.samples, nextSample], nowMs),
  };
}

export function describeProgress(
  snapshot: ProgressSnapshotLike,
  nowMs = Date.now(),
): ProgressPresentation {
  const percent = clampPercent(snapshot.progress.percent);
  const percentLabel =
    percent > 0 && percent < 100 ? `${Math.round(percent)}%` : null;
  const elapsedMs = snapshot.timing
    ? Math.max(0, nowMs - snapshot.timing.startedAtMs)
    : null;

  if (snapshot.fromCache || isTerminalJobState(snapshot.state)) {
    return {
      mode: "simple",
      elapsedMs,
      remainingMs: null,
      percentLabel,
      etaLabel: null,
      detailLabel: null,
      showEta: false,
      indeterminate: false,
      confidence: "none",
      stalled: false,
    };
  }

  if (!isPendingJobState(snapshot.state) || !snapshot.timing) {
    return {
      mode: "indeterminate",
      elapsedMs,
      remainingMs: null,
      percentLabel,
      etaLabel: null,
      detailLabel: percentLabel,
      showEta: false,
      indeterminate: true,
      confidence: "none",
      stalled: false,
    };
  }

  const rateEstimate = estimateRate(snapshot.timing, percent, nowMs);
  const etaLabel = formatActiveEtaLabel(rateEstimate);
  const showEta =
    !rateEstimate.stalled &&
    rateEstimate.remainingMs !== null &&
    rateEstimate.confidence !== "none";
  const mode = resolveDisplayMode(percent, elapsedMs, showEta);
  const detailParts = [
    percentLabel,
    etaLabel,
  ].filter((value): value is string => Boolean(value));

  return {
    mode,
    elapsedMs,
    remainingMs: showEta ? rateEstimate.remainingMs : null,
    percentLabel,
    etaLabel,
    detailLabel: detailParts.join(" • ") || null,
    showEta,
    indeterminate: mode === "indeterminate",
    confidence: rateEstimate.confidence,
    stalled: rateEstimate.stalled,
  };
}

export function useProgressClock(enabled: boolean): number {
  const [nowMs, setNowMs] = useState(() => Date.now());

  useEffect(() => {
    if (!enabled) {
      return;
    }

    setNowMs(Date.now());
    const timer = window.setInterval(() => {
      setNowMs(Date.now());
    }, 1_000);

    return () => {
      window.clearInterval(timer);
    };
  }, [enabled]);

  return nowMs;
}

function estimateRate(
  timing: JobProgressTiming,
  percent: number,
  nowMs: number,
): RateEstimate {
  if (percent <= 0 || percent >= 100) {
    return {
      confidence: "none",
      effectiveRate: null,
      overallRate: null,
      recentRate: null,
      recentWeight: 0,
      remainingMs: null,
      staleMs: null,
      stalled: false,
    };
  }

  const overallRate = calculateOverallRate(timing, percent, nowMs);
  const staleMs = Math.max(0, nowMs - timing.lastProgressAtMs);
  const recentWeight = calculateRecentRateWeight(staleMs, timing.smoothedRate);
  const effectiveRate = blendRates(overallRate, timing.smoothedRate, recentWeight);
  const remainingMs =
    effectiveRate && Number.isFinite(effectiveRate) && effectiveRate > 0
      ? Math.max(0, (100 - percent) / effectiveRate)
      : null;
  const confidence = estimateConfidence(
    timing,
    percent,
    nowMs,
    overallRate,
    timing.smoothedRate,
    effectiveRate,
    staleMs,
    remainingMs,
  );

  return {
    confidence,
    effectiveRate,
    overallRate,
    recentRate: timing.smoothedRate,
    recentWeight,
    remainingMs,
    staleMs,
    stalled: staleMs >= STALL_HIDE_ETA_MS,
  };
}

function estimateConfidence(
  timing: JobProgressTiming,
  percent: number,
  nowMs: number,
  overallRate: number | null,
  recentRate: number | null,
  effectiveRate: number | null,
  staleMs: number,
  remainingMs: number | null,
): EtaConfidence {
  if (effectiveRate === null || remainingMs === null) {
    return "none";
  }

  const measuredSamples = timing.measuredSampleCount;
  const measuredElapsedMs = timing.firstMeasuredSample
    ? Math.max(0, nowMs - timing.firstMeasuredSample.atMs)
    : 0;
  if (
    measuredSamples < MIN_MEASURED_SAMPLES_FOR_ETA ||
    measuredElapsedMs < ETA_MIN_ELAPSED_MS ||
    percent < ETA_MIN_PROGRESS ||
    staleMs >= STALL_HIDE_ETA_MS
  ) {
    return "none";
  }

  let score = 0;
  score += 2;
  if (measuredSamples >= 3) {
    score += 1;
  }
  if (measuredElapsedMs >= 10_000) {
    score += 1;
  }
  if (percent >= 25) {
    score += 1;
  }
  if (staleMs <= RECENT_RATE_DECAY_START_MS) {
    score += 1;
  }

  const agreement = rateAgreement(overallRate, recentRate);
  if (agreement >= 0.7) {
    score += 1;
  } else if (agreement >= 0.45) {
    score += 0.5;
  }

  if (remainingMs <= 10_000) {
    score += 0.5;
  }

  if (score >= 6) {
    return "high";
  }

  if (score >= 4.5) {
    return "medium";
  }

  return "low";
}

function calculateOverallRate(
  timing: JobProgressTiming,
  percent: number,
  nowMs: number,
): number | null {
  const firstMeasuredSample = timing.firstMeasuredSample;
  if (
    !firstMeasuredSample ||
    percent <= firstMeasuredSample.percent + MIN_PERCENT_DELTA
  ) {
    return null;
  }

  const elapsedMs = nowMs - firstMeasuredSample.atMs;
  if (elapsedMs < MIN_RATE_WINDOW_MS) {
    return null;
  }

  return (percent - firstMeasuredSample.percent) / elapsedMs;
}

function calculateRecentRateWeight(
  staleMs: number,
  recentRate: number | null,
): number {
  if (!recentRate) {
    return 0;
  }

  if (staleMs <= RECENT_RATE_DECAY_START_MS) {
    return BASE_RECENT_RATE_WEIGHT;
  }

  if (staleMs >= RECENT_RATE_DECAY_END_MS) {
    return 0;
  }

  const decaySpan = RECENT_RATE_DECAY_END_MS - RECENT_RATE_DECAY_START_MS;
  const freshness =
    1 - (staleMs - RECENT_RATE_DECAY_START_MS) / Math.max(1, decaySpan);
  return BASE_RECENT_RATE_WEIGHT * Math.max(0, freshness);
}

function resolveDisplayMode(
  percent: number,
  elapsedMs: number | null,
  showEta: boolean,
): ProgressDisplayMode {
  if (showEta) {
    return "detailed";
  }

  if (percent <= 0) {
    return "indeterminate";
  }

  if (elapsedMs !== null && elapsedMs < FAST_TASK_MS) {
    return "hidden";
  }

  return "simple";
}

function blendRates(
  overallRate: number | null,
  recentRate: number | null,
  recentWeight: number,
): number | null {
  if (overallRate === null && recentRate === null) {
    return null;
  }

  if (overallRate === null) {
    return recentRate;
  }

  if (recentRate === null) {
    return overallRate;
  }

  const clampedRecentWeight = clampNumber(recentWeight, 0, 1);
  const overallWeight = 1 - clampedRecentWeight;
  return overallRate * overallWeight + recentRate * clampedRecentWeight;
}

function smoothRate(
  previousRate: number | null,
  nextRate: number,
): number {
  if (previousRate === null) {
    return nextRate;
  }

  return previousRate + (nextRate - previousRate) * RATE_EMA_ALPHA;
}

function rateAgreement(
  overallRate: number | null,
  recentRate: number | null,
): number {
  if (overallRate === null || recentRate === null) {
    return 0;
  }

  const lower = Math.min(overallRate, recentRate);
  const higher = Math.max(overallRate, recentRate);
  if (higher <= 0) {
    return 0;
  }

  return lower / higher;
}

function trimSamples(
  samples: JobProgressSample[],
  nowMs: number,
): JobProgressSample[] {
  const recent = samples.filter((sample, index) => {
    if (index === samples.length - 1) {
      return true;
    }

    return nowMs - sample.atMs <= ROLLING_WINDOW_MS;
  });

  return recent.slice(-MAX_SAMPLES);
}

function formatEtaLabel(
  remainingMs: number | null,
  confidence: EtaConfidence,
): string | null {
  if (remainingMs === null) {
    return null;
  }

  if (remainingMs < 5_000) {
    return "<5s remaining";
  }

  const prefix = confidence === "high" ? "" : "~";
  return `${prefix}${formatDuration(bucketRemainingMs(remainingMs))} remaining`;
}

function formatActiveEtaLabel(estimate: RateEstimate): string {
  if (estimate.stalled) {
    return "waiting for next update";
  }

  if (estimate.remainingMs === null || estimate.confidence === "none") {
    return "estimating time...";
  }

  const formattedEta = formatEtaLabel(estimate.remainingMs, estimate.confidence);
  if (!formattedEta) {
    return "estimating time...";
  }

  if (estimate.confidence === "low" && !formattedEta.startsWith("<")) {
    return `estimating... ${formattedEta}`;
  }

  return formattedEta;
}

function bucketRemainingMs(remainingMs: number): number {
  const absMs = Math.max(0, remainingMs);
  if (absMs < 10_000) {
    return bucketCeil(absMs, 1_000);
  }

  if (absMs < 60_000) {
    return bucketCeil(absMs, 5_000);
  }

  if (absMs < 5 * 60_000) {
    return bucketCeil(absMs, 15_000);
  }

  if (absMs < 30 * 60_000) {
    return bucketCeil(absMs, 30_000);
  }

  return bucketCeil(absMs, 60_000);
}

function bucketCeil(value: number, bucketSize: number): number {
  return Math.max(bucketSize, Math.ceil(value / bucketSize) * bucketSize);
}

function clampPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return 0;
  }

  return clampNumber(percent, 0, 100);
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

function formatDuration(durationMs: number): string {
  const seconds = Math.max(1, Math.ceil(durationMs / 1_000));
  if (seconds < 60) {
    return `${seconds}s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainderSeconds = seconds % 60;
  if (minutes < 60) {
    return remainderSeconds > 0 ? `${minutes}m ${remainderSeconds}s` : `${minutes}m`;
  }

  const hours = Math.floor(minutes / 60);
  const remainderMinutes = minutes % 60;
  return remainderMinutes > 0 ? `${hours}h ${remainderMinutes}m` : `${hours}h`;
}
