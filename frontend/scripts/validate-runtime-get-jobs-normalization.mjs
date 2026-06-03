// Validation benchmark for plan step 28: polling should not allocate a
// normalized job array before applying each polled job update.
// Runs with: node frontend/scripts/validate-runtime-get-jobs-normalization.mjs

import { performance } from "node:perf_hooks";

const JOB_COUNT = Number.parseInt(process.env.XRAYVIEW_RUNTIME_GET_JOBS_BENCH_JOBS ?? "20000", 10);
const ITERATIONS = Number.parseInt(
  process.env.XRAYVIEW_RUNTIME_GET_JOBS_BENCH_ITERATIONS ?? "100",
  10,
);
const SAMPLES = Number.parseInt(process.env.XRAYVIEW_RUNTIME_GET_JOBS_BENCH_SAMPLES ?? "7", 10);
const RETAINED_JOBS = 1024;

function makeSnapshots(jobCount) {
  const snapshots = new Array(jobCount);
  for (let index = 0; index < jobCount; index += 1) {
    snapshots[index] = {
      jobId: `job-${index}`,
      jobKind: "renderStudy",
      studyId: `study-${index % 20}`,
      state: "running",
      progress: {
        percent: index % 100,
        stage: "rendering",
        message: "Rendering preview",
      },
      fromCache: false,
      result: null,
      error: null,
    };
  }
  return snapshots;
}

function normalizeJobSnapshot(snapshot) {
  return {
    jobId: snapshot.jobId,
    jobKind: snapshot.jobKind,
    studyId: snapshot.studyId ?? null,
    state: snapshot.state,
    progress: snapshot.progress,
    fromCache: snapshot.fromCache,
    result: snapshot.result,
    error: snapshot.error ?? null,
    timing: null,
  };
}

function runBefore(snapshots) {
  const receiver = createReceiver();

  for (let iteration = 0; iteration < ITERATIONS; iteration += 1) {
    const jobs = snapshots.map((snapshot) => normalizeJobSnapshot(snapshot));
    for (const job of jobs) {
      receiver.receive(job);
    }
  }

  return receiver.checksum;
}

function runAfter(snapshots) {
  const receiver = createReceiver();

  for (let iteration = 0; iteration < ITERATIONS; iteration += 1) {
    for (const snapshot of snapshots) {
      const job = normalizeJobSnapshot(snapshot);
      receiver.receive(job);
    }
  }

  return receiver.checksum;
}

function createReceiver() {
  const retained = new Array(RETAINED_JOBS);
  let checksum = 0;
  let index = 0;

  return {
    receive(job) {
      checksum += job.progress.percent;
      retained[index & (RETAINED_JOBS - 1)] = job;
      index += 1;
    },
    get checksum() {
      return checksum + (retained[index & (RETAINED_JOBS - 1)]?.jobId.length ?? 0);
    },
  };
}

function benchmark(label, runner, snapshots) {
  const samples = [];
  let checksum = 0;

  for (let sample = 0; sample < SAMPLES; sample += 1) {
    const started = performance.now();
    checksum += runner(snapshots);
    samples.push(performance.now() - started);
  }

  return {
    checksum,
    label,
    mean: samples.reduce((sum, value) => sum + value, 0) / samples.length,
    min: Math.min(...samples),
    samples,
  };
}

function formatMs(value) {
  return `${value.toFixed(3)} ms`;
}

const snapshots = makeSnapshots(JOB_COUNT);

const expected = runBefore(snapshots);
const actual = runAfter(snapshots);
if (expected !== actual) {
  throw new Error(`checksum mismatch: before=${expected}, after=${actual}`);
}

// Warm both paths before measuring.
for (let index = 0; index < 10; index += 1) {
  runBefore(snapshots);
  runAfter(snapshots);
}

const before = benchmark("before map + normalize each batch", runBefore, snapshots);
const after = benchmark("after visitor normalization", runAfter, snapshots);

if (before.checksum !== after.checksum) {
  throw new Error(
    `benchmark checksum mismatch: before=${before.checksum}, after=${after.checksum}`,
  );
}

const speedup = before.mean / after.mean;
const timeSaved = before.mean - after.mean;

console.log(
  `Runtime getJobs normalization benchmark (${JOB_COUNT} jobs, ${ITERATIONS} batches/sample, ${SAMPLES} samples)`,
);
console.log(`${before.label}: mean ${formatMs(before.mean)}, min ${formatMs(before.min)}`);
console.log(`${after.label}: mean ${formatMs(after.mean)}, min ${formatMs(after.min)}`);
console.log(
  `speedup time: ${speedup.toFixed(2)}x, ${formatMs(timeSaved)} faster per ${ITERATIONS} batches`,
);
console.log(`samples before: ${before.samples.map(formatMs).join(", ")}`);
console.log(`samples after: ${after.samples.map(formatMs).join(", ")}`);
