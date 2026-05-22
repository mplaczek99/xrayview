import { useEffect } from "react";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import type { JobSnapshot as ContractJobSnapshot } from "../../lib/generated/contracts";
import { getRuntimeAdapter, normalizeJobSnapshot } from "../../lib/runtime";
import { useWorkbenchStore, workbenchActions } from "../../app/store/workbenchStore";
import { selectPendingJobCount } from "../../app/store/selectors";
import {
  clearJobSubmitTiming,
  logCompletedJobVisibleTiming,
} from "./benchmarks";

const ACTIVE_POLL_MS = 500;
const RECENT_TRANSITION_POLL_MS = 200;
const QUEUED_POLL_MS = 1000;
const MAX_POLL_MS = 2000;
const IDLE_POLL_MS = 0;
// When Tauri job-update events are active, skip HTTP polling and re-check after this delay.
// Falls back to normal polling if no event arrives within EVENT_STALE_MS.
const EVENT_HEARTBEAT_MS = 10_000;
const EVENT_STALE_MS = 10_000;
const runtime = getRuntimeAdapter();

export function useJobs() {
  const pendingJobCount = useWorkbenchStore(selectPendingJobCount);

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;
    let unlistenJobUpdate: UnlistenFn | undefined;
    let eventsSubscribed = false;
    let currentIntervalMs = ACTIVE_POLL_MS;
    // Tracks the last time a job-update event was received over the Tauri event bus.
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

    if (runtime.mode === "desktop") {
      eventsSubscribed = true;
      void listen<ContractJobSnapshot>("job-update", (event) => {
        if (cancelled) {
          return;
        }
        lastEventAtMs = Date.now();
        applyJobUpdate(normalizeJobSnapshot(event.payload, runtime.mode));
      }).then((unlisten) => {
        if (cancelled) {
          unlisten();
          return;
        }
        unlistenJobUpdate = unlisten;
      });
    }

    async function pollPendingJobs() {
      const state = workbenchActions.getState();
      const pendingJobIds = [...state.pendingJobIds];

      if (pendingJobIds.length === 0) {
        scheduleNext(IDLE_POLL_MS);
        return;
      }

      // When job-update events are actively delivering updates, skip HTTP polling
      // entirely and schedule a heartbeat in case the stream goes stale.
      if (eventsSubscribed && Date.now() - lastEventAtMs < EVENT_STALE_MS) {
        scheduleNext(EVENT_HEARTBEAT_MS);
        return;
      }

      // Snapshot pre-poll state for progress change detection.
      const prePollState = new Map<string, { percent: number; state: string }>();
      for (const jobId of pendingJobIds) {
        const job = state.jobs[jobId];
        if (job) {
          prePollState.set(jobId, {
            percent: job.progress.percent,
            state: job.state,
          });
        }
      }

      try {
        await runtime.forEachJob(pendingJobIds, (job) => {
          if (!cancelled) {
            applyJobUpdate(job);
          }
        });
      } catch {
        // Batch fetch failed; individual job states remain unchanged until
        // the next poll cycle.
      }

      if (cancelled) {
        return;
      }

      const updatedState = workbenchActions.getState();
      const updatedJobIds = [...updatedState.pendingJobIds];

      if (updatedJobIds.length === 0) {
        scheduleNext(IDLE_POLL_MS);
        return;
      }

      let anyProgress = false;
      let anyStateTransition = false;
      let allQueued = true;
      let anyNearComplete = false;

      for (const jobId of updatedJobIds) {
        const job = updatedState.jobs[jobId];
        if (!job) {
          continue;
        }
        if (job.state !== "queued") {
          allQueued = false;
        }
        if (job.state === "running" && job.progress.percent > 80) {
          anyNearComplete = true;
        }
        const pre = prePollState.get(job.jobId);
        if (pre !== undefined) {
          // Percent advance or state transition (queued → running) counts as progress.
          if (job.state !== pre.state) {
            anyStateTransition = true;
            anyProgress = true;
          } else if (job.progress.percent > pre.percent) {
            anyProgress = true;
          }
        }
      }

      // Completion events arrive in the embedded desktop path, but progress
      // updates still come from polling while a job is running.
      if (anyStateTransition || anyNearComplete) {
        // State transitions and near-complete jobs get a brief fast cadence.
        currentIntervalMs = RECENT_TRANSITION_POLL_MS;
        scheduleNext(currentIntervalMs);
      } else if (anyProgress) {
        // Progress detected: reset to the active cadence.
        currentIntervalMs = ACTIVE_POLL_MS;
        scheduleNext(currentIntervalMs);
      } else if (allQueued) {
        // Queued-only: use steady slow interval without advancing the backoff state.
        // currentIntervalMs stays at ACTIVE_POLL_MS so backoff starts fresh when running begins.
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
      unlistenJobUpdate?.();
    };
  }, [pendingJobCount]);
}
