// Validation benchmark for plan step 25: selectPendingJobCount should not scan
// the full jobs map after pendingJobIds is maintained by the store.
// Runs with: node frontend/scripts/validate-pending-job-count-selector.mjs

import { performance } from "node:perf_hooks";

const JOB_COUNT = Number.parseInt(
  process.env.XRAYVIEW_PENDING_JOB_SELECTOR_JOBS ?? "10000",
  10,
);
const ITERATIONS = Number.parseInt(
  process.env.XRAYVIEW_PENDING_JOB_SELECTOR_ITERATIONS ?? "1000",
  10,
);
const SAMPLES = Number.parseInt(
  process.env.XRAYVIEW_PENDING_JOB_SELECTOR_SAMPLES ?? "7",
  10,
);

const PENDING_STATES = new Set(["queued", "running", "cancelling"]);

function makeJob(id, state) {
  return {
    jobId: id,
    jobKind: "renderStudy",
    studyId: "study-1",
    state,
    progress: { percent: state === "completed" ? 100 : 50 },
    fromCache: false,
    result: state === "completed" ? {} : null,
    error: state === "failed" ? { code: "internal", message: "failed" } : null,
    timing: null,
  };
}

function makeState(jobCount) {
  const jobs = {};
  const pendingJobIds = new Set();
  const states = ["queued", "running", "completed", "failed", "cancelling"];

  for (let index = 0; index < jobCount; index += 1) {
    const state = states[index % states.length];
    const jobId = `job-${index}`;
    jobs[jobId] = makeJob(jobId, state);
    if (PENDING_STATES.has(state)) {
      pendingJobIds.add(jobId);
    }
  }

  return { jobs, pendingJobIds };
}

function selectPendingJobCountBefore(state) {
  return Object.values(state.jobs).filter((job) => PENDING_STATES.has(job.state)).length;
}

function selectPendingJobCountAfter(state) {
  return state.pendingJobIds.size;
}

function benchmark(label, selector, state) {
  const samples = [];
  let checksum = 0;

  for (let sample = 0; sample < SAMPLES; sample += 1) {
    const started = performance.now();
    for (let iteration = 0; iteration < ITERATIONS; iteration += 1) {
      checksum += selector(state);
    }
    samples.push(performance.now() - started);
  }

  const mean = samples.reduce((sum, value) => sum + value, 0) / samples.length;
  const min = Math.min(...samples);
  return { label, mean, min, samples, checksum };
}

function formatMs(value) {
  return `${value.toFixed(3)} ms`;
}

const state = makeState(JOB_COUNT);
const expected = selectPendingJobCountBefore(state);
const actual = selectPendingJobCountAfter(state);

if (expected !== actual) {
  throw new Error(`pending count mismatch: before=${expected}, after=${actual}`);
}

// Warm both paths before measuring.
for (let index = 0; index < 100; index += 1) {
  selectPendingJobCountBefore(state);
  selectPendingJobCountAfter(state);
}

const before = benchmark("before Object.values filter", selectPendingJobCountBefore, state);
const after = benchmark("after pendingJobIds.size", selectPendingJobCountAfter, state);

if (before.checksum !== after.checksum) {
  throw new Error(`benchmark checksum mismatch: before=${before.checksum}, after=${after.checksum}`);
}

const speedup = before.mean / after.mean;
const timeSaved = before.mean - after.mean;

console.log(
  `Pending job count selector benchmark (${JOB_COUNT} jobs, ${ITERATIONS} reads/sample, ${SAMPLES} samples)`,
);
console.log(`${before.label}: mean ${formatMs(before.mean)}, min ${formatMs(before.min)}`);
console.log(`${after.label}: mean ${formatMs(after.mean)}, min ${formatMs(after.min)}`);
console.log(
  `speedup time: ${speedup.toFixed(2)}x, ${formatMs(timeSaved)} faster per ${ITERATIONS} reads`,
);
console.log(`pending count: ${actual}`);
console.log(`samples before: ${before.samples.map(formatMs).join(", ")}`);
console.log(`samples after: ${after.samples.map(formatMs).join(", ")}`);
