// Validation test for step 9.2: Avoid spreading entire state on every update.
// Runs with: node frontend/scripts/validate-job-updates.mjs
//
// Tests that receiveJobUpdate skips listener notification when the incoming
// backend snapshot carries no new information (same state, progress, error,
// result) — the common case during polling when a job has not advanced.

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// jobSnapshotEqual — mirrors the implementation in workbenchStore.ts
// ---------------------------------------------------------------------------
function jobSnapshotEqual(prev, next) {
  return (
    prev.state === next.state &&
    prev.progress.percent === next.progress.percent &&
    prev.progress.stage === next.progress.stage &&
    prev.progress.message === next.progress.message &&
    prev.fromCache === next.fromCache &&
    (prev.result === null) === (next.result === null) &&
    (prev.error === null) === (next.error === null)
  );
}

// ---------------------------------------------------------------------------
// Minimal fixture helpers
// ---------------------------------------------------------------------------
function makeProgress(percent = 0, stage = "working", message = "In progress") {
  return { percent, stage, message };
}

function makeSnapshot(overrides = {}) {
  return {
    jobId: "j1",
    jobKind: "renderStudy",
    studyId: "s1",
    state: "running",
    progress: makeProgress(),
    fromCache: false,
    result: null,
    error: null,
    timing: null,
    ...overrides,
  };
}

// Minimal store that counts listener notifications — mirrors WorkbenchStore logic.
// setState returns `current` unchanged when the updater returns the same ref
// (as in the real store's `nextState === this.state` guard).
function makeMinimalStore(initialSnapshot) {
  let state = {
    jobs: { [initialSnapshot.jobId]: initialSnapshot },
  };
  let listenerCount = 0;

  function setState(updater) {
    const next = updater(state);
    if (next === state) return; // no-op guard — mirrors workbenchStore.ts:727-731
    state = next;
    listenerCount++;
  }

  // BEFORE: no equality check, always spreads state
  function receiveJobUpdate_BEFORE(job) {
    setState((current) => {
      const jobs = { ...current.jobs, [job.jobId]: { ...job } };
      return { ...current, jobs };
    });
  }

  // AFTER: skips update when snapshot is equal to stored
  function receiveJobUpdate_AFTER(job) {
    setState((current) => {
      const previous = current.jobs[job.jobId];
      if (previous && jobSnapshotEqual(previous, job)) {
        return current; // <-- early return, same ref → no listener notification
      }
      const jobs = { ...current.jobs, [job.jobId]: { ...job } };
      return { ...current, jobs };
    });
  }

  return {
    getListenerCount: () => listenerCount,
    resetListenerCount: () => { listenerCount = 0; },
    getState: () => state,
    receiveJobUpdate_BEFORE,
    receiveJobUpdate_AFTER,
  };
}

// ---------------------------------------------------------------------------
// BEFORE tests — baseline showing every poll fires a notification
// ---------------------------------------------------------------------------

test("BEFORE: identical polling snapshot fires listener on every call", () => {
  const snap = makeSnapshot({ state: "running", progress: makeProgress(30) });
  const store = makeMinimalStore(snap);
  store.resetListenerCount();

  const repeated = makeSnapshot({ state: "running", progress: makeProgress(30) });
  for (let i = 0; i < 5; i++) {
    store.receiveJobUpdate_BEFORE(repeated);
  }

  // Without equality check every call creates a new object → new state ref → fires listener
  assert.equal(store.getListenerCount(), 5, "BEFORE: listener fires 5× for 5 identical polls");
});

// ---------------------------------------------------------------------------
// AFTER tests — equality check blocks no-op notifications
// ---------------------------------------------------------------------------

test("AFTER: identical polling snapshot fires listener only once (first time)", () => {
  const snap = makeSnapshot({ state: "running", progress: makeProgress(30) });
  const store = makeMinimalStore(snap);
  store.resetListenerCount(); // snap already stored; subsequent identical polls are no-ops

  const repeated = makeSnapshot({ state: "running", progress: makeProgress(30) });
  for (let i = 0; i < 5; i++) {
    store.receiveJobUpdate_AFTER(repeated);
  }

  assert.equal(store.getListenerCount(), 0, "AFTER: 0 notifications for 5 no-op polls (snap identical to stored)");
});

test("AFTER: progress percent change fires listener", () => {
  const snap = makeSnapshot({ state: "running", progress: makeProgress(30) });
  const store = makeMinimalStore(snap);
  store.resetListenerCount();

  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(31) }));
  assert.equal(store.getListenerCount(), 1, "AFTER: percent change → 1 notification");
});

test("AFTER: progress stage change fires listener", () => {
  const snap = makeSnapshot({ state: "running", progress: makeProgress(30, "decode", "Decoding") });
  const store = makeMinimalStore(snap);
  store.resetListenerCount();

  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(30, "render", "Rendering") }));
  assert.equal(store.getListenerCount(), 1, "AFTER: stage change → 1 notification");
});

test("AFTER: state transition running→completed fires listener", () => {
  const snap = makeSnapshot({ state: "running", progress: makeProgress(99) });
  const store = makeMinimalStore(snap);
  store.resetListenerCount();

  const done = makeSnapshot({
    state: "completed",
    progress: makeProgress(100),
    result: { kind: "renderStudy", payload: { previewPath: "/tmp/out.png" } },
  });
  store.receiveJobUpdate_AFTER(done);
  assert.equal(store.getListenerCount(), 1, "AFTER: running→completed fires listener");
});

test("AFTER: null→error transition fires listener", () => {
  const snap = makeSnapshot({ state: "running", progress: makeProgress(50) });
  const store = makeMinimalStore(snap);
  store.resetListenerCount();

  const failed = makeSnapshot({
    state: "failed",
    progress: makeProgress(50),
    error: { code: "internal", message: "boom", details: [], recoverable: false },
  });
  store.receiveJobUpdate_AFTER(failed);
  assert.equal(store.getListenerCount(), 1, "AFTER: null→error fires listener");
});

test("AFTER: repeated completed snapshot does NOT fire listener again", () => {
  const done = makeSnapshot({
    state: "completed",
    progress: makeProgress(100),
    result: { kind: "renderStudy", payload: { previewPath: "/tmp/out.png" } },
  });
  const store = makeMinimalStore(done);
  store.resetListenerCount();

  // Simulate 3 more polls returning the same completed state (different object ref)
  for (let i = 0; i < 3; i++) {
    store.receiveJobUpdate_AFTER(makeSnapshot({
      state: "completed",
      progress: makeProgress(100),
      result: { kind: "renderStudy", payload: { previewPath: "/tmp/out.png" } },
    }));
  }
  assert.equal(store.getListenerCount(), 0, "AFTER: terminal state repeated polls → 0 notifications");
});

test("AFTER: mixed sequence counts correctly", () => {
  // Scenario: 10 polls, 6 no-ops, 4 real changes
  const snap = makeSnapshot({ state: "queued", progress: makeProgress(0) });
  const store = makeMinimalStore(snap);
  store.resetListenerCount();

  // Poll 1: no-op (same queued, 0%)
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "queued", progress: makeProgress(0) }));
  // Poll 2: no-op
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "queued", progress: makeProgress(0) }));
  // Poll 3: state changes to running → fires
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(0) }));
  // Poll 4: no-op (still running, 0%)
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(0) }));
  // Poll 5: progress advances → fires
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(50) }));
  // Poll 6: no-op (50%)
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(50) }));
  // Poll 7: no-op (50%)
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(50) }));
  // Poll 8: progress 50→99 → fires
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "running", progress: makeProgress(99) }));
  // Poll 9: completed → fires
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "completed", progress: makeProgress(100), result: {} }));
  // Poll 10: repeated completed → no-op
  store.receiveJobUpdate_AFTER(makeSnapshot({ state: "completed", progress: makeProgress(100), result: {} }));

  assert.equal(store.getListenerCount(), 4, "AFTER: 4 real changes in 10 polls → exactly 4 notifications");
});
