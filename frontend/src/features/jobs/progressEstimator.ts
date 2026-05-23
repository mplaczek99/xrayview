import type { JobProgressTiming } from "./model";

const ETA_MIN_ELAPSED_MS = 3_000;
const ETA_MIN_PROGRESS = 8;
const MIN_PERCENT_DELTA = 0.5;
const MIN_RATE_WINDOW_MS = 250;
const BASE_RECENT_RATE_WEIGHT = 0.72;
const RECENT_RATE_DECAY_START_MS = 4_000;
const RECENT_RATE_DECAY_END_MS = 14_000;
const STALL_HIDE_ETA_MS = 12_000;
const MIN_MEASURED_SAMPLES_FOR_ETA = 2;

export type EtaConfidence = "none" | "low" | "medium" | "high";

export interface RateEstimate {
  confidence: EtaConfidence;
  effectiveRate: number | null;
  overallRate: number | null;
  recentRate: number | null;
  recentWeight: number;
  remainingMs: number | null;
  staleMs: number | null;
  stalled: boolean;
}

export function estimateRate(
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
  if (!firstMeasuredSample || percent <= firstMeasuredSample.percent + MIN_PERCENT_DELTA) {
    return null;
  }

  const elapsedMs = nowMs - firstMeasuredSample.atMs;
  if (elapsedMs < MIN_RATE_WINDOW_MS) {
    return null;
  }

  return (percent - firstMeasuredSample.percent) / elapsedMs;
}

function calculateRecentRateWeight(staleMs: number, recentRate: number | null): number {
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
  const freshness = 1 - (staleMs - RECENT_RATE_DECAY_START_MS) / Math.max(1, decaySpan);
  return BASE_RECENT_RATE_WEIGHT * Math.max(0, freshness);
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

function rateAgreement(overallRate: number | null, recentRate: number | null): number {
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

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
