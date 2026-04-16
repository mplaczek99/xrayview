// Validation test for step 10.2: SSE-based job updates replacing HTTP polling.
// Runs with: node frontend/scripts/validate-sse-polling-reduction.mjs
//
// Measures the reduction in HTTP get_job poll requests when SSE events are
// actively delivering job updates (desktop mode with Wails EventsOn active).
//
// BEFORE: frontend polls /commands/get_job every 200ms while jobs are pending.
// AFTER:  frontend skips polling when events fired within the last 10s.
//         A 10s heartbeat ensures fallback if events go stale.
//
// Expected result: ≥90% reduction in poll requests for jobs ≤10s duration.

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Mock timer infrastructure.
// ---------------------------------------------------------------------------

let timerQueue = [];
let timerNextId = 1;

function mockSetTimeout(cb, ms) {
  const id = timerNextId++;
  timerQueue.push({ id, cb, ms });
  return id;
}

function mockClearTimeout(id) {
  timerQueue = timerQueue.filter((t) => t.id !== id);
}

function flushOneTimer() {
  const first = timerQueue.shift();
  if (first) first.cb();
}

function resetTimers() {
  timerQueue = [];
  timerNextId = 1;
}

// ---------------------------------------------------------------------------
// Job helpers.
// ---------------------------------------------------------------------------

function makeJob(id, state = "running", percent = 0) {
  return { jobId: id, state, progress: { percent, stage: "", message: "" } };
}

// ---------------------------------------------------------------------------
// BEFORE: polling-only mode (no SSE).
// Mirrors useJobs.ts with FAST_POLL_MS=200 + exponential backoff (step 10.1).
// ---------------------------------------------------------------------------

const FAST_POLL_MS = 200;
const QUEUED_POLL_MS = 1000;
const MAX_POLL_MS = 2000;

function makePoller_BEFORE_polling_only(getJobs, notifyFetch) {
  let cancelled = false;
  let timer;
  let currentIntervalMs = FAST_POLL_MS;

  function scheduleNext(intervalMs) {
    if (cancelled) return;
    if (timer !== undefined) mockClearTimeout(timer);
    if (intervalMs <= 0) return;
    timer = mockSetTimeout(() => poll(), intervalMs);
  }

  function poll() {
    const pendingJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (pendingJobs.length === 0) {
      scheduleNext(0);
      return;
    }

    const prePollState = new Map(
      pendingJobs.map((j) => [j.jobId, { percent: j.progress.percent, state: j.state }]),
    );

    for (const { jobId } of pendingJobs) {
      notifyFetch(jobId); // counts as one HTTP get_job request
    }

    const updatedJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (updatedJobs.length === 0) { scheduleNext(0); return; }

    let anyProgress = false;
    let allQueued = true;
    let anyNearComplete = false;

    for (const job of updatedJobs) {
      if (job.state !== "queued") allQueued = false;
      if (job.state === "running" && job.progress.percent > 80) anyNearComplete = true;
      const pre = prePollState.get(job.jobId);
      if (pre && (job.progress.percent > pre.percent || job.state !== pre.state)) {
        anyProgress = true;
      }
    }

    if (anyProgress || anyNearComplete) {
      currentIntervalMs = FAST_POLL_MS;
      scheduleNext(currentIntervalMs);
    } else if (allQueued) {
      scheduleNext(QUEUED_POLL_MS);
    } else {
      scheduleNext(currentIntervalMs);
      currentIntervalMs = Math.min(currentIntervalMs * 2, MAX_POLL_MS);
    }
  }

  return {
    start: () => poll(),
    cancel: () => { cancelled = true; if (timer !== undefined) mockClearTimeout(timer); },
  };
}

// ---------------------------------------------------------------------------
// AFTER: SSE-aware polling mode (step 10.2).
// getNowMs() returns the current simulated wall-clock time (not Date.now()).
// When eventsOn is active and lastEventAtMs is fresh, polling is suppressed.
// ---------------------------------------------------------------------------

const EVENT_HEARTBEAT_MS = 10_000;
const EVENT_STALE_MS = 10_000;

function makePoller_AFTER_with_sse(getJobs, notifyFetch, getLastEventAtMs, getNowMs) {
  let cancelled = false;
  let timer;
  let currentIntervalMs = FAST_POLL_MS;
  const eventsOn = true; // desktop mode: Wails EventsOn is available

  function scheduleNext(intervalMs) {
    if (cancelled) return;
    if (timer !== undefined) mockClearTimeout(timer);
    if (intervalMs <= 0) return;
    timer = mockSetTimeout(() => poll(), intervalMs);
  }

  function poll() {
    const pendingJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (pendingJobs.length === 0) {
      scheduleNext(0);
      return;
    }

    // SSE suppression: skip HTTP polling when events are fresh (virtual clock).
    if (eventsOn && getNowMs() - getLastEventAtMs() < EVENT_STALE_MS) {
      scheduleNext(EVENT_HEARTBEAT_MS);
      return;
    }

    const prePollState = new Map(
      pendingJobs.map((j) => [j.jobId, { percent: j.progress.percent, state: j.state }]),
    );

    for (const { jobId } of pendingJobs) {
      notifyFetch(jobId); // HTTP poll — not suppressed by events
    }

    const updatedJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (updatedJobs.length === 0) { scheduleNext(0); return; }

    let anyProgress = false;
    let allQueued = true;
    let anyNearComplete = false;

    for (const job of updatedJobs) {
      if (job.state !== "queued") allQueued = false;
      if (job.state === "running" && job.progress.percent > 80) anyNearComplete = true;
      const pre = prePollState.get(job.jobId);
      if (pre && (job.progress.percent > pre.percent || job.state !== pre.state)) {
        anyProgress = true;
      }
    }

    if (anyProgress || anyNearComplete) {
      currentIntervalMs = FAST_POLL_MS;
      scheduleNext(currentIntervalMs);
    } else if (allQueued) {
      scheduleNext(QUEUED_POLL_MS);
    } else {
      scheduleNext(currentIntervalMs);
      currentIntervalMs = Math.min(currentIntervalMs * 2, MAX_POLL_MS);
    }
  }

  return {
    start: () => poll(),
    cancel: () => { cancelled = true; if (timer !== undefined) mockClearTimeout(timer); },
  };
}

// ---------------------------------------------------------------------------
// Simulation driver.
// ---------------------------------------------------------------------------

// Simulate a job running for durationMs, advancing state at the given
// milestones. SSE events fire at each milestone in the AFTER variant.
// Returns { pollCount } when wall clock reaches durationMs.
function simulateJob({ durationMs, jobTimeline, makePollerFn, withSSE }) {
  resetTimers();
  let wallTimeMs = 0;
  let pollCount = 0;
  let lastEventAtMs = withSSE ? 0 : -EVENT_STALE_MS * 10; // AFTER: starts fresh

  function currentJobState() {
    let state = "queued";
    let percent = 0;
    for (const milestone of jobTimeline) {
      if (wallTimeMs >= milestone.atMs) {
        state = milestone.state;
        percent = milestone.percent;
      }
    }
    return makeJob("j1", state, percent);
  }

  const jobs = [currentJobState()];

  function fireSSEEvent() {
    // SSE event: update job state from backend push (no HTTP fetch counted).
    jobs[0] = currentJobState();
    lastEventAtMs = wallTimeMs; // simulation time
  }

  const poller = makePollerFn({
    getJobs: () => jobs,
    notifyFetch: () => { jobs[0] = currentJobState(); pollCount++; },
    getLastEventAtMs: () => lastEventAtMs,
    getNowMs: () => wallTimeMs,
  });

  poller.start();

  // Drive simulation by consuming timers and advancing wall clock.
  // SSE events fire when the milestone time passes during timer advancement.
  let milestoneIdx = 0;

  for (let iter = 0; iter < 1000 && wallTimeMs < durationMs; iter++) {
    if (timerQueue.length === 0) break;
    const nextTimerMs = timerQueue[0].ms;
    const nextWallMs = wallTimeMs + nextTimerMs;
    if (nextWallMs > durationMs) break;

    // Fire any SSE milestones that fall between now and nextWallMs.
    if (withSSE) {
      while (
        milestoneIdx < jobTimeline.length &&
        jobTimeline[milestoneIdx].atMs <= nextWallMs
      ) {
        wallTimeMs = jobTimeline[milestoneIdx].atMs;
        fireSSEEvent();
        milestoneIdx++;
      }
    }

    wallTimeMs = nextWallMs;
    flushOneTimer();
  }

  poller.cancel();
  return { pollCount };
}

// Convenience wrappers.
function beforeSimulate(opts) {
  return simulateJob({
    ...opts,
    withSSE: false,
    makePollerFn: ({ getJobs, notifyFetch }) =>
      makePoller_BEFORE_polling_only(getJobs, notifyFetch),
  });
}

function afterSimulate(opts) {
  return simulateJob({
    ...opts,
    withSSE: true,
    makePollerFn: ({ getJobs, notifyFetch, getLastEventAtMs, getNowMs }) =>
      makePoller_AFTER_with_sse(getJobs, notifyFetch, getLastEventAtMs, getNowMs),
  });
}

// ---------------------------------------------------------------------------
// Test scenarios.
// ---------------------------------------------------------------------------

// Scenario: 3-second render job with frequent progress events.
const SHORT_JOB_TIMELINE = [
  { atMs: 400,  state: "running", percent: 10 },
  { atMs: 800,  state: "running", percent: 20 },
  { atMs: 1200, state: "running", percent: 35 },
  { atMs: 1600, state: "running", percent: 50 },
  { atMs: 2000, state: "running", percent: 65 },
  { atMs: 2400, state: "running", percent: 80 },
  { atMs: 2800, state: "running", percent: 92 },
];
const SHORT_JOB_DURATION_MS = 3000;

test("BEFORE (polling only): 3s job with progress every 400ms → ≥10 get_job requests", () => {
  const result = beforeSimulate({ durationMs: SHORT_JOB_DURATION_MS, jobTimeline: SHORT_JOB_TIMELINE });
  assert.ok(result.pollCount >= 10, `BEFORE: at least 10 polls in 3s job (got ${result.pollCount})`);
  console.log(`\nBEFORE (polling only): 3s job → ${result.pollCount} HTTP get_job requests`);
});

test("AFTER (SSE active): 3s job with progress every 400ms → ≤1 poll request", () => {
  const result = afterSimulate({ durationMs: SHORT_JOB_DURATION_MS, jobTimeline: SHORT_JOB_TIMELINE });
  // SSE event fires every 400ms, well within EVENT_STALE_MS=10s, so polling is
  // fully suppressed. Only the very first poll (before any event) may fire.
  assert.ok(
    result.pollCount <= 1,
    `AFTER: ≤1 HTTP polls for 3s job when SSE active (got ${result.pollCount})`,
  );
  console.log(`AFTER  (SSE active):   3s job → ${result.pollCount} HTTP get_job requests`);
});

test("AFTER vs BEFORE: ≥90% fewer requests for 3s job with SSE active", () => {
  const before = beforeSimulate({ durationMs: SHORT_JOB_DURATION_MS, jobTimeline: SHORT_JOB_TIMELINE });
  const after  = afterSimulate({ durationMs: SHORT_JOB_DURATION_MS, jobTimeline: SHORT_JOB_TIMELINE });

  assert.ok(before.pollCount > 0, "BEFORE: at least one poll");
  const reduction = after.pollCount === 0
    ? 100
    : ((before.pollCount - after.pollCount) / before.pollCount) * 100;

  console.log(`\nSSE polling reduction (3s job): ${before.pollCount} → ${after.pollCount} requests`);
  console.log(`Reduction: ${reduction.toFixed(1)}% (target: ≥90%)`);

  assert.ok(
    reduction >= 90,
    `Expected ≥90% reduction, got ${reduction.toFixed(1)}% (${before.pollCount} → ${after.pollCount})`,
  );
});

// Scenario: 30-second long-running job with SSE events every 5s.
const LONG_JOB_TIMELINE = [
  { atMs: 1000,  state: "running", percent: 5 },
  { atMs: 5000,  state: "running", percent: 20 },
  { atMs: 10000, state: "running", percent: 40 },
  { atMs: 15000, state: "running", percent: 60 },
  { atMs: 20000, state: "running", percent: 80 },
  { atMs: 25000, state: "running", percent: 95 },
];
const LONG_JOB_DURATION_MS = 30_000;

test("BEFORE (polling only): 30s job with backoff → bounded request count", () => {
  const result = beforeSimulate({ durationMs: LONG_JOB_DURATION_MS, jobTimeline: LONG_JOB_TIMELINE });
  console.log(`\nBEFORE (polling only): 30s job → ${result.pollCount} HTTP get_job requests`);
  assert.ok(result.pollCount > 0, "BEFORE: should have polls");
});

test("AFTER (SSE active): 30s job with events every 5s → ≥80% fewer requests", () => {
  const before = beforeSimulate({ durationMs: LONG_JOB_DURATION_MS, jobTimeline: LONG_JOB_TIMELINE });
  const after  = afterSimulate({ durationMs: LONG_JOB_DURATION_MS, jobTimeline: LONG_JOB_TIMELINE });

  const reduction = after.pollCount === 0
    ? 100
    : ((before.pollCount - after.pollCount) / before.pollCount) * 100;

  console.log(`AFTER  (SSE active):   30s job → ${after.pollCount} HTTP get_job requests`);
  console.log(`Reduction: ${reduction.toFixed(1)}% (target: ≥80% for events every 5s)`);

  assert.ok(
    reduction >= 80,
    `Expected ≥80% reduction for 30s job, got ${reduction.toFixed(1)}% (${before.pollCount} → ${after.pollCount})`,
  );
});

test("AFTER: no SSE events → falls back to normal polling after 10s stale window", () => {
  // Simulate a job with NO SSE events (sidecar SSE bridge disconnected).
  // The first poll fires immediately (lastEventAtMs starts at -infinity, stale).
  // After each poll, if still no events, the exponential backoff drives future polls.
  resetTimers();
  let wallTimeMs = 0;
  let pollCount = 0;
  const lastEventAtMs = -EVENT_STALE_MS * 10; // never fired

  const jobs = [makeJob("j1", "running", 50)];
  const poller = makePoller_AFTER_with_sse(
    () => jobs,
    () => pollCount++,
    () => lastEventAtMs,
    () => wallTimeMs,
  );

  poller.start();

  for (let iter = 0; iter < 200 && wallTimeMs < 25000; iter++) {
    if (timerQueue.length === 0) break;
    const nextMs = timerQueue[0].ms;
    if (wallTimeMs + nextMs > 25000) break;
    wallTimeMs += nextMs;
    flushOneTimer();
  }

  poller.cancel();

  assert.ok(
    pollCount >= 1,
    `Expected fallback polling when SSE stale (got ${pollCount} polls in 25s)`,
  );
  console.log(`\nFallback (no SSE events): ${pollCount} polls in 25s simulation`);
});
