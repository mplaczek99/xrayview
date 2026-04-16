import { useEffect } from "react";
import type { JobSnapshot as ContractJobSnapshot } from "../../lib/generated/contracts";
import { getRuntimeAdapter, normalizeJobSnapshot } from "../../lib/runtime";
import {
  selectPendingJobCount,
  useWorkbenchStore,
  workbenchActions,
} from "../../app/store/workbenchStore";
import {
  clearJobSubmitTiming,
  logCompletedJobVisibleTiming,
} from "./benchmarks";

const FAST_POLL_MS = 200;
const QUEUED_POLL_MS = 1000;
const MAX_POLL_MS = 2000;
const IDLE_POLL_MS = 0;
// When SSE events are active, skip HTTP polling and re-check after this delay.
// Falls back to normal polling if no event arrives within EVENT_STALE_MS.
const EVENT_HEARTBEAT_MS = 10_000;
const EVENT_STALE_MS = 10_000;
const JOB_UPDATE_EVENT = "xrayview:job-update";
const runtime = getRuntimeAdapter();

declare global {
  interface Window {
    runtime?: {
      EventsOn?: (
        eventName: string,
        callback: (...args: unknown[]) => void,
      ) => (() => void) | void;
    };
  }
}

export function useJobs() {
  const pendingJobCount = useWorkbenchStore(selectPendingJobCount);

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    let unsubscribeEvent: (() => void) | undefined;
    let currentIntervalMs = FAST_POLL_MS;
    // Tracks the last time a job-update event was received via Wails/SSE.
    // When fresh (< EVENT_STALE_MS ago), HTTP polling is suppressed entirely.
    let lastEventAtMs = 0;

    function applyJobUpdate(job: Awaited<ReturnType<typeof runtime.getJob>>) {
      workbenchActions.receiveJobUpdate(job);
      if (job.state === "completed") {
        logCompletedJobVisibleTiming(job.jobId);
      } else if (job.state === "failed" || job.state === "cancelled") {
        clearJobSubmitTiming(job.jobId);
      }
    }

    const eventsOn = runtime.mode === "desktop" ? window.runtime?.EventsOn : undefined;
    if (eventsOn) {
      const unsubscribe = eventsOn(
        JOB_UPDATE_EVENT,
        (...args: unknown[]) => {
          if (cancelled) {
            return;
          }

          const [snapshot] = args as [ContractJobSnapshot | undefined];
          if (!snapshot) {
            return;
          }

          lastEventAtMs = Date.now();
          applyJobUpdate(normalizeJobSnapshot(snapshot, runtime.mode));
        },
      );
      if (typeof unsubscribe === "function") {
        unsubscribeEvent = unsubscribe;
      }
    }

    async function pollPendingJobs() {
      const state = workbenchActions.getState();
      const pendingJobs = Object.values(state.jobs).filter(
        (job) =>
          job.state === "queued" ||
          job.state === "running" ||
          job.state === "cancelling",
      );

      if (pendingJobs.length === 0) {
        scheduleNext(IDLE_POLL_MS);
        return;
      }

      // When SSE/Wails events are actively delivering updates, skip HTTP
      // polling entirely and schedule a heartbeat in case events go stale.
      if (eventsOn && Date.now() - lastEventAtMs < EVENT_STALE_MS) {
        scheduleNext(EVENT_HEARTBEAT_MS);
        return;
      }

      // Snapshot pre-poll state for progress change detection.
      const prePollState = new Map(
        pendingJobs.map((job) => [job.jobId, { percent: job.progress.percent, state: job.state }]),
      );

      // Batch fetch: deduplicate IDs and fetch all snapshots in one request.
      const jobIds = [...new Set(pendingJobs.map((job) => job.jobId))];
      try {
        const snapshots = await runtime.getJobs(jobIds);
        if (!cancelled) {
          for (const job of snapshots) {
            applyJobUpdate(job);
          }
        }
      } catch {
        // Batch fetch failed; individual job states remain unchanged until
        // the next poll cycle.
      }

      if (cancelled) {
        return;
      }

      const updatedJobs = Object.values(workbenchActions.getState().jobs).filter(
        (job) =>
          job.state === "queued" ||
          job.state === "running" ||
          job.state === "cancelling",
      );

      if (updatedJobs.length === 0) {
        scheduleNext(IDLE_POLL_MS);
        return;
      }

      let anyProgress = false;
      let allQueued = true;
      let anyNearComplete = false;

      for (const job of updatedJobs) {
        if (job.state !== "queued") {
          allQueued = false;
        }
        if (job.state === "running" && job.progress.percent > 80) {
          anyNearComplete = true;
        }
        const pre = prePollState.get(job.jobId);
        if (pre !== undefined) {
          // Percent advance or state transition (queued → running) counts as progress.
          if (job.progress.percent > pre.percent || job.state !== pre.state) {
            anyProgress = true;
          }
        }
      }

      // Completion events arrive in the embedded desktop path, but progress
      // updates still come from polling while a job is running.
      if (anyProgress || anyNearComplete) {
        // Progress detected or near-complete: reset to fast polling.
        currentIntervalMs = FAST_POLL_MS;
        scheduleNext(currentIntervalMs);
      } else if (allQueued) {
        // Queued-only: use steady slow interval without advancing the backoff state.
        // currentIntervalMs stays at FAST_POLL_MS so backoff starts fresh when running begins.
        scheduleNext(QUEUED_POLL_MS);
      } else {
        // No progress on running/cancelling jobs: schedule at current interval then double.
        scheduleNext(currentIntervalMs);
        currentIntervalMs = Math.min(currentIntervalMs * 2, MAX_POLL_MS);
      }
    }

    function scheduleNext(intervalMs: number) {
      if (cancelled) {
        return;
      }

      if (timer !== undefined) {
        window.clearTimeout(timer);
      }

      if (intervalMs <= 0) {
        return;
      }

      timer = window.setTimeout(() => {
        void pollPendingJobs();
      }, intervalMs);
    }

    if (pendingJobCount > 0) {
      void pollPendingJobs();
    }

    return () => {
      cancelled = true;
      if (timer !== undefined) {
        window.clearTimeout(timer);
      }
      unsubscribeEvent?.();
    };
  }, [pendingJobCount]);
}
