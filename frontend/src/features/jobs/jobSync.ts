import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { workbenchActions } from "../../app/store/workbenchStore";
import type { JobSnapshot as ContractJobSnapshot } from "../../lib/generated/contracts";
import { getRuntimeAdapter, normalizeJobSnapshot } from "../../lib/runtime";
import { clearJobSubmitTiming, logCompletedJobVisibleTiming } from "./benchmarks";

const ACTIVE_POLL_MS = 500;
const RECENT_TRANSITION_POLL_MS = 200;
const QUEUED_POLL_MS = 1000;
const MAX_POLL_MS = 2000;
const EVENT_HEARTBEAT_MS = 10_000;
const EVENT_STALE_MS = 10_000;

const runtime = getRuntimeAdapter();

function pendingJobCount(): number {
  return workbenchActions.getState().pendingJobIds.size;
}

function applyJobUpdate(job: Awaited<ReturnType<typeof runtime.getJob>>) {
  workbenchActions.receiveJobUpdate(job);
  if (job.state === "completed") {
    logCompletedJobVisibleTiming(job.jobId);
  } else if (job.state === "failed" || job.state === "cancelled") {
    clearJobSubmitTiming(job.jobId);
  }
}

export function startJobSync(): () => void {
  let cancelled = false;
  let timer: number | undefined;
  let unlistenJobUpdate: UnlistenFn | undefined;
  let eventsSubscribed = false;
  let currentIntervalMs = ACTIVE_POLL_MS;
  let lastEventAtMs = 0;
  let lastPendingJobCount = pendingJobCount();

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

  function scheduleNext(intervalMs: number) {
    if (cancelled) {
      return;
    }

    if (timer !== undefined) {
      window.clearTimeout(timer);
    }

    if (intervalMs <= 0) {
      timer = undefined;
      return;
    }

    timer = window.setTimeout(() => {
      void pollPendingJobs();
    }, intervalMs);
  }

  async function pollPendingJobs() {
    const state = workbenchActions.getState();
    const pendingJobIds = [...state.pendingJobIds];

    if (pendingJobIds.length === 0) {
      scheduleNext(0);
      return;
    }

    if (eventsSubscribed && Date.now() - lastEventAtMs < EVENT_STALE_MS) {
      scheduleNext(EVENT_HEARTBEAT_MS);
      return;
    }

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
      const jobs = await runtime.getJobs(pendingJobIds);
      for (const job of jobs) {
        if (!cancelled) {
          applyJobUpdate(job);
        }
      }
    } catch {
      // Keep the previous snapshots until the next poll cycle.
    }

    if (cancelled) {
      return;
    }

    const updatedState = workbenchActions.getState();
    const updatedJobIds = [...updatedState.pendingJobIds];

    if (updatedJobIds.length === 0) {
      scheduleNext(0);
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
        if (job.state !== pre.state) {
          anyStateTransition = true;
          anyProgress = true;
        } else if (job.progress.percent > pre.percent) {
          anyProgress = true;
        }
      }
    }

    if (anyStateTransition || anyNearComplete) {
      currentIntervalMs = RECENT_TRANSITION_POLL_MS;
      scheduleNext(currentIntervalMs);
    } else if (anyProgress) {
      currentIntervalMs = ACTIVE_POLL_MS;
      scheduleNext(currentIntervalMs);
    } else if (allQueued) {
      scheduleNext(QUEUED_POLL_MS);
    } else {
      scheduleNext(currentIntervalMs);
      currentIntervalMs = Math.min(currentIntervalMs * 2, MAX_POLL_MS);
    }
  }

  const unsubscribe = workbenchActions.subscribe(() => {
    const nextPendingJobCount = pendingJobCount();
    if (nextPendingJobCount === lastPendingJobCount) {
      return;
    }

    lastPendingJobCount = nextPendingJobCount;
    if (nextPendingJobCount > 0 && timer === undefined) {
      void pollPendingJobs();
    }
  });

  if (lastPendingJobCount > 0) {
    void pollPendingJobs();
  }

  return () => {
    cancelled = true;
    unsubscribe();
    if (timer !== undefined) {
      window.clearTimeout(timer);
    }
    unlistenJobUpdate?.();
  };
}
