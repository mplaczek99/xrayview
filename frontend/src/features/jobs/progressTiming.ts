import { useEffect, useState } from "react";
import type { JobProgress, JobState } from "../../lib/generated/contracts";
import type { JobProgressSample, JobProgressTiming } from "./model";

const FAST_TASK_MS = 1_000;
const LONG_TASK_MS = 5_000;
const MAX_SAMPLES = 6;
const ROLLING_WINDOW_MS = 15_000;
const MIN_PROGRESS_FOR_ETA = 12;
const MIN_SAMPLES_FOR_ETA = 2;
const MIN_PERCENT_DELTA = 0.5;

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
}

export interface ProgressSnapshotLike {
  state: JobState;
  progress: JobProgress;
  timing: JobProgressTiming | null;
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
    samples: [{ atMs: nowMs, percent }],
  };
  const lastSample = base.samples[base.samples.length - 1];

  if (lastSample && Math.abs(lastSample.percent - percent) < MIN_PERCENT_DELTA) {
    return {
      ...base,
      lastUpdatedAtMs: nowMs,
    };
  }

  return {
    startedAtMs: base.startedAtMs,
    lastUpdatedAtMs: nowMs,
    lastProgressAtMs: nowMs,
    samples: trimSamples([
      ...base.samples,
      {
        atMs: nowMs,
        percent,
      },
    ], nowMs),
  };
}

export function describeProgress(
  snapshot: ProgressSnapshotLike,
  nowMs = Date.now(),
): ProgressPresentation {
  const pending = isPendingJobState(snapshot.state);
  const timing = snapshot.timing;
  const percent = clampPercent(snapshot.progress.percent);
  const percentLabel =
    percent > 0 && percent < 100 ? `${Math.round(percent)}%` : null;
  const elapsedMs = timing ? Math.max(0, nowMs - timing.startedAtMs) : null;

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
    };
  }

  if (!pending || !timing) {
    return {
      mode: "indeterminate",
      elapsedMs,
      remainingMs: null,
      percentLabel,
      etaLabel: null,
      detailLabel: percentLabel,
      showEta: false,
      indeterminate: true,
    };
  }

  const remainingMs = estimateRemainingMs(timing, percent, nowMs);
  const hasStableEta =
    remainingMs !== null &&
    elapsedMs !== null &&
    elapsedMs >= LONG_TASK_MS &&
    percent >= MIN_PROGRESS_FOR_ETA &&
    timing.samples.length >= MIN_SAMPLES_FOR_ETA;
  const mode = resolveDisplayMode(percent, elapsedMs, hasStableEta);
  const etaLabel = hasStableEta ? `~${formatDuration(remainingMs)} remaining` : null;
  const detailParts = [percentLabel, etaLabel].filter(
    (value): value is string => Boolean(value),
  );

  return {
    mode,
    elapsedMs,
    remainingMs: hasStableEta ? remainingMs : null,
    percentLabel,
    etaLabel,
    detailLabel: detailParts.join(" • ") || null,
    showEta: hasStableEta,
    indeterminate: mode === "indeterminate",
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

function resolveDisplayMode(
  percent: number,
  elapsedMs: number | null,
  hasStableEta: boolean,
): ProgressDisplayMode {
  if (hasStableEta) {
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

function estimateRemainingMs(
  timing: JobProgressTiming,
  percent: number,
  nowMs: number,
): number | null {
  if (percent <= 0 || percent >= 100) {
    return null;
  }

  const elapsedMs = Math.max(1, nowMs - timing.startedAtMs);
  const overallRate = percent / elapsedMs;
  const rollingStart = timing.samples.find(
    (sample) => sample.percent <= percent - MIN_PERCENT_DELTA,
  );
  // Blend a short rolling window with the full-job rate so ETA reacts to
  // recent slowdowns without swinging wildly on every progress tick.
  const rollingRate =
    rollingStart && nowMs > rollingStart.atMs
      ? (percent - rollingStart.percent) / (nowMs - rollingStart.atMs)
      : null;

  const effectiveRate = blendRates(overallRate, rollingRate);
  if (!effectiveRate || !Number.isFinite(effectiveRate) || effectiveRate <= 0) {
    return null;
  }

  return Math.max(0, (100 - percent) / effectiveRate);
}

function blendRates(
  overallRate: number | null,
  rollingRate: number | null,
): number | null {
  if (overallRate === null && rollingRate === null) {
    return null;
  }

  if (overallRate === null) {
    return rollingRate;
  }

  if (rollingRate === null) {
    return overallRate;
  }

  return rollingRate * 0.65 + overallRate * 0.35;
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

  return Math.min(100, Math.max(0, percent));
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
