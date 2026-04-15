// Validation test for step 9.4: Memoize AnnotationLayer with React.memo
// Runs with: node frontend/scripts/validate-annotation-memo.mjs
//
// Tests the two custom comparators in isolation (no React compile needed):
//   annotationLayerPropsEqual — controls AnnotationLayer re-renders
//   lineItemPropsEqual        — controls LineAnnotationItem re-renders

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// Mirror of annotationLayerPropsEqual from AnnotationLayer.tsx
// ---------------------------------------------------------------------------
function annotationLayerPropsEqual(prev, next) {
  return (
    prev.width === next.width &&
    prev.height === next.height &&
    prev.transform.offsetX === next.transform.offsetX &&
    prev.transform.offsetY === next.transform.offsetY &&
    prev.transform.scale === next.transform.scale &&
    Object.is(prev.annotations, next.annotations) &&
    prev.selectedAnnotationId === next.selectedAnnotationId &&
    Object.is(prev.draftLine, next.draftLine)
    // callbacks intentionally excluded
  );
}

// ---------------------------------------------------------------------------
// Mirror of lineItemPropsEqual from AnnotationLayer.tsx
// ---------------------------------------------------------------------------
function lineItemPropsEqual(prev, next) {
  return (
    Object.is(prev.annotation, next.annotation) &&
    prev.isSelected === next.isSelected &&
    prev.scale === next.scale
    // callbacks intentionally excluded
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function makeTransform(offsetX, offsetY, scale) {
  return { offsetX, offsetY, scale };
}

function makeAnnotations(lines = [], rectangles = []) {
  return { lines, rectangles };
}

function makeLine(id, x1, y1, x2, y2) {
  return {
    id,
    label: `line-${id}`,
    source: "manual",
    editable: true,
    start: { x: x1, y: y1 },
    end: { x: x2, y: y2 },
    measurement: null,
  };
}

function makeLayerProps(overrides = {}) {
  const annotations = makeAnnotations([makeLine("a1", 0, 0, 100, 100)]);
  return {
    width: 800,
    height: 600,
    transform: makeTransform(10, 20, 1.0),
    annotations,
    selectedAnnotationId: null,
    draftLine: null,
    onSelectAnnotation: () => {},
    onStartHandleDrag: () => {},
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// BEFORE: no custom comparator — React.memo uses Object.is on entire props obj
// New transform object on every render always causes re-render.
// ---------------------------------------------------------------------------

test("BEFORE: new transform object reference always triggers re-render (Object.is)", () => {
  let renderCount = 0;
  // Simulate React.memo default: re-render if any prop reference differs
  function shouldRerender_BEFORE(prev, next) {
    // React.memo default = shallow Object.is on each prop key
    for (const key of Object.keys(next)) {
      if (!Object.is(prev[key], next[key])) return true;
    }
    return false;
  }

  const base = makeLayerProps();
  // Pan: same logical values but new transform object (as ViewerCanvas creates on each setViewport)
  const afterPan = {
    ...base,
    transform: makeTransform(15, 25, 1.0), // new object, same scale, different offsets
  };
  const afterPanSameObj = { ...base }; // same transform reference

  // New transform object → re-render even though scale unchanged
  assert.ok(
    shouldRerender_BEFORE(base, afterPan),
    "BEFORE: new transform object → re-renders (can't distinguish pan from zoom)",
  );
  // Same transform reference → no re-render
  assert.ok(
    !shouldRerender_BEFORE(base, afterPanSameObj),
    "BEFORE: same transform ref → no re-render",
  );

  renderCount++;
  assert.equal(renderCount, 1); // confirm test ran
});

test("BEFORE: callback recreation always triggers re-render (Object.is)", () => {
  function shouldRerender_BEFORE(prev, next) {
    for (const key of Object.keys(next)) {
      if (!Object.is(prev[key], next[key])) return true;
    }
    return false;
  }

  const base = makeLayerProps();
  // ViewerCanvas recreates beginHandleDrag on every render (not useCallback-wrapped)
  const withFreshCallback = {
    ...base,
    onStartHandleDrag: () => {}, // new function reference, same behavior
  };

  assert.ok(
    shouldRerender_BEFORE(base, withFreshCallback),
    "BEFORE: fresh callback ref → re-renders even though nothing meaningful changed",
  );
});

// ---------------------------------------------------------------------------
// AFTER: annotationLayerPropsEqual — field-level transform comparison, skip callbacks
// ---------------------------------------------------------------------------

test("AFTER: pan (offsetX/Y change) → re-renders (SVG transform must update)", () => {
  const base = makeLayerProps({ transform: makeTransform(10, 20, 1.5) });
  const afterPan = {
    ...base,
    transform: makeTransform(15, 25, 1.5), // pan: offsets change, scale same
  };
  assert.ok(
    !annotationLayerPropsEqual(base, afterPan),
    "AFTER: pan changes offsetX/Y → NOT equal → re-renders (correct)",
  );
});

test("AFTER: zoom (scale change) → re-renders", () => {
  const base = makeLayerProps({ transform: makeTransform(10, 20, 1.0) });
  const afterZoom = {
    ...base,
    transform: makeTransform(8, 16, 1.2), // zoom: scale + offsets change
  };
  assert.ok(
    !annotationLayerPropsEqual(base, afterZoom),
    "AFTER: zoom changes scale → NOT equal → re-renders (correct)",
  );
});

test("AFTER: unrelated parent state change (e.g. hoverCoord) → skips re-render", () => {
  const base = makeLayerProps();
  // Simulate ViewerCanvas re-render from hoverCoord update — AnnotationLayer props unchanged
  const sameProps = { ...base };
  assert.ok(
    annotationLayerPropsEqual(base, sameProps),
    "AFTER: same props → equal → skips re-render (correct)",
  );
});

test("AFTER: callback recreation alone does NOT trigger re-render", () => {
  const base = makeLayerProps();
  const withFreshCallback = {
    ...base,
    onSelectAnnotation: () => {}, // new reference, same behavior
    onStartHandleDrag: () => {},
  };
  assert.ok(
    annotationLayerPropsEqual(base, withFreshCallback),
    "AFTER: fresh callback refs excluded from comparison → equal → skips re-render",
  );
});

test("AFTER: annotation change → re-renders", () => {
  const annotations1 = makeAnnotations([makeLine("a1", 0, 0, 100, 100)]);
  const annotations2 = makeAnnotations([makeLine("a1", 0, 0, 200, 200)]); // different ref
  const base = makeLayerProps({ annotations: annotations1 });
  const updated = { ...base, annotations: annotations2 };
  assert.ok(
    !annotationLayerPropsEqual(base, updated),
    "AFTER: new annotations ref → NOT equal → re-renders (correct)",
  );
});

test("AFTER: selectedAnnotationId change → re-renders", () => {
  const base = makeLayerProps({ selectedAnnotationId: null });
  const updated = { ...base, selectedAnnotationId: "a1" };
  assert.ok(
    !annotationLayerPropsEqual(base, updated),
    "AFTER: selectedAnnotationId changed → NOT equal → re-renders (correct)",
  );
});

test("AFTER: draftLine change → re-renders", () => {
  const base = makeLayerProps({ draftLine: null });
  const updated = { ...base, draftLine: makeLine("draft", 0, 0, 50, 50) };
  assert.ok(
    !annotationLayerPropsEqual(base, updated),
    "AFTER: draftLine changed → NOT equal → re-renders (correct)",
  );
});

// ---------------------------------------------------------------------------
// LineAnnotationItem comparator tests
// ---------------------------------------------------------------------------

test("BEFORE LineAnnotationItem: inline onPointerDown recreated each render → always re-renders", () => {
  // Without React.memo, the map() body runs on every AnnotationLayer render,
  // creating new inline handlers each time.  Simulate by comparing prop objects.
  function shouldRerender_BEFORE(prev, next) {
    for (const key of Object.keys(next)) {
      if (!Object.is(prev[key], next[key])) return true;
    }
    return false;
  }

  const annotation = makeLine("a1", 0, 0, 100, 100);
  const prev = { annotation, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  const next = { annotation, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} }; // fresh fn

  assert.ok(
    shouldRerender_BEFORE(prev, next),
    "BEFORE: fresh onSelectAnnotation ref → re-renders on every AnnotationLayer render",
  );
});

test("AFTER LineAnnotationItem: pan (scale unchanged, same annotation) → skips re-render", () => {
  const annotation = makeLine("a1", 0, 0, 100, 100);
  const prev = { annotation, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  // Pan: AnnotationLayer re-renders, passes same annotation ref + same scale
  const next = { annotation, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  assert.ok(
    lineItemPropsEqual(prev, next),
    "AFTER: pan → same annotation + same scale → equal → skips re-render",
  );
});

test("AFTER LineAnnotationItem: zoom (scale changed) → re-renders", () => {
  const annotation = makeLine("a1", 0, 0, 100, 100);
  const prev = { annotation, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  const next = { annotation, isSelected: false, scale: 1.5, onSelectAnnotation: () => {} };
  assert.ok(
    !lineItemPropsEqual(prev, next),
    "AFTER: zoom → scale changed → NOT equal → re-renders (label offset update)",
  );
});

test("AFTER LineAnnotationItem: annotation data change → re-renders", () => {
  const annotation1 = makeLine("a1", 0, 0, 100, 100);
  const annotation2 = makeLine("a1", 0, 0, 200, 200); // new object ref (edited annotation)
  const prev = { annotation: annotation1, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  const next = { annotation: annotation2, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  assert.ok(
    !lineItemPropsEqual(prev, next),
    "AFTER: new annotation ref → NOT equal → re-renders (correct)",
  );
});

test("AFTER LineAnnotationItem: isSelected change → re-renders", () => {
  const annotation = makeLine("a1", 0, 0, 100, 100);
  const prev = { annotation, isSelected: false, scale: 1.0, onSelectAnnotation: () => {} };
  const next = { annotation, isSelected: true, scale: 1.0, onSelectAnnotation: () => {} };
  assert.ok(
    !lineItemPropsEqual(prev, next),
    "AFTER: isSelected toggled → NOT equal → re-renders (correct)",
  );
});

test("AFTER LineAnnotationItem: only selected item re-renders on selection change", () => {
  // Simulate 5-annotation list; user selects annotation 3.
  // selectedAnnotationId changes in AnnotationLayer → all 5 items get new isSelected prop.
  // Items 1,2,4,5 (isSelected stays false) → skip.
  // Item 3 (false → true) → re-renders.
  const annotations = [
    makeLine("a1", 0, 0, 10, 10),
    makeLine("a2", 20, 20, 30, 30),
    makeLine("a3", 40, 40, 50, 50),
    makeLine("a4", 60, 60, 70, 70),
    makeLine("a5", 80, 80, 90, 90),
  ];

  let skipped = 0;
  let rerendered = 0;
  for (const ann of annotations) {
    const wasSelected = false;
    const isNowSelected = ann.id === "a3";
    const prev = { annotation: ann, isSelected: wasSelected, scale: 1.0, onSelectAnnotation: () => {} };
    const next = { annotation: ann, isSelected: isNowSelected, scale: 1.0, onSelectAnnotation: () => {} };
    if (lineItemPropsEqual(prev, next)) {
      skipped++;
    } else {
      rerendered++;
    }
  }

  assert.equal(rerendered, 1, "AFTER: only 1 item re-renders when selection changes");
  assert.equal(skipped, 4, "AFTER: 4 items skip re-render (isSelected unchanged)");
});

test("AFTER: selectedLine useMemo — find() runs once for repeated calls with same inputs", () => {
  // Mirror the useMemo([annotations.lines, selectedAnnotationId]) logic
  let findCallCount = 0;

  function createMemoizedSelectedLine() {
    let lastLines;
    let lastId;
    let lastResult;
    let initialized = false;
    return (lines, selectedId) => {
      if (initialized && Object.is(lastLines, lines) && Object.is(lastId, selectedId)) {
        return lastResult;
      }
      findCallCount++;
      lastLines = lines;
      lastId = selectedId;
      lastResult = lines.find((a) => a.id === selectedId) ?? null;
      initialized = true;
      return lastResult;
    };
  }

  const sel = createMemoizedSelectedLine();
  const lines = [makeLine("a1", 0, 0, 100, 100), makeLine("a2", 10, 10, 20, 20)];

  // Call 5 times with same inputs (e.g. AnnotationLayer re-renders on pan)
  for (let i = 0; i < 5; i++) sel(lines, "a1");

  assert.equal(findCallCount, 1, "useMemo: find() runs once for 5 calls with same lines + selectedId");

  // Selection changes → recomputes
  sel(lines, "a2");
  assert.equal(findCallCount, 2, "useMemo: find() recomputes on selectedAnnotationId change");

  // Lines ref changes → recomputes
  const newLines = [...lines];
  sel(newLines, "a2");
  assert.equal(findCallCount, 3, "useMemo: find() recomputes on new lines array ref");
});
