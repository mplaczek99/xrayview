import { useEffect } from "react";
import { getRuntimeAdapter } from "../../lib/runtime";
import { workbenchActions } from "../../app/store/workbenchStore";
import {
  clearJobSubmitTiming,
  logCompletedJobVisibleTiming,
} from "./benchmarks";

const POLL_INTERVAL_MS = 1500;
const runtime = getRuntimeAdapter();

export function useJobs() {
  useEffect(() => {
    let cancelled = false;

    async function pollPendingJobs() {
      const state = workbenchActions.getState();
      const pendingIds = Object.values(state.jobs)
        .filter((job) =>
          job.state === "queued" ||
          job.state === "running" ||
          job.state === "cancelling",
        )
        .map((job) => job.jobId);

      await Promise.all(
        pendingIds.map(async (jobId) => {
          try {
            const job = await runtime.getJob(jobId);
            if (!cancelled) {
              workbenchActions.receiveJobUpdate(job);
              if (job.state === "completed") {
                logCompletedJobVisibleTiming(jobId);
              } else if (job.state === "failed" || job.state === "cancelled") {
                clearJobSubmitTiming(jobId);
              }
            }
          } catch {
            // Keep polling other jobs; individual fetch failures should not tear
            // down the UI loop.
          }
        }),
      );
    }

    void pollPendingJobs();
    const timer = window.setInterval(() => {
      void pollPendingJobs();
    }, POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);
}
