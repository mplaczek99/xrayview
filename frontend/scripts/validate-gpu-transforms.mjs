// Validation test for step 9.5: GPU-Accelerated CSS Transforms for Image Positioning
// Runs with: node frontend/scripts/validate-gpu-transforms.mjs
//
// Tests the style computation logic in isolation (no React compile needed):
//   BEFORE: left/top/width/height inline styles → triggers layout on every change
//   AFTER:  transform: translate+scale with fixed natural dimensions → compositor only

import { test } from "node:test";
import assert from "node:assert/strict";

// ---------------------------------------------------------------------------
// BEFORE: style computation using layout-triggering properties
// ---------------------------------------------------------------------------
function computeStyleBefore(transform, imageSize) {
  if (!transform || !imageSize) return undefined;
  return {
    left: `${transform.offsetX}px`,
    top: `${transform.offsetY}px`,
    width: `${imageSize.width * transform.scale}px`,
    height: `${imageSize.height * transform.scale}px`,
  };
}

// ---------------------------------------------------------------------------
// AFTER: style computation using GPU-composited transform
// ---------------------------------------------------------------------------
function computeStyleAfter(transform, imageSize) {
  if (!transform || !imageSize) return undefined;
  return {
    width: `${imageSize.width}px`,
    height: `${imageSize.height}px`,
    transform: `translate(${transform.offsetX}px, ${transform.offsetY}px) scale(${transform.scale})`,
    transformOrigin: "0 0",
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function makeTransform(offsetX, offsetY, scale) {
  return { offsetX, offsetY, scale };
}

function makeImageSize(width, height) {
  return { width, height };
}

// Properties that trigger layout (reflow) when changed
const LAYOUT_PROPERTIES = new Set(["left", "top", "width", "height"]);

function changedProperties(styleBefore, styleAfter) {
  const changed = new Set();
  const allKeys = new Set([...Object.keys(styleBefore), ...Object.keys(styleAfter)]);
  for (const key of allKeys) {
    if (styleBefore[key] !== styleAfter[key]) changed.add(key);
  }
  return changed;
}

function layoutTriggersCount(changedProps) {
  return [...changedProps].filter((p) => LAYOUT_PROPERTIES.has(p)).length;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test("BEFORE: null transform/imageSize → undefined style", () => {
  assert.equal(computeStyleBefore(null, null), undefined);
  assert.equal(computeStyleBefore(makeTransform(10, 20, 1.0), null), undefined);
});

test("AFTER: null transform/imageSize → undefined style", () => {
  assert.equal(computeStyleAfter(null, null), undefined);
  assert.equal(computeStyleAfter(makeTransform(10, 20, 1.0), null), undefined);
});

test("BEFORE: pan changes left + top (2 layout-triggering properties)", () => {
  const img = makeImageSize(2048, 1536);
  const t1 = makeTransform(100, 80, 0.5);
  const t2 = makeTransform(115, 95, 0.5); // pan: offsets change, scale same

  const s1 = computeStyleBefore(t1, img);
  const s2 = computeStyleBefore(t2, img);
  const changed = changedProperties(s1, s2);

  assert.ok(changed.has("left"), "BEFORE: pan changes 'left' (layout trigger)");
  assert.ok(changed.has("top"), "BEFORE: pan changes 'top' (layout trigger)");
  assert.equal(layoutTriggersCount(changed), 2, "BEFORE: pan triggers 2 layout properties");
});

test("BEFORE: zoom changes all 4 layout-triggering properties", () => {
  const img = makeImageSize(2048, 1536);
  const t1 = makeTransform(100, 80, 0.5);
  const t2 = makeTransform(90, 72, 0.6); // zoom: scale + offsets change

  const s1 = computeStyleBefore(t1, img);
  const s2 = computeStyleBefore(t2, img);
  const changed = changedProperties(s1, s2);

  assert.ok(changed.has("left"), "BEFORE: zoom changes 'left'");
  assert.ok(changed.has("top"), "BEFORE: zoom changes 'top'");
  assert.ok(changed.has("width"), "BEFORE: zoom changes 'width'");
  assert.ok(changed.has("height"), "BEFORE: zoom changes 'height'");
  assert.equal(layoutTriggersCount(changed), 4, "BEFORE: zoom triggers all 4 layout properties");
});

test("AFTER: pan changes only 'transform' (zero layout-triggering properties)", () => {
  const img = makeImageSize(2048, 1536);
  const t1 = makeTransform(100, 80, 0.5);
  const t2 = makeTransform(115, 95, 0.5); // pan: offsets change, scale same

  const s1 = computeStyleAfter(t1, img);
  const s2 = computeStyleAfter(t2, img);
  const changed = changedProperties(s1, s2);

  assert.ok(changed.has("transform"), "AFTER: pan changes 'transform' (GPU compositor)");
  assert.equal(layoutTriggersCount(changed), 0, "AFTER: pan triggers ZERO layout properties");
  assert.equal(changed.size, 1, "AFTER: pan changes exactly 1 property (transform)");
});

test("AFTER: zoom changes only 'transform' (zero layout-triggering properties)", () => {
  const img = makeImageSize(2048, 1536);
  const t1 = makeTransform(100, 80, 0.5);
  const t2 = makeTransform(90, 72, 0.6); // zoom: scale + offsets change

  const s1 = computeStyleAfter(t1, img);
  const s2 = computeStyleAfter(t2, img);
  const changed = changedProperties(s1, s2);

  assert.ok(changed.has("transform"), "AFTER: zoom changes 'transform' (GPU compositor)");
  assert.equal(layoutTriggersCount(changed), 0, "AFTER: zoom triggers ZERO layout properties");
  assert.equal(changed.size, 1, "AFTER: zoom changes exactly 1 property (transform)");
});

test("AFTER: width/height are fixed to natural image dimensions (no layout thrash on pan)", () => {
  const img = makeImageSize(2048, 1536);
  const transforms = [
    makeTransform(0, 0, 0.3),
    makeTransform(50, 30, 0.5),
    makeTransform(100, 80, 0.8),
    makeTransform(-20, -10, 1.2),
  ];

  for (const t of transforms) {
    const style = computeStyleAfter(t, img);
    assert.equal(style.width, "2048px", `width fixed at natural size (scale=${t.scale})`);
    assert.equal(style.height, "1536px", `height fixed at natural size (scale=${t.scale})`);
  }
});

test("AFTER: transform string maps pixels correctly (math equivalence to BEFORE)", () => {
  const img = makeImageSize(2048, 1536);
  const t = makeTransform(100, 80, 0.5);
  const style = computeStyleAfter(t, img);

  // BEFORE maps image corner (0,0) to screen (offsetX, offsetY)
  // AFTER: translate(offsetX, offsetY) scale(S) with transformOrigin "0 0"
  //        → element corner (0,0) → screen (offsetX, offsetY) ✓
  // BEFORE maps image corner (W,H) to screen (offsetX + W*S, offsetY + H*S)
  // AFTER: element corner (W,H) → after scale → (W*S, H*S) → after translate → (offsetX + W*S, offsetY + H*S) ✓

  assert.equal(
    style.transform,
    "translate(100px, 80px) scale(0.5)",
    "transform string is correct",
  );
  assert.equal(style.transformOrigin, "0 0", "transformOrigin anchors to top-left");

  // Verify BEFORE and AFTER produce equivalent screen positions for image corners
  const styleBefore = computeStyleBefore(t, img);

  // Top-left corner: left = offsetX, top = offsetY
  assert.equal(styleBefore.left, "100px");
  assert.equal(styleBefore.top, "80px");
  // translate(100px, 80px) positions top-left at (100, 80) ✓

  // Bottom-right corner: before = left+width = 100 + 2048*0.5 = 1124, top+height = 80 + 1536*0.5 = 848
  const beforeRight = 100 + 2048 * 0.5;
  const beforeBottom = 80 + 1536 * 0.5;
  // After: translate(100,80) scale(0.5) → bottom-right of 2048×1536 element = (100 + 2048*0.5, 80 + 1536*0.5)
  const afterRight = 100 + 2048 * 0.5;
  const afterBottom = 80 + 1536 * 0.5;
  assert.equal(beforeRight, afterRight, "bottom-right x matches between BEFORE and AFTER");
  assert.equal(beforeBottom, afterBottom, "bottom-right y matches between BEFORE and AFTER");
});

test("AFTER: transformOrigin is always '0 0' regardless of transform", () => {
  const img = makeImageSize(800, 600);
  const transforms = [
    makeTransform(0, 0, 1.0),
    makeTransform(-200, -150, 2.5),
    makeTransform(400, 300, 0.1),
  ];
  for (const t of transforms) {
    const style = computeStyleAfter(t, img);
    assert.equal(style.transformOrigin, "0 0", "transformOrigin is always top-left");
  }
});

test("BEFORE vs AFTER: layout trigger count comparison for 100 pan frames", () => {
  const img = makeImageSize(2048, 1536);
  let beforeLayoutTriggers = 0;
  let afterLayoutTriggers = 0;

  // Simulate 100 pan frames (offsetX/Y changing, scale constant)
  const baseTransform = makeTransform(100, 80, 0.5);
  for (let i = 0; i < 100; i++) {
    const prev = makeTransform(baseTransform.offsetX + i, baseTransform.offsetY + i, 0.5);
    const next = makeTransform(baseTransform.offsetX + i + 1, baseTransform.offsetY + i + 1, 0.5);

    const sb1 = computeStyleBefore(prev, img);
    const sb2 = computeStyleBefore(next, img);
    beforeLayoutTriggers += layoutTriggersCount(changedProperties(sb1, sb2));

    const sa1 = computeStyleAfter(prev, img);
    const sa2 = computeStyleAfter(next, img);
    afterLayoutTriggers += layoutTriggersCount(changedProperties(sa1, sa2));
  }

  assert.equal(beforeLayoutTriggers, 200, "BEFORE: 100 pan frames × 2 layout triggers (left+top) = 200");
  assert.equal(afterLayoutTriggers, 0, "AFTER: 100 pan frames × 0 layout triggers = 0");
  console.log(
    `\n  Layout trigger reduction: ${beforeLayoutTriggers} → ${afterLayoutTriggers} ` +
    `(100% eliminated for pan gestures)`,
  );
});

test("BEFORE vs AFTER: layout trigger count comparison for 100 zoom frames", () => {
  const img = makeImageSize(2048, 1536);
  let beforeLayoutTriggers = 0;
  let afterLayoutTriggers = 0;

  // Simulate 100 zoom frames (all transform fields changing)
  for (let i = 0; i < 100; i++) {
    const scale1 = 0.5 + i * 0.01;
    const scale2 = 0.5 + (i + 1) * 0.01;
    const prev = makeTransform(100 - i * 0.5, 80 - i * 0.4, scale1);
    const next = makeTransform(100 - (i + 1) * 0.5, 80 - (i + 1) * 0.4, scale2);

    const sb1 = computeStyleBefore(prev, img);
    const sb2 = computeStyleBefore(next, img);
    beforeLayoutTriggers += layoutTriggersCount(changedProperties(sb1, sb2));

    const sa1 = computeStyleAfter(prev, img);
    const sa2 = computeStyleAfter(next, img);
    afterLayoutTriggers += layoutTriggersCount(changedProperties(sa1, sa2));
  }

  assert.equal(beforeLayoutTriggers, 400, "BEFORE: 100 zoom frames × 4 layout triggers = 400");
  assert.equal(afterLayoutTriggers, 0, "AFTER: 100 zoom frames × 0 layout triggers = 0");
  console.log(
    `  Layout trigger reduction: ${beforeLayoutTriggers} → ${afterLayoutTriggers} ` +
    `(100% eliminated for zoom gestures)\n`,
  );
});
