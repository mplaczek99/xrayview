import type { JobProgress, JobState } from "../../lib/generated/contracts";
import type { JobProgressTiming } from "./model";
import { estimateRate, type EtaConfidence, type RateEstimate } from "./progressEstimator";
import { isPendingJobState, isTerminalJobState } from "./progressTiming";

const FAST_TASK_MS = 1_000;

export type ProgressDisplayMode =
  | "hidden"
  | "simple"
  | "detailed"
  | "indeterminate";

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
