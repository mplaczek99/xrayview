import { useEffect } from "react";
import { getJob } from "../../lib/backend";
import { workbenchActions } from "../../app/store/workbenchStore";

const POLL_INTERVAL_MS = 1500;

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
            const job = await getJob(jobId);
            if (!cancelled) {
              workbenchActions.receiveJobUpdate(job);
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
