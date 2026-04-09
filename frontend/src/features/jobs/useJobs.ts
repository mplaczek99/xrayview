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
const SLOW_POLL_MS = 2000;
const IDLE_POLL_MS = 0;
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

          applyJobUpdate(normalizeJobSnapshot(snapshot, runtime.mode));
        },
      );
      if (typeof unsubscribe === "function") {
        unsubscribeEvent = unsubscribe;
      }
    }

    async function pollPendingJobs() {
      const state = workbenchActions.getState();
      const pendingIds = Object.values(state.jobs)
        .filter((job) =>
          job.state === "queued" ||
          job.state === "running" ||
          job.state === "cancelling",
        )
        .map((job) => job.jobId);

      if (pendingIds.length === 0) {
        scheduleNext(IDLE_POLL_MS);
        return;
      }

      await Promise.all(
        pendingIds.map(async (jobId) => {
          try {
            const job = await runtime.getJob(jobId);
            if (!cancelled) {
              applyJobUpdate(job);
            }
          } catch {
            // Keep polling other jobs; individual fetch failures should not tear
            // down the UI loop.
          }
        }),
      );

      const stillPending = Object.values(workbenchActions.getState().jobs).some(
        (job) =>
          job.state === "queued" ||
          job.state === "running" ||
          job.state === "cancelling",
      );

      // Completion events arrive in the embedded desktop path, but progress
      // updates still come from polling while a job is running.
      scheduleNext(stillPending ? FAST_POLL_MS : SLOW_POLL_MS);
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
