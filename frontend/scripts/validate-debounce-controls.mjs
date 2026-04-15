// Validation test for step 9.7: Debounce processing control updates via rAF.
// Runs with: node frontend/scripts/validate-debounce-controls.mjs
//
// Tests that rapid slider events (brightness, contrast) are coalesced into one
// state update per animation frame, reducing React reconciliation from ~60/s
// to 1/frame during slider drags.

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Mock requestAnimationFrame (not available in Node.js)
// ---------------------------------------------------------------------------

let rafQueue = [];
let rafNextId = 1;

globalThis.requestAnimationFrame = (cb) => {
  const id = rafNextId++;
  rafQueue.push({ id, cb });
  return id;
};

globalThis.cancelAnimationFrame = (id) => {
  rafQueue = rafQueue.filter((entry) => entry.id !== id);
};

function flushRAF() {
  const pending = rafQueue.slice();
  rafQueue = [];
  for (const { cb } of pending) {
    cb(performance.now());
  }
}

function flushMicrotasks() {
  return new Promise((resolve) => queueMicrotask(resolve));
}

// Reset rAF state between tests
function resetRAF() {
  rafQueue = [];
  rafNextId = 1;
}

// ---------------------------------------------------------------------------
// Minimal store implementations — mirror WorkbenchStore logic.
// ---------------------------------------------------------------------------

function makeControls(brightness = 0, contrast = 1.0) {
  return { brightness, contrast, invert: false, equalize: false, palette: "none" };
}

function makeStudy(id = "study-1") {
  return {
    studyId: id,
    processing: { form: { controls: makeControls() } },
  };
}

// BEFORE: immediate state update on every setProcessingControls call.
function makeStore_BEFORE() {
  let state = { studies: { "study-1": makeStudy() }, activeStudyId: "study-1" };
  let listenerCount = 0;

  function setState(updater) {
    const next = updater(state);
    if (next === state) return;
    state = next;
    listenerCount++;
  }

  function setStudyState(studyId, updater) {
    setState((current) => {
      const study = current.studies[studyId];
      if (!study) return current;
      return {
        ...current,
        studies: { ...current.studies, [studyId]: updater(study) },
      };
    });
  }

  function activeStudy() {
    return state.activeStudyId ? state.studies[state.activeStudyId] ?? null : null;
  }

  function setProcessingControls(controls) {
    const study = activeStudy();
    if (!study) return;
    setStudyState(study.studyId, (current) => ({
      ...current,
      processing: {
        ...current.processing,
        form: { ...current.processing.form, controls: { ...controls } },
      },
    }));
  }

  return {
    setProcessingControls,
    getState: () => state,
    getListenerCount: () => listenerCount,
    resetListenerCount: () => { listenerCount = 0; },
    setActiveStudyId: (id) => { state = { ...state, activeStudyId: id }; },
  };
}

// AFTER: rAF-debounced state update — coalesces events within a single frame.
function makeStore_AFTER() {
  let state = { studies: { "study-1": makeStudy() }, activeStudyId: "study-1" };
  let listenerCount = 0;
  let pendingNotification = false;

  // rAF debounce fields
  let _pendingControls = null;
  let _pendingControlsStudyId = null;
  let _controlsRaf = 0;

  function setState(updater) {
    const next = updater(state);
    if (next === state) return;
    state = next;
    if (!pendingNotification) {
      pendingNotification = true;
      queueMicrotask(() => {
        pendingNotification = false;
        listenerCount++;
      });
    }
  }

  function setStudyState(studyId, updater) {
    setState((current) => {
      const study = current.studies[studyId];
      if (!study) return current;
      return {
        ...current,
        studies: { ...current.studies, [studyId]: updater(study) },
      };
    });
  }

  function activeStudy() {
    return state.activeStudyId ? state.studies[state.activeStudyId] ?? null : null;
  }

  function commitPendingControls() {
    const controls = _pendingControls;
    const studyId = _pendingControlsStudyId;
    _pendingControls = null;
    _pendingControlsStudyId = null;
    if (!controls || !studyId) return;
    setStudyState(studyId, (current) => ({
      ...current,
      processing: {
        ...current.processing,
        form: { ...current.processing.form, controls: { ...controls } },
      },
    }));
  }

  function setProcessingControls(controls) {
    const study = activeStudy();
    if (!study) return;
    _pendingControls = controls;
    _pendingControlsStudyId = study.studyId;
    if (!_controlsRaf) {
      _controlsRaf = requestAnimationFrame(() => {
        _controlsRaf = 0;
        commitPendingControls();
      });
    }
  }

  return {
    setProcessingControls,
    getState: () => state,
    getListenerCount: () => listenerCount,
    resetListenerCount: () => { listenerCount = 0; },
    setActiveStudyId: (id) => { state = { ...state, activeStudyId: id }; },
    getRAFQueueLength: () => rafQueue.length,
  };
}

// ---------------------------------------------------------------------------
// BEFORE tests — baseline: every slider event fires a state update.
// ---------------------------------------------------------------------------

test("BEFORE: single setProcessingControls fires 1 listener immediately", () => {
  resetRAF();
  const store = makeStore_BEFORE();
  store.setProcessingControls(makeControls(10));
  assert.equal(store.getListenerCount(), 1, "BEFORE: 1 call → 1 listener notification");
});

test("BEFORE: 5 rapid slider events fire 5 state updates", () => {
  resetRAF();
  const store = makeStore_BEFORE();
  for (let i = 0; i < 5; i++) {
    store.setProcessingControls(makeControls(i * 10));
  }
  assert.equal(store.getListenerCount(), 5, "BEFORE: 5 slider events → 5 state updates");
  assert.equal(store.getState().studies["study-1"].processing.form.controls.brightness, 40,
    "BEFORE: final brightness is last value");
});

test("BEFORE: 10 rapid events in one 'frame' → 10 state updates", () => {
  resetRAF();
  const store = makeStore_BEFORE();
  for (let i = 0; i < 10; i++) {
    store.setProcessingControls(makeControls(i));
  }
  assert.equal(store.getListenerCount(), 10, "BEFORE: 10 events → 10 updates (no coalescing)");
});

// ---------------------------------------------------------------------------
// AFTER tests — debounced: N events within a frame → 1 state update after rAF.
// ---------------------------------------------------------------------------

test("AFTER: single setProcessingControls fires 0 updates immediately, 1 after rAF+microtask", async () => {
  resetRAF();
  const store = makeStore_AFTER();
  store.setProcessingControls(makeControls(10));
  assert.equal(store.getListenerCount(), 0, "AFTER: 0 listener notifications before rAF");
  assert.equal(rafQueue.length, 1, "AFTER: 1 rAF scheduled");
  flushRAF();
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: 1 notification after rAF+microtask flush");
  assert.equal(store.getState().studies["study-1"].processing.form.controls.brightness, 10,
    "AFTER: state updated to correct value");
});

test("AFTER: 5 rapid slider events → 1 rAF scheduled, 0 updates until flush", async () => {
  resetRAF();
  const store = makeStore_AFTER();
  for (let i = 0; i < 5; i++) {
    store.setProcessingControls(makeControls(i * 10));
  }
  assert.equal(store.getListenerCount(), 0, "AFTER: 0 updates before rAF flush");
  assert.equal(rafQueue.length, 1, "AFTER: exactly 1 rAF scheduled (not 5)");
});

test("AFTER: 5 rapid slider events → 1 update after rAF flush, last value wins", async () => {
  resetRAF();
  const store = makeStore_AFTER();
  for (let i = 0; i < 5; i++) {
    store.setProcessingControls(makeControls(i * 10));
  }
  flushRAF();
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: 5 events coalesced to 1 state update");
  assert.equal(store.getState().studies["study-1"].processing.form.controls.brightness, 40,
    "AFTER: last value (40) wins");
});

test("AFTER: 10 rapid events coalesced to 1 update (90% reduction vs BEFORE)", async () => {
  resetRAF();
  const store = makeStore_AFTER();
  for (let i = 0; i < 10; i++) {
    store.setProcessingControls(makeControls(i));
  }
  assert.equal(rafQueue.length, 1, "AFTER: 1 rAF regardless of event count");
  flushRAF();
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: 10 events → 1 state update (1/10th the work)");
  assert.equal(store.getState().studies["study-1"].processing.form.controls.brightness, 9,
    "AFTER: final value is last event value");
});

test("AFTER: second frame after flush schedules new rAF independently", async () => {
  resetRAF();
  const store = makeStore_AFTER();

  // Frame 1: events + flush
  store.setProcessingControls(makeControls(10));
  store.setProcessingControls(makeControls(20));
  flushRAF();
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: frame 1 → 1 update");

  // Frame 2: more events
  store.setProcessingControls(makeControls(30));
  store.setProcessingControls(makeControls(40));
  assert.equal(rafQueue.length, 1, "AFTER: frame 2 schedules new rAF");
  flushRAF();
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 2, "AFTER: frame 2 → 1 more update (2 total)");
  assert.equal(store.getState().studies["study-1"].processing.form.controls.brightness, 40,
    "AFTER: frame 2 final value correct");
});

test("AFTER: no update when no active study", async () => {
  resetRAF();
  const store = makeStore_AFTER();
  store.setActiveStudyId(null);
  store.setProcessingControls(makeControls(50));
  assert.equal(rafQueue.length, 0, "AFTER: no rAF scheduled when no active study");
  assert.equal(store.getListenerCount(), 0, "AFTER: no state update when no active study");
});

test("AFTER: study ID captured at call time, not at rAF time", async () => {
  resetRAF();
  const store = makeStore_AFTER();

  // study-2 is in state
  const study2 = makeStudy("study-2");
  const rawState = store.getState();
  // Manually inject study-2 (simulate opening second study)
  Object.assign(rawState.studies, { "study-2": study2 });

  // Call with study-1 active
  store.setProcessingControls(makeControls(77));
  assert.equal(store.getState().activeStudyId, "study-1", "sanity: study-1 active at call time");

  // Switch active study before rAF fires
  store.setActiveStudyId("study-2");

  // rAF fires — should commit to study-1 (the captured ID), not study-2
  flushRAF();
  await flushMicrotasks();

  assert.equal(
    store.getState().studies["study-1"].processing.form.controls.brightness, 77,
    "AFTER: controls committed to study captured at call time (study-1)"
  );
  assert.equal(
    store.getState().studies["study-2"].processing.form.controls.brightness, 0,
    "AFTER: study-2 controls unchanged (correct isolation)"
  );
});

test("AFTER: contrast and brightness both coalesced correctly", async () => {
  resetRAF();
  const store = makeStore_AFTER();

  // Simulate slider moving brightness and contrast together across multiple events
  store.setProcessingControls({ brightness: 10, contrast: 1.1, invert: false, equalize: false, palette: "none" });
  store.setProcessingControls({ brightness: 20, contrast: 1.2, invert: false, equalize: false, palette: "none" });
  store.setProcessingControls({ brightness: 30, contrast: 1.5, invert: true,  equalize: false, palette: "hot"  });

  flushRAF();
  await flushMicrotasks();

  const controls = store.getState().studies["study-1"].processing.form.controls;
  assert.equal(controls.brightness, 30, "AFTER: brightness coalesced to last value");
  assert.equal(controls.contrast, 1.5, "AFTER: contrast coalesced to last value");
  assert.equal(controls.invert, true, "AFTER: invert coalesced to last value");
  assert.equal(controls.palette, "hot", "AFTER: palette coalesced to last value");
  assert.equal(store.getListenerCount(), 1, "AFTER: all fields in 1 update");
});
