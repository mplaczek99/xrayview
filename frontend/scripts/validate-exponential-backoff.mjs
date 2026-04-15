// Validation test for step 10.1: Exponential backoff on job polling.
// Runs with: node frontend/scripts/validate-exponential-backoff.mjs
//
// Tests that the polling interval backs off exponentially (200 → 400 → 800 → 1600 → 2000ms)
// when jobs make no progress, resets to 200ms on progress, uses 1s for queued-only jobs,
// and stays at 200ms when any running job is >80% complete.
//
// Note: pollers in this script are synchronous mirrors of the async useJobs.ts logic,
// valid because the backoff decision is made after all fetches complete — the async nature
// of getJob() doesn't affect which interval is chosen.

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Mock setTimeout / clearTimeout — capture scheduled intervals without waiting.
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
// Job factory helpers.
// ---------------------------------------------------------------------------

function makeJob(id, state = "running", percent = 0) {
  return {
    jobId: id,
    state,
    progress: { percent, stage: "", message: "" },
  };
}

// ---------------------------------------------------------------------------
// BEFORE: fixed-interval polling — always 200ms while pending.
// Synchronous mirror of pre-backoff useJobs.ts logic.
// ---------------------------------------------------------------------------

const BEFORE_FAST_POLL_MS = 200;
const BEFORE_SLOW_POLL_MS = 2000;
const BEFORE_IDLE_POLL_MS = 0;

function makePoller_BEFORE(getJobs, getUpdatedJob) {
  let cancelled = false;
  let timer;

  function scheduleNext(intervalMs) {
    if (cancelled) return;
    if (timer !== undefined) mockClearTimeout(timer);
    if (intervalMs <= 0) return;
    timer = mockSetTimeout(() => poll(), intervalMs);
  }

  function poll() {
    const jobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (jobs.length === 0) {
      scheduleNext(BEFORE_IDLE_POLL_MS);
      return;
    }
    // Synchronously fetch each job's latest state.
    for (const { jobId } of jobs) {
      getUpdatedJob(jobId);
    }
    const stillPending = getJobs().some(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    scheduleNext(stillPending ? BEFORE_FAST_POLL_MS : BEFORE_SLOW_POLL_MS);
  }

  return {
    start: () => poll(),
    cancel: () => {
      cancelled = true;
      if (timer !== undefined) mockClearTimeout(timer);
    },
  };
}

// ---------------------------------------------------------------------------
// AFTER: exponential backoff polling — synchronous mirror of updated useJobs.ts.
// ---------------------------------------------------------------------------

const AFTER_FAST_POLL_MS = 200;
const AFTER_QUEUED_POLL_MS = 1000;
const AFTER_MAX_POLL_MS = 2000;
const AFTER_IDLE_POLL_MS = 0;

function makePoller_AFTER(getJobs, getUpdatedJob) {
  let cancelled = false;
  let timer;
  let currentIntervalMs = AFTER_FAST_POLL_MS;

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
      scheduleNext(AFTER_IDLE_POLL_MS);
      return;
    }

    // Snapshot pre-poll state for change detection.
    const prePollState = new Map(
      pendingJobs.map((j) => [j.jobId, { percent: j.progress.percent, state: j.state }]),
    );

    // Synchronously fetch each job's latest state.
    for (const { jobId } of pendingJobs) {
      getUpdatedJob(jobId);
    }

    const updatedJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (updatedJobs.length === 0) {
      scheduleNext(AFTER_IDLE_POLL_MS);
      return;
    }

    let anyProgress = false;
    let allQueued = true;
    let anyNearComplete = false;

    for (const job of updatedJobs) {
      if (job.state !== "queued") allQueued = false;
      if (job.state === "running" && job.progress.percent > 80) anyNearComplete = true;
      const pre = prePollState.get(job.jobId);
      if (pre !== undefined) {
        if (job.progress.percent > pre.percent || job.state !== pre.state) {
          anyProgress = true;
        }
      }
    }

    if (anyProgress || anyNearComplete) {
      currentIntervalMs = AFTER_FAST_POLL_MS;
      scheduleNext(currentIntervalMs);
    } else if (allQueued) {
      scheduleNext(AFTER_QUEUED_POLL_MS);
    } else {
      scheduleNext(currentIntervalMs);
      currentIntervalMs = Math.min(currentIntervalMs * 2, AFTER_MAX_POLL_MS);
    }
  }

  return {
    start: () => poll(),
    cancel: () => {
      cancelled = true;
      if (timer !== undefined) mockClearTimeout(timer);
    },
    getCurrentInterval: () => currentIntervalMs,
  };
}

// ---------------------------------------------------------------------------
// Helper: run N poll cycles, return each scheduled interval.
// ---------------------------------------------------------------------------

function runNCycles(poller, n) {
  poller.start();
  const intervals = [];
  for (let i = 0; i < n; i++) {
    if (timerQueue.length === 0) break;
    intervals.push(timerQueue[0].ms);
    flushOneTimer();
  }
  poller.cancel();
  return intervals;
}

// ---------------------------------------------------------------------------
// BEFORE tests: baseline — fixed 200ms regardless of job state.
// ---------------------------------------------------------------------------

test("BEFORE: running job no progress → always 200ms", () => {
  resetTimers();
  const jobs = [makeJob("j1", "running", 10)];
  const poller = makePoller_BEFORE(() => jobs, () => null);

  const intervals = runNCycles(poller, 6);
  assert.deepEqual(intervals, [200, 200, 200, 200, 200, 200],
    "BEFORE: fixed 200ms on every poll regardless of inactivity");
});

test("BEFORE: queued job → same 200ms (no distinction)", () => {
  resetTimers();
  const jobs = [makeJob("j1", "queued", 0)];
  const poller = makePoller_BEFORE(() => jobs, () => null);

  const intervals = runNCycles(poller, 3);
  assert.deepEqual(intervals, [200, 200, 200], "BEFORE: queued same as running");
});

test("BEFORE: 10 idle polls → 10 x 200ms = 2000ms total wait", () => {
  resetTimers();
  const jobs = [makeJob("j1", "running", 50)];
  const poller = makePoller_BEFORE(() => jobs, () => null);

  const intervals = runNCycles(poller, 10);
  const total = intervals.reduce((a, b) => a + b, 0);
  assert.equal(total, 2000, "BEFORE: 10 polls × 200ms = 2000ms");
  assert.equal(intervals.length, 10, "BEFORE: exactly 10 polls");
});

// ---------------------------------------------------------------------------
// AFTER tests: exponential backoff on stalled jobs.
// ---------------------------------------------------------------------------

test("AFTER: running job no progress → exponential backoff 200→400→800→1600→2000 (capped)", () => {
  resetTimers();
  const jobs = [makeJob("j1", "running", 50)];
  const poller = makePoller_AFTER(() => jobs, () => null);

  const intervals = runNCycles(poller, 6);
  assert.deepEqual(intervals, [200, 400, 800, 1600, 2000, 2000],
    "AFTER: backoff doubles each poll, caps at 2000ms");
});

test("AFTER: queued-only jobs → steady 1000ms (not 200ms, not backoff)", () => {
  resetTimers();
  const jobs = [makeJob("j1", "queued", 0)];
  const poller = makePoller_AFTER(() => jobs, () => null);

  const intervals = runNCycles(poller, 5);
  assert.deepEqual(intervals, [1000, 1000, 1000, 1000, 1000],
    "AFTER: queued-only always uses QUEUED_POLL_MS");
});

test("AFTER: progress detected → resets interval to 200ms", () => {
  resetTimers();
  let pollCount = 0;
  const jobs = [makeJob("j1", "running", 50)];

  // First 3 polls: no change (50%). 4th poll: advances to 60%.
  const poller = makePoller_AFTER(
    () => jobs,
    (id) => {
      pollCount++;
      if (pollCount >= 4) {
        jobs[0] = makeJob(id, "running", 60);
      }
    },
  );

  const intervals = runNCycles(poller, 5);
  // Polls 1-3: no progress → schedule-then-double: 200, 400, 800.
  // Poll 4: progress (50→60) → reset currentIntervalMs=200, schedule 200.
  // Poll 5: no progress → schedule currentIntervalMs=200, double to 400.
  // (200ms used twice: once at reset, once as starting point of new backoff)
  assert.deepEqual(intervals, [200, 400, 800, 200, 200],
    "AFTER: backoff resets to 200ms on progress, restarts backoff from 200");
});

test("AFTER: running job >80% → stays at 200ms (near-complete fast path)", () => {
  resetTimers();
  const jobs = [makeJob("j1", "running", 85)];

  // No percent change, but >80% → should stay at 200ms every poll
  const poller = makePoller_AFTER(() => jobs, () => null);

  const intervals = runNCycles(poller, 5);
  assert.deepEqual(intervals, [200, 200, 200, 200, 200],
    "AFTER: near-complete (>80%) always polled at 200ms");
});

test("AFTER: queued → running state transition resets interval to 200ms", () => {
  resetTimers();
  let pollCount = 0;
  const jobs = [makeJob("j1", "queued", 0)];

  // First 2 polls: queued. 3rd poll: transitions to running.
  const poller = makePoller_AFTER(
    () => jobs,
    (id) => {
      pollCount++;
      if (pollCount >= 3) {
        jobs[0] = makeJob(id, "running", 0);
      }
    },
  );

  const intervals = runNCycles(poller, 5);
  // Polls 1-2: queued → 1000ms (currentIntervalMs stays at 200 during queued phase).
  // Poll 3: queued→running → anyProgress=true → currentIntervalMs=200, schedule 200.
  // Poll 4: running, no change → schedule currentIntervalMs=200, double to 400.
  // Poll 5: still no change → schedule 400, double to 800.
  assert.deepEqual(intervals, [1000, 1000, 200, 200, 400],
    "AFTER: state transition queued→running resets backoff to 200ms");
});

test("AFTER: cancelling state jobs back off when no progress", () => {
  resetTimers();
  const jobs = [makeJob("j1", "cancelling", 40)];
  const poller = makePoller_AFTER(() => jobs, () => null);

  const intervals = runNCycles(poller, 4);
  assert.deepEqual(intervals, [200, 400, 800, 1600],
    "AFTER: cancelling jobs use same backoff logic");
});

test("AFTER: multiple jobs — progress in any job resets global interval", () => {
  resetTimers();
  let pollCount = 0;
  const jobs = [
    makeJob("j1", "running", 20),
    makeJob("j2", "running", 30),
  ];

  // j1 never advances. j2 advances on poll 4.
  const poller = makePoller_AFTER(
    () => jobs,
    (id) => {
      pollCount++;
      if (id === "j2" && pollCount >= 7) { // 2 jobs × 3 polls = 6 calls before advance
        jobs[1] = makeJob("j2", "running", 40);
      }
    },
  );

  const intervals = runNCycles(poller, 5);
  // Polls 1-3: no progress from either → 200, 400, 800.
  // Poll 4: j2 advances (30→40) → anyProgress=true → currentIntervalMs=200, schedule 200.
  // Poll 5: no progress → schedule currentIntervalMs=200, double to 400.
  assert.deepEqual(intervals, [200, 400, 800, 200, 200],
    "AFTER: any job progress resets global backoff; restarts from 200ms");
});

// ---------------------------------------------------------------------------
// Request count comparison: BEFORE vs AFTER over simulated 10s render job.
// ---------------------------------------------------------------------------
// Job timeline: queued 0-5s, running 5-10s with progress at t=7s (30%) and t=9s (60%).

test("AFTER: ≥40% fewer HTTP requests than BEFORE over 10s job", () => {
  function simulatePolls(makePoller_fn) {
    resetTimers();
    let wallTimeMs = 0;
    let pollCount = 0;
    const pollIntervals = [];

    let jobState = "queued";
    let jobPercent = 0;

    const jobs = [{ jobId: "j1", state: jobState, progress: { percent: jobPercent, stage: "", message: "" } }];

    function syncJobs(id) {
      if (wallTimeMs >= 9000) { jobPercent = 60; jobState = "running"; }
      else if (wallTimeMs >= 7000) { jobPercent = 30; jobState = "running"; }
      else if (wallTimeMs >= 5000) { jobPercent = 0; jobState = "running"; }
      // else: queued
      jobs[0] = { jobId: id, state: jobState, progress: { percent: jobPercent, stage: "", message: "" } };
    }

    const poller = makePoller_fn(() => jobs, syncJobs);
    poller.start();

    // Drive simulation by consuming timers, advancing wall clock by each interval.
    for (let iter = 0; iter < 200 && wallTimeMs < 10000; iter++) {
      if (timerQueue.length === 0) break;
      const nextMs = timerQueue[0].ms;
      if (wallTimeMs + nextMs > 10000) break;
      wallTimeMs += nextMs;
      pollCount++;
      pollIntervals.push(nextMs);
      flushOneTimer();
    }

    poller.cancel();
    return { pollCount, pollIntervals, totalWaitMs: pollIntervals.reduce((a, b) => a + b, 0) };
  }

  const before = simulatePolls(makePoller_BEFORE);
  const after = simulatePolls(makePoller_AFTER);

  assert.ok(
    before.pollCount > after.pollCount,
    `AFTER fewer polls than BEFORE: ${after.pollCount} < ${before.pollCount}`,
  );

  const reduction = ((before.pollCount - after.pollCount) / before.pollCount) * 100;
  assert.ok(
    reduction >= 40,
    `AFTER: ≥40% request reduction (got ${reduction.toFixed(1)}%: ${before.pollCount} → ${after.pollCount})`,
  );

  console.log(`\nRequest count comparison (0–10s simulated job):`);
  console.log(`  BEFORE: ${before.pollCount} polls at fixed 200ms`);
  console.log(`  AFTER:  ${after.pollCount} polls with exponential backoff`);
  console.log(`  AFTER intervals: [${after.pollIntervals.join(", ")}]`);
  console.log(`  Reduction: ${reduction.toFixed(1)}%`);
});

test("AFTER: interval resets to 200ms when new job becomes pending (effect re-mount)", () => {
  // Simulates pendingJobCount going 0→1: the effect re-mounts, currentIntervalMs resets to 200.
  // Each makePoller_AFTER call creates a fresh closure with currentIntervalMs = FAST_POLL_MS.
  resetTimers();
  const jobs = [makeJob("j1", "running", 20)];

  // Simulate a stalled poller that has backed off to 2000ms.
  const stalledPoller = makePoller_AFTER(() => jobs, () => null);
  runNCycles(stalledPoller, 10); // backs off to 2000ms cap
  stalledPoller.cancel();
  resetTimers();

  // New effect mount: fresh poller, should start at 200ms again.
  const freshPoller = makePoller_AFTER(() => jobs, () => null);
  freshPoller.start();
  assert.equal(timerQueue.length, 1, "AFTER: fresh poller schedules timer");
  assert.equal(timerQueue[0].ms, 200, "AFTER: fresh poller starts at 200ms (not stale 2000ms)");
  freshPoller.cancel();
});
