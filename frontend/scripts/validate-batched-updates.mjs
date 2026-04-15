// Validation test for step 9.3: Batch setStudyState updates via queueMicrotask.
// Runs with: node frontend/scripts/validate-batched-updates.mjs
//
// Tests that multiple synchronous setState calls within the same microtask
// are coalesced into a single listener notification, reducing React reconciliation
// work when several rapid state changes occur together.

import { test } from "node:test";
import assert from "node:assert/strict";

// Flush all currently-queued microtasks. A single await Promise.resolve() resolves
// after all earlier queueMicrotask callbacks have fired (microtasks run in FIFO order).
function flushMicrotasks() {
  return new Promise((resolve) => queueMicrotask(resolve));
}

// ---------------------------------------------------------------------------
// Minimal store implementations — mirror WorkbenchStore logic.
// ---------------------------------------------------------------------------

function makeState(overrides = {}) {
  return { counter: 0, label: "initial", ...overrides };
}

// BEFORE: synchronous listener notification on every state change.
function makeStore_BEFORE(initial) {
  let state = makeState(initial);
  let listenerCount = 0;

  function setState(updater) {
    const next = updater(state);
    if (next === state) return;
    state = next;
    listenerCount++; // mirrors iterating listeners in real store
  }

  return {
    setState,
    getState: () => state,
    getListenerCount: () => listenerCount,
    resetListenerCount: () => { listenerCount = 0; },
  };
}

// AFTER: deferred listener notification via queueMicrotask batching.
function makeStore_AFTER(initial) {
  let state = makeState(initial);
  let listenerCount = 0;
  let pendingNotification = false;

  function setState(updater) {
    const next = updater(state);
    if (next === state) return;
    state = next;

    if (!pendingNotification) {
      pendingNotification = true;
      queueMicrotask(() => {
        pendingNotification = false; // reset BEFORE iterating (re-entrancy safe)
        listenerCount++;
      });
    }
  }

  return {
    setState,
    getState: () => state,
    getListenerCount: () => listenerCount,
    resetListenerCount: () => { listenerCount = 0; },
  };
}

// ---------------------------------------------------------------------------
// BEFORE tests — baseline: every synchronous setState fires a listener.
// ---------------------------------------------------------------------------

test("BEFORE: single setState fires listener immediately (synchronously)", () => {
  const store = makeStore_BEFORE();
  store.setState((s) => ({ ...s, counter: 1 }));
  assert.equal(store.getListenerCount(), 1, "BEFORE: 1 setState → 1 listener notification");
});

test("BEFORE: 3 synchronous setState calls fire 3 listener notifications", () => {
  const store = makeStore_BEFORE();
  store.setState((s) => ({ ...s, counter: 1 }));
  store.setState((s) => ({ ...s, counter: 2 }));
  store.setState((s) => ({ ...s, counter: 3 }));
  assert.equal(store.getListenerCount(), 3, "BEFORE: 3 rapid updates → 3 notifications");
});

test("BEFORE: no-op setState (same ref) does not fire listener", () => {
  const store = makeStore_BEFORE();
  store.setState((s) => s); // no-op, returns same ref
  assert.equal(store.getListenerCount(), 0, "BEFORE: no-op → 0 notifications");
});

// ---------------------------------------------------------------------------
// AFTER tests — batched: multiple synchronous calls coalesced to 1 notification.
// ---------------------------------------------------------------------------

test("AFTER: single setState fires 0 notifications immediately, 1 after flush", async () => {
  const store = makeStore_AFTER();
  store.setState((s) => ({ ...s, counter: 1 }));
  assert.equal(store.getListenerCount(), 0, "AFTER: notification not yet fired before microtask flush");
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: 1 notification after flush");
});

test("AFTER: 3 synchronous setState calls coalesce to 1 notification after flush", async () => {
  const store = makeStore_AFTER();
  store.setState((s) => ({ ...s, counter: 1 }));
  store.setState((s) => ({ ...s, counter: 2 }));
  store.setState((s) => ({ ...s, counter: 3 }));
  assert.equal(store.getListenerCount(), 0, "AFTER: 0 notifications before microtask flush");
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: 3 rapid updates → exactly 1 batched notification");
  assert.equal(store.getState().counter, 3, "AFTER: final state is from last update");
});

test("AFTER: no-op setState does not queue notification", async () => {
  const store = makeStore_AFTER();
  store.setState((s) => s); // no-op
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 0, "AFTER: no-op → 0 notifications even after flush");
});

test("AFTER: state is updated synchronously even before flush", () => {
  const store = makeStore_AFTER();
  store.setState((s) => ({ ...s, counter: 42 }));
  // State must be readable immediately — useSyncExternalStore depends on this.
  assert.equal(store.getState().counter, 42, "AFTER: state updated sync, readable before listener fires");
});

test("AFTER: mix of ops and no-ops — only real changes trigger notification", async () => {
  const store = makeStore_AFTER();
  store.setState((s) => s);              // no-op
  store.setState((s) => ({ ...s, counter: 1 })); // real change, queues microtask
  store.setState((s) => s);              // no-op, flag already set
  store.setState((s) => ({ ...s, counter: 2 })); // real change, flag already set
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: mixed ops → 1 batched notification");
  assert.equal(store.getState().counter, 2, "AFTER: state reflects all real changes");
});

test("AFTER: second batch after flush queues new microtask", async () => {
  const store = makeStore_AFTER();

  // First batch
  store.setState((s) => ({ ...s, counter: 1 }));
  store.setState((s) => ({ ...s, counter: 2 }));
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: first batch → 1 notification");

  // Second batch (new synchronous burst after first flush)
  store.setState((s) => ({ ...s, counter: 3 }));
  store.setState((s) => ({ ...s, counter: 4 }));
  assert.equal(store.getListenerCount(), 1, "AFTER: second batch not yet fired");
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 2, "AFTER: second batch → 1 more notification (2 total)");
  assert.equal(store.getState().counter, 4, "AFTER: final state correct");
});

test("AFTER: polling scenario — 3 concurrent job updates coalesce to 1 notification", async () => {
  // Models the Promise.all polling pattern from useJobs.ts (Step 10.3 context):
  // when 3 job fetch promises resolve in the same microtask, all 3 receiveJobUpdate
  // calls happen synchronously → should produce 1 listener notification.
  const store = makeStore_AFTER();

  // Simulate 3 receiveJobUpdate calls triggered synchronously by Promise.all resolution
  store.setState((s) => ({ ...s, label: "job1-updated" }));
  store.setState((s) => ({ ...s, counter: s.counter + 1 }));
  store.setState((s) => ({ ...s, label: "job3-updated" }));

  assert.equal(store.getListenerCount(), 0, "AFTER: 3 job updates → 0 immediate notifications");
  await flushMicrotasks();
  assert.equal(store.getListenerCount(), 1, "AFTER: 3 concurrent job updates → 1 batched notification");
  assert.equal(store.getState().label, "job3-updated", "AFTER: final state correct");
  assert.equal(store.getState().counter, 1, "AFTER: all updates applied");
});
