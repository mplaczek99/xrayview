import type { JobProgress, JobState } from "../../lib/generated/contracts";
import { clamp } from "../../lib/math";
import type { JobProgressTiming } from "./model";

const MIN_PERCENT_DELTA = 0.5;
const MIN_RATE_WINDOW_MS = 250;
const RATE_EMA_ALPHA = 0.35;

interface ProgressSnapshotLike {
  state: JobState;
  progress: JobProgress;
  fromCache: boolean;
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
    return previous ? { ...previous, lastUpdatedAtMs: nowMs } : null;
  }

  const percent = clampPercent(snapshot.progress.percent);
  if (!previous) {
    return freshTiming(nowMs, nowMs, percent);
  }

  // Backend reset its percent; keep startedAtMs so elapsed time continues from job start.
  if (percent + MIN_PERCENT_DELTA < previous.lastProgressPercent) {
    return freshTiming(previous.startedAtMs, nowMs, percent);
  }

  if (Math.abs(previous.lastProgressPercent - percent) < MIN_PERCENT_DELTA) {
    return { ...previous, lastUpdatedAtMs: nowMs };
  }

  const deltaMs = nowMs - previous.lastProgressAtMs;
  const deltaPercent = percent - previous.lastProgressPercent;
  const progressSample = { atMs: nowMs, percent };
  const measuredRate =
    previous.lastProgressPercent > 0 &&
    deltaMs >= MIN_RATE_WINDOW_MS &&
    deltaPercent >= MIN_PERCENT_DELTA
      ? deltaPercent / deltaMs
      : null;

  return {
    startedAtMs: previous.startedAtMs,
    lastUpdatedAtMs: nowMs,
    lastProgressAtMs: nowMs,
    lastProgressPercent: percent,
    firstMeasuredSample: previous.firstMeasuredSample ?? (percent > 0 ? progressSample : null),
    measuredSampleCount: previous.measuredSampleCount + (percent > 0 ? 1 : 0),
    smoothedRate: measuredRate
      ? smoothRate(previous.smoothedRate, measuredRate)
      : previous.smoothedRate,
  };
}

function freshTiming(startedAtMs: number, nowMs: number, percent: number): JobProgressTiming {
  const sample = { atMs: nowMs, percent };
  return {
    startedAtMs,
    lastUpdatedAtMs: nowMs,
    lastProgressAtMs: nowMs,
    lastProgressPercent: percent,
    firstMeasuredSample: percent > 0 ? sample : null,
    measuredSampleCount: percent > 0 ? 1 : 0,
    smoothedRate: null,
  };
}

function smoothRate(previousRate: number | null, nextRate: number): number {
  if (previousRate === null) {
    return nextRate;
  }

  return previousRate + (nextRate - previousRate) * RATE_EMA_ALPHA;
}

export function clampPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return 0;
  }

  return clamp(percent, 0, 100);
}
