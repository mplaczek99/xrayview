import type { JobProgress, JobState } from "../../lib/generated/contracts";
import type { JobProgressSample, JobProgressTiming } from "./model";

const MAX_SAMPLES = 8;
const ROLLING_WINDOW_MS = 20_000;
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

function smoothRate(
  previousRate: number | null,
  nextRate: number,
): number {
  if (previousRate === null) {
    return nextRate;
  }

  return previousRate + (nextRate - previousRate) * RATE_EMA_ALPHA;
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

function clampPercent(percent: number): number {
  if (!Number.isFinite(percent)) {
    return 0;
  }

  return clampNumber(percent, 0, 100);
}

function clampNumber(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
