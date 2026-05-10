// Validation benchmark for plan step 26: fallback job polling should not stay
// on a 200ms cadence unless a recent state transition or near-complete job
// needs it.
// Runs with: node frontend/scripts/validate-fast-poll-cadence.mjs

import { performance } from "node:perf_hooks";

const DURATION_MS = Number.parseInt(
  process.env.XRAYVIEW_FAST_POLL_BENCH_DURATION_MS ?? "10000",
  10,
);
const JOB_COUNT = Number.parseInt(
  process.env.XRAYVIEW_FAST_POLL_BENCH_JOBS ?? "20000",
  10,
);
const SAMPLES = Number.parseInt(
  process.env.XRAYVIEW_FAST_POLL_BENCH_SAMPLES ?? "7",
  10,
);

const BEFORE_ACTIVE_POLL_MS = 200;
const AFTER_ACTIVE_POLL_MS = 500;
const AFTER_RECENT_TRANSITION_POLL_MS = 200;
const AFTER_MAX_POLL_MS = 2000;

function makeState(jobCount) {
  const jobs = {};
  const pendingJobIds = [];

  for (let index = 0; index < jobCount; index += 1) {
    const jobId = `job-${index}`;
    jobs[jobId] = {
      jobId,
      state: "running",
      progress: { percent: 40 + (index % 20), stage: "", message: "" },
    };
    pendingJobIds.push(jobId);
  }

  return { jobs, pendingJobIds };
}

function simulatePollWork(state) {
  const prePollState = new Map();
  let checksum = 0;

  for (const jobId of state.pendingJobIds) {
    const job = state.jobs[jobId];
    if (!job) {
      continue;
    }
    prePollState.set(jobId, {
      percent: job.progress.percent,
      state: job.state,
    });
    checksum += job.progress.percent;
  }

  let anyProgress = false;
  let anyStateTransition = false;
  let allQueued = true;
  let anyNearComplete = false;

  for (const jobId of state.pendingJobIds) {
    const job = state.jobs[jobId];
    if (!job) {
      continue;
    }
    if (job.state !== "queued") {
      allQueued = false;
    }
    if (job.state === "running" && job.progress.percent > 80) {
      anyNearComplete = true;
    }
    const pre = prePollState.get(jobId);
    if (pre !== undefined) {
      if (job.state !== pre.state) {
        anyStateTransition = true;
        anyProgress = true;
      } else if (job.progress.percent > pre.percent) {
        anyProgress = true;
      }
    }
    checksum += pre?.percent ?? 0;
  }

  return {
    allQueued,
    anyNearComplete,
    anyProgress,
    anyStateTransition,
    checksum,
  };
}

function runBefore(state) {
  let wallTimeMs = 0;
  let pollCount = 0;
  let checksum = 0;
  const intervals = [];

  while (wallTimeMs <= DURATION_MS) {
    checksum += simulatePollWork(state).checksum;
    pollCount += 1;

    const intervalMs = BEFORE_ACTIVE_POLL_MS;
    if (wallTimeMs + intervalMs > DURATION_MS) {
      break;
    }
    wallTimeMs += intervalMs;
    intervals.push(intervalMs);
  }

  return { checksum, intervals, pollCount };
}

function runAfter(state) {
  let wallTimeMs = 0;
  let pollCount = 0;
  let checksum = 0;
  let currentIntervalMs = AFTER_ACTIVE_POLL_MS;
  const intervals = [];

  while (wallTimeMs <= DURATION_MS) {
    const result = simulatePollWork(state);
    checksum += result.checksum;
    pollCount += 1;

    let intervalMs;
    if (result.anyStateTransition || result.anyNearComplete) {
      currentIntervalMs = AFTER_RECENT_TRANSITION_POLL_MS;
      intervalMs = currentIntervalMs;
    } else if (result.anyProgress) {
      currentIntervalMs = AFTER_ACTIVE_POLL_MS;
      intervalMs = currentIntervalMs;
    } else if (result.allQueued) {
      intervalMs = 1000;
    } else {
      intervalMs = currentIntervalMs;
      currentIntervalMs = Math.min(currentIntervalMs * 2, AFTER_MAX_POLL_MS);
    }

    if (wallTimeMs + intervalMs > DURATION_MS) {
      break;
    }
    wallTimeMs += intervalMs;
    intervals.push(intervalMs);
  }

  return { checksum, intervals, pollCount };
}

function benchmark(label, runner, state) {
  const samples = [];
  let checksum = 0;
  let lastRun;

  for (let sample = 0; sample < SAMPLES; sample += 1) {
    const started = performance.now();
    lastRun = runner(state);
    samples.push(performance.now() - started);
    checksum += lastRun.checksum;
  }

  return {
    checksum,
    label,
    mean: samples.reduce((sum, value) => sum + value, 0) / samples.length,
    min: Math.min(...samples),
    pollCount: lastRun.pollCount,
    intervals: lastRun.intervals,
    samples,
  };
}

function assertCadence() {
  const state = makeState(1);
  const before = runBefore(state);
  const after = runAfter(state);

  if (before.pollCount <= after.pollCount) {
    throw new Error(`expected fewer after polls, got before=${before.pollCount}, after=${after.pollCount}`);
  }
  if (after.intervals[0] !== AFTER_ACTIVE_POLL_MS) {
    throw new Error(`expected after to start at ${AFTER_ACTIVE_POLL_MS}ms, got ${after.intervals[0]}ms`);
  }

  const transitionInterval = decideAfterInterval({
    allQueued: false,
    anyNearComplete: false,
    anyProgress: true,
    anyStateTransition: true,
  });
  if (transitionInterval !== AFTER_RECENT_TRANSITION_POLL_MS) {
    throw new Error(
      `expected recent state transition interval ${AFTER_RECENT_TRANSITION_POLL_MS}ms, got ${transitionInterval}ms`,
    );
  }
}

function decideAfterInterval(result) {
  if (result.anyStateTransition || result.anyNearComplete) {
    return AFTER_RECENT_TRANSITION_POLL_MS;
  }
  if (result.anyProgress) {
    return AFTER_ACTIVE_POLL_MS;
  }
  if (result.allQueued) {
    return 1000;
  }
  return AFTER_ACTIVE_POLL_MS;
}

function formatMs(value) {
  return `${value.toFixed(3)} ms`;
}

assertCadence();

const state = makeState(JOB_COUNT);

// Warm both paths before collecting samples.
runBefore(state);
runAfter(state);

const before = benchmark("before 200ms fallback cadence", runBefore, state);
const after = benchmark("after 500ms active cadence", runAfter, state);

if (after.pollCount >= before.pollCount) {
  throw new Error(`expected fewer after polls, got before=${before.pollCount}, after=${after.pollCount}`);
}
if (before.checksum <= after.checksum) {
  throw new Error("expected before checksum to be larger because it executes more poll work");
}

const speedup = before.mean / after.mean;
const timeSaved = before.mean - after.mean;
const requestReduction = ((before.pollCount - after.pollCount) / before.pollCount) * 100;

console.log(
  `Fast poll cadence benchmark (${JOB_COUNT} pending jobs, ${DURATION_MS}ms fallback window, ${SAMPLES} samples)`,
);
console.log(`${before.label}: mean ${formatMs(before.mean)}, min ${formatMs(before.min)}, ${before.pollCount} polls`);
console.log(`${after.label}: mean ${formatMs(after.mean)}, min ${formatMs(after.min)}, ${after.pollCount} polls`);
console.log(`speedup time: ${speedup.toFixed(2)}x, ${formatMs(timeSaved)} faster per simulated window`);
console.log(`poll reduction: ${requestReduction.toFixed(1)}% (${before.pollCount} -> ${after.pollCount})`);
console.log(`after intervals: [${after.intervals.join(", ")}]`);
console.log(`samples before: ${before.samples.map(formatMs).join(", ")}`);
console.log(`samples after: ${after.samples.map(formatMs).join(", ")}`);
