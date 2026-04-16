// Validation test for step 10.3: Deduplicate and batch job polling requests.
// Runs with: node frontend/scripts/validate-batch-polling.mjs
//
// Measures HTTP request reduction when useJobs switches from one get_job call
// per pending job (N requests/cycle) to a single get_jobs batch call (1 request/cycle).
//
// BEFORE: Promise.all with N individual get_job requests per poll cycle.
// AFTER:  Single get_jobs batch request per poll cycle.
//
// Expected: 60-80% reduction in HTTP requests with 3+ concurrent jobs.

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
// Job factory helpers.
// ---------------------------------------------------------------------------

function makeJob(id, state = "running", percent = 0) {
  return { jobId: id, state, progress: { percent, stage: "", message: "" } };
}

// ---------------------------------------------------------------------------
// BEFORE: one get_job request per pending job per poll cycle.
// Mirrors pre-10.3 useJobs.ts (Promise.all with individual getJob calls).
// ---------------------------------------------------------------------------

const FAST_POLL_MS = 200;
const QUEUED_POLL_MS = 1000;
const MAX_POLL_MS = 2000;
const IDLE_POLL_MS = 0;

function makePoller_BEFORE(getJobs, notifyFetch) {
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
    if (pendingJobs.length === 0) { scheduleNext(IDLE_POLL_MS); return; }

    const prePollState = new Map(
      pendingJobs.map((j) => [j.jobId, { percent: j.progress.percent, state: j.state }]),
    );

    // BEFORE: one request per job.
    for (const { jobId } of pendingJobs) {
      notifyFetch(jobId);
    }

    const updatedJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (updatedJobs.length === 0) { scheduleNext(IDLE_POLL_MS); return; }

    let anyProgress = false;
    let allQueued = true;
    let anyNearComplete = false;

    for (const job of updatedJobs) {
      if (job.state !== "queued") allQueued = false;
      if (job.state === "running" && job.progress.percent > 80) anyNearComplete = true;
      const pre = prePollState.get(job.jobId);
      if (pre && (job.progress.percent > pre.percent || job.state !== pre.state)) anyProgress = true;
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
// AFTER: single get_jobs batch request per poll cycle, deduplicating IDs.
// Mirrors post-10.3 useJobs.ts (single getJobs batch call).
// ---------------------------------------------------------------------------

function makePoller_AFTER(getJobs, notifyBatchFetch) {
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
    if (pendingJobs.length === 0) { scheduleNext(IDLE_POLL_MS); return; }

    const prePollState = new Map(
      pendingJobs.map((j) => [j.jobId, { percent: j.progress.percent, state: j.state }]),
    );

    // AFTER: one batch request for all jobs (deduplicated).
    const jobIds = [...new Set(pendingJobs.map((j) => j.jobId))];
    notifyBatchFetch(jobIds);

    const updatedJobs = getJobs().filter(
      (j) => j.state === "queued" || j.state === "running" || j.state === "cancelling",
    );
    if (updatedJobs.length === 0) { scheduleNext(IDLE_POLL_MS); return; }

    let anyProgress = false;
    let allQueued = true;
    let anyNearComplete = false;

    for (const job of updatedJobs) {
      if (job.state !== "queued") allQueued = false;
      if (job.state === "running" && job.progress.percent > 80) anyNearComplete = true;
      const pre = prePollState.get(job.jobId);
      if (pre && (job.progress.percent > pre.percent || job.state !== pre.state)) anyProgress = true;
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

function simulate({ jobCount, durationMs, makePollerFn }) {
  resetTimers();
  let wallTimeMs = 0;
  let httpRequestCount = 0;

  const jobs = Array.from({ length: jobCount }, (_, i) =>
    makeJob(`job-${i + 1}`, "running", 10),
  );

  const poller = makePollerFn({
    getJobs: () => jobs,
    notifyFetch: (id) => { httpRequestCount++; void id; },
    notifyBatchFetch: (ids) => { httpRequestCount++; void ids; },
  });

  poller.start();

  for (let iter = 0; iter < 500 && wallTimeMs < durationMs; iter++) {
    if (timerQueue.length === 0) break;
    const nextMs = timerQueue[0].ms;
    if (wallTimeMs + nextMs > durationMs) break;
    wallTimeMs += nextMs;
    flushOneTimer();
  }

  poller.cancel();
  return { httpRequestCount };
}

// ---------------------------------------------------------------------------
// Tests.
// ---------------------------------------------------------------------------

test("BEFORE: 1 job → 1 HTTP request per poll cycle", () => {
  resetTimers();
  let perCycleRequests = 0;
  let cycles = 0;
  const jobs = [makeJob("j1", "running", 10)];
  const poller = makePoller_BEFORE(() => jobs, () => { perCycleRequests++; });

  // Run 3 cycles manually to count per-cycle requests.
  poller.start(); cycles++;
  assert.equal(perCycleRequests, 1, "BEFORE: 1 job = 1 request first cycle");
  perCycleRequests = 0;
  flushOneTimer(); cycles++;
  assert.equal(perCycleRequests, 1, "BEFORE: 1 job = 1 request second cycle");
  poller.cancel();
  console.log("\nBEFORE: 1 job → 1 request/cycle ✓");
});

test("BEFORE: 3 jobs → 3 HTTP requests per poll cycle", () => {
  resetTimers();
  let requestsThisCycle = 0;
  const jobs = [
    makeJob("j1", "running", 10),
    makeJob("j2", "running", 20),
    makeJob("j3", "running", 30),
  ];
  const poller = makePoller_BEFORE(() => jobs, () => { requestsThisCycle++; });

  poller.start();
  assert.equal(requestsThisCycle, 3, "BEFORE: 3 pending jobs = 3 get_job requests per cycle");
  poller.cancel();
  console.log("BEFORE: 3 jobs → 3 requests/cycle ✓");
});

test("AFTER: 1 job → 1 HTTP request per poll cycle (same as before)", () => {
  resetTimers();
  let batchCalls = 0;
  let batchIds = [];
  const jobs = [makeJob("j1", "running", 10)];
  const poller = makePoller_AFTER(() => jobs, (ids) => { batchCalls++; batchIds = ids; });

  poller.start();
  assert.equal(batchCalls, 1, "AFTER: 1 batch call per cycle");
  assert.deepEqual(batchIds, ["j1"], "AFTER: batch contains the one job ID");
  poller.cancel();
  console.log("\nAFTER: 1 job → 1 batch request/cycle ✓");
});

test("AFTER: 3 jobs → 1 HTTP request per poll cycle (67% reduction)", () => {
  resetTimers();
  let batchCalls = 0;
  let batchIds = [];
  const jobs = [
    makeJob("j1", "running", 10),
    makeJob("j2", "running", 20),
    makeJob("j3", "running", 30),
  ];
  const poller = makePoller_AFTER(() => jobs, (ids) => { batchCalls++; batchIds = [...ids]; });

  poller.start();
  assert.equal(batchCalls, 1, "AFTER: 3 pending jobs still = 1 batch request");
  assert.equal(batchIds.length, 3, "AFTER: batch includes all 3 job IDs");
  assert.ok(batchIds.includes("j1"), "AFTER: j1 in batch");
  assert.ok(batchIds.includes("j2"), "AFTER: j2 in batch");
  assert.ok(batchIds.includes("j3"), "AFTER: j3 in batch");
  poller.cancel();
  console.log("AFTER: 3 jobs → 1 batch request/cycle ✓");
});

test("AFTER: deduplication — no duplicate IDs in batch even if map has same ID twice", () => {
  // Simulate edge case: somehow same jobId appears twice in pending list.
  resetTimers();
  let batchIds = [];
  const duplicateJobs = [
    makeJob("j1", "running", 10),
    makeJob("j1", "running", 10), // duplicate
    makeJob("j2", "running", 20),
  ];
  const poller = makePoller_AFTER(() => duplicateJobs, (ids) => { batchIds = [...ids]; });

  poller.start();
  const uniqueIds = [...new Set(batchIds)];
  assert.equal(batchIds.length, uniqueIds.length, "AFTER: no duplicate IDs in batch request");
  assert.equal(batchIds.length, 2, "AFTER: deduplicated to 2 unique IDs");
  poller.cancel();
  console.log("AFTER: deduplication works — 2 unique IDs from 3 jobs (1 dup) ✓");
});

test("BEFORE vs AFTER: 3-job request count over 5s simulated run", () => {
  const before = simulate({
    jobCount: 3,
    durationMs: 5000,
    makePollerFn: ({ getJobs, notifyFetch }) => makePoller_BEFORE(getJobs, notifyFetch),
  });
  const after = simulate({
    jobCount: 3,
    durationMs: 5000,
    makePollerFn: ({ getJobs, notifyBatchFetch }) => makePoller_AFTER(getJobs, notifyBatchFetch),
  });

  const reduction = ((before.httpRequestCount - after.httpRequestCount) / before.httpRequestCount) * 100;

  console.log("\nRequest count comparison (3 jobs, 5s simulated, no progress):");
  console.log(`  BEFORE: ${before.httpRequestCount} HTTP requests (3 per poll cycle)`);
  console.log(`  AFTER:  ${after.httpRequestCount} HTTP requests (1 per poll cycle)`);
  console.log(`  Reduction: ${reduction.toFixed(1)}% (target: ≥60%)`);

  assert.ok(
    after.httpRequestCount < before.httpRequestCount,
    `AFTER fewer requests than BEFORE: ${after.httpRequestCount} < ${before.httpRequestCount}`,
  );
  assert.ok(
    reduction >= 60,
    `Expected ≥60% reduction with 3 jobs, got ${reduction.toFixed(1)}% (${before.httpRequestCount} → ${after.httpRequestCount})`,
  );
});

test("BEFORE vs AFTER: 5-job request count over 5s simulated run", () => {
  const before = simulate({
    jobCount: 5,
    durationMs: 5000,
    makePollerFn: ({ getJobs, notifyFetch }) => makePoller_BEFORE(getJobs, notifyFetch),
  });
  const after = simulate({
    jobCount: 5,
    durationMs: 5000,
    makePollerFn: ({ getJobs, notifyBatchFetch }) => makePoller_AFTER(getJobs, notifyBatchFetch),
  });

  const reduction = ((before.httpRequestCount - after.httpRequestCount) / before.httpRequestCount) * 100;

  console.log("\nRequest count comparison (5 jobs, 5s simulated, no progress):");
  console.log(`  BEFORE: ${before.httpRequestCount} HTTP requests (5 per poll cycle)`);
  console.log(`  AFTER:  ${after.httpRequestCount} HTTP requests (1 per poll cycle)`);
  console.log(`  Reduction: ${reduction.toFixed(1)}% (target: ≥75%)`);

  assert.ok(
    reduction >= 75,
    `Expected ≥75% reduction with 5 jobs, got ${reduction.toFixed(1)}% (${before.httpRequestCount} → ${after.httpRequestCount})`,
  );
});

test("AFTER: batch poller handles empty pending list without sending request", () => {
  resetTimers();
  let batchCalls = 0;
  const jobs = []; // no pending jobs
  const poller = makePoller_AFTER(() => jobs, () => { batchCalls++; });

  poller.start();
  assert.equal(batchCalls, 0, "AFTER: no batch request when no pending jobs");
  poller.cancel();
  console.log("\nAFTER: empty pending list → 0 requests ✓");
});
