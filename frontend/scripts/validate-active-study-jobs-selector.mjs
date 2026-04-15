// Validation test for step 9.6: Fine-Grained Store Selectors to Reduce ViewTab Re-renders.
// Runs with: node frontend/scripts/validate-active-study-jobs-selector.mjs
//
// Tests that selectActiveStudyJobs returns the same object reference when an
// unrelated study's job updates (cross-study poll churn), and a new reference
// only when the active study's own job snapshot changes.
//
// BEFORE: selectJobs(s) = s.jobs. Any job update → new jobs ref → ViewTab re-renders.
// AFTER:  selectActiveStudyJobs(s) compares specific job snapshot refs. Cross-study
//         updates return the same result ref → ViewTab skips re-render.

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Minimal fixture helpers
// ---------------------------------------------------------------------------
function makeJob(id, studyId, state = "running", percent = 0) {
  return {
    jobId: id,
    jobKind: "renderStudy",
    studyId,
    state,
    progress: { percent, stage: "working", message: "In progress" },
    fromCache: false,
    result: null,
    error: null,
    timing: null,
  };
}

function makeStudy(studyId, renderJobId = null, analysisJobId = null) {
  return { studyId, renderJobId, analysisJobId };
}

function makeState({ activeStudyId = null, studies = {}, jobs = {} } = {}) {
  return { activeStudyId, studies, jobs };
}

// ---------------------------------------------------------------------------
// selectActiveStudy — mirrors workbenchStore.ts createSelector2 implementation
// ---------------------------------------------------------------------------
function createSelector2(selA, selB, resultFn) {
  let lastA, lastB, lastResult;
  let initialized = false;
  return (s) => {
    const a = selA(s);
    const b = selB(s);
    if (initialized && Object.is(lastA, a) && Object.is(lastB, b)) return lastResult;
    lastA = a; lastB = b;
    lastResult = resultFn(a, b);
    initialized = true;
    return lastResult;
  };
}

const selectActiveStudy = createSelector2(
  (s) => s.activeStudyId,
  (s) => s.studies,
  (activeStudyId, studies) => activeStudyId ? studies[activeStudyId] ?? null : null,
);

// ---------------------------------------------------------------------------
// BEFORE: selectJobs — returns s.jobs directly
// ---------------------------------------------------------------------------
function selectJobs_BEFORE(s) {
  return s.jobs;
}

// Simulate ViewTab computing analysisJob from the full jobs map
function getAnalysisJob_BEFORE(s) {
  const study = selectActiveStudy(s);
  const jobs = selectJobs_BEFORE(s);
  return study?.analysisJobId ? jobs[study.analysisJobId] ?? null : null;
}

// ---------------------------------------------------------------------------
// AFTER: selectActiveStudyJobs — IIFE closure comparing specific job refs
// ---------------------------------------------------------------------------
function makeSelectActiveStudyJobs() {
  let lastRender = null;
  let lastAnalysis = null;
  let lastResult = { render: null, analysis: null };
  let initialized = false;
  return (s) => {
    const study = selectActiveStudy(s);
    const renderJob = study?.renderJobId ? s.jobs[study.renderJobId] ?? null : null;
    const analysisJob = study?.analysisJobId ? s.jobs[study.analysisJobId] ?? null : null;
    if (initialized && Object.is(renderJob, lastRender) && Object.is(analysisJob, lastAnalysis)) {
      return lastResult;
    }
    lastRender = renderJob;
    lastAnalysis = analysisJob;
    lastResult = { render: renderJob, analysis: analysisJob };
    initialized = true;
    return lastResult;
  };
}

// ---------------------------------------------------------------------------
// BEFORE tests — baseline: full jobs map subscription causes re-render on every poll
// ---------------------------------------------------------------------------

test("BEFORE: any job update creates new jobs ref → ViewTab-equivalent re-renders", () => {
  const activeStudy = makeStudy("s1", "j-render-1", "j-analysis-1");
  const otherStudy = makeStudy("s2", "j-render-2", null);
  const j1 = makeJob("j-render-1", "s1", "running", 10);
  const j2 = makeJob("j-render-2", "s2", "running", 20);
  const ja = makeJob("j-analysis-1", "s1", "running", 0);

  const state1 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy, s2: otherStudy },
    jobs: { "j-render-1": j1, "j-render-2": j2, "j-analysis-1": ja },
  });

  // Simulate polling: study s2's render job advances (unrelated to active study)
  const j2_updated = makeJob("j-render-2", "s2", "running", 30);
  const state2 = makeState({
    activeStudyId: "s1",
    studies: { ...state1.studies },  // same study refs
    jobs: { ...state1.jobs, "j-render-2": j2_updated },  // new jobs ref, only s2 changed
  });

  const jobs1 = selectJobs_BEFORE(state1);
  const jobs2 = selectJobs_BEFORE(state2);

  // BEFORE: jobs ref always changes → different ref → ViewTab re-renders
  assert.notEqual(jobs1, jobs2, "BEFORE: different jobs refs even though active study unchanged");
});

test("BEFORE: active study analysis job update fires re-render (expected)", () => {
  const activeStudy = makeStudy("s1", null, "j-analysis-1");
  const ja1 = makeJob("j-analysis-1", "s1", "running", 50);
  const ja2 = makeJob("j-analysis-1", "s1", "running", 75);

  const state1 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-analysis-1": ja1 },
  });
  const state2 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-analysis-1": ja2 },
  });

  const r1 = getAnalysisJob_BEFORE(state1);
  const r2 = getAnalysisJob_BEFORE(state2);

  assert.notEqual(r1, r2, "BEFORE: active job update produces new job ref (expected re-render)");
  assert.equal(r1?.progress.percent, 50);
  assert.equal(r2?.progress.percent, 75);
});

// ---------------------------------------------------------------------------
// AFTER tests — fine-grained selector suppresses cross-study re-renders
// ---------------------------------------------------------------------------

test("AFTER: cross-study job update returns same result ref → ViewTab skips re-render", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const activeStudy = makeStudy("s1", "j-render-1", "j-analysis-1");
  const otherStudy = makeStudy("s2", "j-render-2", null);
  const j1 = makeJob("j-render-1", "s1", "running", 10);
  const j2 = makeJob("j-render-2", "s2", "running", 20);
  const ja = makeJob("j-analysis-1", "s1", "running", 0);

  const state1 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy, s2: otherStudy },
    jobs: { "j-render-1": j1, "j-render-2": j2, "j-analysis-1": ja },
  });

  // Only s2's job advances; active study's j1 and ja refs are unchanged
  const j2_updated = makeJob("j-render-2", "s2", "running", 30);
  const state2 = makeState({
    activeStudyId: "s1",
    studies: { ...state1.studies },
    jobs: { ...state1.jobs, "j-render-2": j2_updated },
  });

  const r1 = selectActiveStudyJobs(state1);
  const r2 = selectActiveStudyJobs(state2);

  // AFTER: same ref returned → ViewTab does NOT re-render
  assert.equal(r1, r2, "AFTER: same result ref when only cross-study job updated");
  assert.equal(r1.render, j1, "render job snapshot is j1");
  assert.equal(r1.analysis, ja, "analysis job snapshot is ja");
});

test("AFTER: active render job update returns new result ref → ViewTab re-renders", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const activeStudy = makeStudy("s1", "j-render-1", null);
  const j1 = makeJob("j-render-1", "s1", "running", 10);
  const j1_updated = makeJob("j-render-1", "s1", "running", 50);

  const state1 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-render-1": j1 },
  });
  const state2 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-render-1": j1_updated },
  });

  const r1 = selectActiveStudyJobs(state1);
  const r2 = selectActiveStudyJobs(state2);

  assert.notEqual(r1, r2, "AFTER: new result ref when active render job snapshot changes");
  assert.equal(r2.render, j1_updated, "render field reflects updated snapshot");
});

test("AFTER: active analysis job update returns new result ref → ViewTab re-renders", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const activeStudy = makeStudy("s1", null, "j-analysis-1");
  const ja1 = makeJob("j-analysis-1", "s1", "running", 30);
  const ja2 = makeJob("j-analysis-1", "s1", "running", 80);

  const state1 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-analysis-1": ja1 },
  });
  const state2 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-analysis-1": ja2 },
  });

  const r1 = selectActiveStudyJobs(state1);
  const r2 = selectActiveStudyJobs(state2);

  assert.notEqual(r1, r2, "AFTER: new result ref when active analysis job snapshot changes");
  assert.equal(r2.analysis?.progress.percent, 80);
});

test("AFTER: no active study → stable null result ref", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const state = makeState({ activeStudyId: null, studies: {}, jobs: {} });

  const r1 = selectActiveStudyJobs(state);
  const r2 = selectActiveStudyJobs(state);
  const r3 = selectActiveStudyJobs(makeState({ activeStudyId: null, studies: {}, jobs: { "j-x": makeJob("j-x", "s9") } }));

  assert.equal(r1, r2, "AFTER: same ref on repeated calls with no active study");
  assert.equal(r2, r3, "AFTER: stable null result even when unrelated jobs added");
  assert.equal(r1.render, null);
  assert.equal(r1.analysis, null);
});

test("AFTER: active study switch returns new result ref", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const study1 = makeStudy("s1", "j1", null);
  const study2 = makeStudy("s2", "j2", null);
  const j1 = makeJob("j1", "s1", "completed", 100);
  const j2 = makeJob("j2", "s2", "running", 20);

  const state1 = makeState({
    activeStudyId: "s1",
    studies: { s1: study1, s2: study2 },
    jobs: { j1, j2 },
  });
  const state2 = makeState({
    activeStudyId: "s2",
    studies: { s1: study1, s2: study2 },
    jobs: { j1, j2 },
  });

  const r1 = selectActiveStudyJobs(state1);
  const r2 = selectActiveStudyJobs(state2);

  assert.notEqual(r1, r2, "AFTER: new result ref on active study switch");
  assert.equal(r1.render, j1);
  assert.equal(r2.render, j2);
});

test("AFTER: 10 polls of other study, 0 new refs for active study selector", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const activeStudy = makeStudy("s1", "j-render-1", "j-analysis-1");
  const j1 = makeJob("j-render-1", "s1", "running", 5);
  const ja = makeJob("j-analysis-1", "s1", "running", 0);
  const baseJobs = { "j-render-1": j1, "j-analysis-1": ja };

  const initialState = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: baseJobs,
  });
  const r0 = selectActiveStudyJobs(initialState);

  let refChangeCount = 0;
  let prev = r0;

  for (let i = 0; i < 10; i++) {
    // Other study's job advances each poll (new job object each time)
    const otherJob = makeJob("j-other", "s2", "running", i * 5);
    const state = makeState({
      activeStudyId: "s1",
      studies: { s1: activeStudy },
      jobs: { ...baseJobs, "j-other": otherJob },  // new jobs spread each iteration
    });
    const r = selectActiveStudyJobs(state);
    if (r !== prev) refChangeCount++;
    prev = r;
  }

  assert.equal(refChangeCount, 0, "AFTER: 0 new result refs across 10 cross-study polls");
});

test("AFTER: mixed scenario — 10 polls, 8 cross-study, 2 active-study advances", () => {
  const selectActiveStudyJobs = makeSelectActiveStudyJobs();

  const activeStudy = makeStudy("s1", "j-render-1", null);
  let j1 = makeJob("j-render-1", "s1", "running", 0);

  const state0 = makeState({
    activeStudyId: "s1",
    studies: { s1: activeStudy },
    jobs: { "j-render-1": j1 },
  });
  let prev = selectActiveStudyJobs(state0);
  let refChangeCount = 0;

  for (let i = 1; i <= 10; i++) {
    const otherJob = makeJob("j-other", "s2", "running", i * 3);
    // Polls 3 and 7: active study's render job advances
    if (i === 3) j1 = makeJob("j-render-1", "s1", "running", 40);
    if (i === 7) j1 = makeJob("j-render-1", "s1", "running", 80);
    const state = makeState({
      activeStudyId: "s1",
      studies: { s1: activeStudy },
      jobs: { "j-render-1": j1, "j-other": otherJob },
    });
    const r = selectActiveStudyJobs(state);
    if (r !== prev) refChangeCount++;
    prev = r;
  }

  assert.equal(refChangeCount, 2, "AFTER: exactly 2 new result refs for 2 active-study advances in 10 polls");
});
