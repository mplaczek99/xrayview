import { performance } from "node:perf_hooks";

const ITERATIONS = 5_000_000;
const SAMPLES = 9;
const singletonJobIds = ["job-001"];
const duplicateJobIds = ["job-001", "job-002", "job-001", "job-003"];

function beforeAlwaysDedupe(jobIds) {
  return [...new Set(jobIds)];
}

function afterSingletonFastPath(jobIds) {
  if (jobIds.length < 2) {
    return jobIds;
  }

  return [...new Set(jobIds)];
}

function measure(label, fn, jobIds) {
  const samples = [];
  let checksum = 0;

  for (let sample = 0; sample < SAMPLES; sample += 1) {
    const start = performance.now();
    for (let iteration = 0; iteration < ITERATIONS; iteration += 1) {
      checksum += fn(jobIds).length;
    }
    samples.push(performance.now() - start);
  }

  const mean = samples.reduce((sum, sample) => sum + sample, 0) / samples.length;
  const min = Math.min(...samples);
  return { label, mean, min, checksum };
}

const beforeSingleton = measure(
  "before singleton batch payload",
  beforeAlwaysDedupe,
  singletonJobIds,
);
const afterSingleton = measure(
  "after singleton batch payload",
  afterSingletonFastPath,
  singletonJobIds,
);

const beforeDuplicate = beforeAlwaysDedupe(duplicateJobIds);
const afterDuplicate = afterSingletonFastPath(duplicateJobIds);
if (JSON.stringify(beforeDuplicate) !== JSON.stringify(afterDuplicate)) {
  throw new Error(
    `duplicate behavior changed: ${JSON.stringify(afterDuplicate)} != ${JSON.stringify(
      beforeDuplicate,
    )}`,
  );
}

if (beforeSingleton.checksum !== afterSingleton.checksum) {
  throw new Error("singleton benchmark checksum mismatch");
}

const meanSpeedup = beforeSingleton.mean / afterSingleton.mean;
const meanSaved = beforeSingleton.mean - afterSingleton.mean;
const minSpeedup = beforeSingleton.min / afterSingleton.min;
const minSaved = beforeSingleton.min - afterSingleton.min;

console.log(
  `Singleton getJobs ID prep benchmark (${ITERATIONS} iterations/sample, ${SAMPLES} samples)`,
);
console.log(
  `${beforeSingleton.label}: mean ${beforeSingleton.mean.toFixed(3)} ms, min ${beforeSingleton.min.toFixed(3)} ms`,
);
console.log(
  `${afterSingleton.label}:  mean ${afterSingleton.mean.toFixed(3)} ms, min ${afterSingleton.min.toFixed(3)} ms`,
);
console.log(
  `Speedup: mean ${meanSpeedup.toFixed(2)}x (${meanSaved.toFixed(
    3,
  )} ms faster/sample), min ${minSpeedup.toFixed(2)}x (${minSaved.toFixed(3)} ms faster/sample)`,
);
