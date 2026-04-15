// Validation test for step 9.1: Memoized selector results.
// Runs with: node frontend/scripts/validate-selectors.mjs
// Tests both BEFORE (unmemoized) and AFTER (memoized) invocation counts.

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Minimal WorkbenchState shape needed for selector testing
// ---------------------------------------------------------------------------
function makeState({ jobs = {}, activeStudyId = null, studies = {} } = {}) {
  return { jobs, activeStudyId, studies };
}

function makeJob(id, state) {
  return { jobId: id, state, jobKind: "renderStudy", studyId: null, progress: { percent: 0 }, fromCache: false, result: null, error: null, timing: null };
}

// ---------------------------------------------------------------------------
// BEFORE: unmemoized implementations (mirrors original code)
// ---------------------------------------------------------------------------
function selectPendingJobCount_BEFORE(s) {
  return Object.values(s.jobs).filter(
    (job) => job.state === "queued" || job.state === "running" || job.state === "cancelling",
  ).length;
}

function selectActiveStudy_BEFORE(s) {
  return s.activeStudyId ? s.studies[s.activeStudyId] ?? null : null;
}

// ---------------------------------------------------------------------------
// AFTER: memoization helpers (mirrors implementation in workbenchStore.ts)
// ---------------------------------------------------------------------------
function createSelector(inputSelector, resultFn) {
  let lastInput;
  let lastResult;
  let initialized = false;
  return (s) => {
    const input = inputSelector(s);
    if (initialized && Object.is(lastInput, input)) {
      return lastResult;
    }
    lastInput = input;
    lastResult = resultFn(input);
    initialized = true;
    return lastResult;
  };
}

function createSelector2(selA, selB, resultFn) {
  let lastA;
  let lastB;
  let lastResult;
  let initialized = false;
  return (s) => {
    const a = selA(s);
    const b = selB(s);
    if (initialized && Object.is(lastA, a) && Object.is(lastB, b)) {
      return lastResult;
    }
    lastA = a;
    lastB = b;
    lastResult = resultFn(a, b);
    initialized = true;
    return lastResult;
  };
}

// ---------------------------------------------------------------------------
// selectPendingJobCount tests
// ---------------------------------------------------------------------------

test("BEFORE selectPendingJobCount: filter runs on every call (same input)", () => {
  let filterCallCount = 0;
  const countBefore = (s) => {
    filterCallCount++;
    return Object.values(s.jobs).filter(
      (job) => job.state === "queued" || job.state === "running" || job.state === "cancelling",
    ).length;
  };

  const jobs = { j1: makeJob("j1", "running"), j2: makeJob("j2", "done") };
  const state = makeState({ jobs });

  // Call 5 times with the same state object
  for (let i = 0; i < 5; i++) countBefore(state);

  // BEFORE: filter runs every single call — 5 invocations
  assert.equal(filterCallCount, 5, "BEFORE: expected filter to run 5 times for 5 calls");
});

test("AFTER selectPendingJobCount: filter runs once when jobs ref unchanged", () => {
  let filterCallCount = 0;
  const selectPendingJobCount_AFTER = createSelector(
    (s) => s.jobs,
    (jobs) => {
      filterCallCount++;
      return Object.values(jobs).filter(
        (job) => job.state === "queued" || job.state === "running" || job.state === "cancelling",
      ).length;
    },
  );

  const jobs = { j1: makeJob("j1", "running"), j2: makeJob("j2", "done") };
  const state = makeState({ jobs });

  // Simulate 5 calls with same jobs ref (e.g. non-job state changes like study updates)
  for (let i = 0; i < 5; i++) selectPendingJobCount_AFTER(state);

  // AFTER: filter runs only once — memoized on jobs ref
  assert.equal(filterCallCount, 1, "AFTER: expected filter to run exactly once for 5 calls with same jobs ref");
});

test("AFTER selectPendingJobCount: recomputes when jobs ref changes", () => {
  let filterCallCount = 0;
  const selectPendingJobCount_AFTER = createSelector(
    (s) => s.jobs,
    (jobs) => {
      filterCallCount++;
      return Object.values(jobs).filter(
        (job) => job.state === "queued" || job.state === "running" || job.state === "cancelling",
      ).length;
    },
  );

  const jobs1 = { j1: makeJob("j1", "running") };
  const jobs2 = { j1: makeJob("j1", "done") };   // new reference — job finished

  selectPendingJobCount_AFTER(makeState({ jobs: jobs1 }));
  selectPendingJobCount_AFTER(makeState({ jobs: jobs1 })); // same ref, skipped
  selectPendingJobCount_AFTER(makeState({ jobs: jobs2 })); // new ref, recomputed
  selectPendingJobCount_AFTER(makeState({ jobs: jobs2 })); // same ref, skipped

  assert.equal(filterCallCount, 2, "AFTER: expected 2 filter runs for 2 distinct jobs refs");
});

test("AFTER selectPendingJobCount: returns correct count", () => {
  const sel = createSelector(
    (s) => s.jobs,
    (jobs) =>
      Object.values(jobs).filter(
        (job) => job.state === "queued" || job.state === "running" || job.state === "cancelling",
      ).length,
  );

  const jobs = {
    j1: makeJob("j1", "running"),
    j2: makeJob("j2", "queued"),
    j3: makeJob("j3", "done"),
    j4: makeJob("j4", "cancelling"),
  };
  assert.equal(sel(makeState({ jobs })), 3);
  assert.equal(sel(makeState({ jobs: {} })), 0);
});

// ---------------------------------------------------------------------------
// selectActiveStudy tests
// ---------------------------------------------------------------------------

test("BEFORE selectActiveStudy: lookup runs on every call (same inputs)", () => {
  let callCount = 0;
  const selectActiveStudy_traced = (s) => {
    callCount++;
    return s.activeStudyId ? s.studies[s.activeStudyId] ?? null : null;
  };

  const study = { studyId: "s1", inputName: "scan.dcm" };
  const state = makeState({ activeStudyId: "s1", studies: { s1: study } });

  for (let i = 0; i < 5; i++) selectActiveStudy_traced(state);

  assert.equal(callCount, 5, "BEFORE: body runs 5 times");
});

test("AFTER selectActiveStudy: result fn runs once when activeStudyId + studies unchanged", () => {
  let callCount = 0;
  const sel = createSelector2(
    (s) => s.activeStudyId,
    (s) => s.studies,
    (activeStudyId, studies) => {
      callCount++;
      return activeStudyId ? studies[activeStudyId] ?? null : null;
    },
  );

  const study = { studyId: "s1" };
  const studies = { s1: study };
  const state = makeState({ activeStudyId: "s1", studies });

  for (let i = 0; i < 5; i++) sel(state);

  assert.equal(callCount, 1, "AFTER: result fn runs once for 5 calls with same inputs");
});

test("AFTER selectActiveStudy: recomputes when activeStudyId changes", () => {
  let callCount = 0;
  const sel = createSelector2(
    (s) => s.activeStudyId,
    (s) => s.studies,
    (activeStudyId, studies) => {
      callCount++;
      return activeStudyId ? studies[activeStudyId] ?? null : null;
    },
  );

  const study1 = { studyId: "s1" };
  const study2 = { studyId: "s2" };
  const studies = { s1: study1, s2: study2 };

  sel(makeState({ activeStudyId: "s1", studies }));
  sel(makeState({ activeStudyId: "s1", studies })); // no change, skip
  sel(makeState({ activeStudyId: "s2", studies })); // activeStudyId changed, recompute
  sel(makeState({ activeStudyId: "s2", studies })); // no change, skip

  assert.equal(callCount, 2, "AFTER: 2 computations for 2 distinct activeStudyId values");
});

test("AFTER selectActiveStudy: returns same object reference when cached", () => {
  const sel = createSelector2(
    (s) => s.activeStudyId,
    (s) => s.studies,
    (activeStudyId, studies) =>
      activeStudyId ? studies[activeStudyId] ?? null : null,
  );

  const study = { studyId: "s1" };
  const studies = { s1: study };

  const r1 = sel(makeState({ activeStudyId: "s1", studies }));
  const r2 = sel(makeState({ activeStudyId: "s1", studies }));

  assert.equal(r1, r2, "AFTER: same object reference returned from cache");
  assert.equal(r1, study, "AFTER: returned value is the actual study object");
});

test("AFTER selectActiveStudy: recomputes when studies ref changes (other study updated)", () => {
  let callCount = 0;
  const sel = createSelector2(
    (s) => s.activeStudyId,
    (s) => s.studies,
    (activeStudyId, studies) => {
      callCount++;
      return activeStudyId ? studies[activeStudyId] ?? null : null;
    },
  );

  const study1 = { studyId: "s1" };
  const study2 = { studyId: "s2" };
  const studies1 = { s1: study1, s2: study2 };
  // Simulate receiveJobUpdate spreading studies (even if s1 unchanged, studies ref is new)
  const studies2 = { ...studies1, s2: { ...study2, status: "updated" } };

  sel(makeState({ activeStudyId: "s1", studies: studies1 }));
  sel(makeState({ activeStudyId: "s1", studies: studies2 })); // new studies ref

  assert.equal(callCount, 2, "AFTER: recomputes on new studies ref");
});
