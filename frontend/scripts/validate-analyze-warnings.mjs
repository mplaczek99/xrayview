// Validation test: applyAnalyzeJob must surface both backend analyzer warnings.
// Runs with: node frontend/scripts/validate-analyze-warnings.mjs
//
// The Rust analyzer (backend-rs/src/analysis.rs) appends two distinct
// substrings to the result's `mode` string when detection is unreliable:
//   * "; no reliable tooth mask found" — fires when tooth detection is weak
//   * "; no reliable bone level found" — fires when bone detection is weak
//
// applyAnalyzeJob in frontend/src/app/store/applyJob.ts must distinguish all
// four combinations (none/tooth-only/bone-only/both) so the clinician sees a
// warning whenever the overlay is suspect — previously the bone-only case
// silently fell through to the generic success message.
//
// This file inlines the status-derivation logic from applyAnalyzeJob so the
// validation runs without TypeScript/Vite tooling, mirroring the project's
// other validate-*.mjs scripts.

import assert from "node:assert/strict";
import { test } from "node:test";

function deriveAnalyzeStatus(mode, fromCache) {
  const toothUnreliable = mode.includes("no reliable tooth mask");
  const boneUnreliable = mode.includes("no reliable bone level");
  if (toothUnreliable && boneUnreliable) {
    return "Analysis completed, but no reliable tooth mask or bone level was found.";
  }
  if (toothUnreliable) {
    return "Analysis completed, but no reliable tooth mask was found.";
  }
  if (boneUnreliable) {
    return "Analysis completed, but no reliable bone level was found.";
  }
  if (fromCache) {
    return "Tooth and bone level overlay loaded from cache.";
  }
  return "Tooth and bone level overlay generated.";
}

test("clean mode produces generic success status", () => {
  assert.equal(
    deriveAnalyzeStatus("dynamic tooth and bone level overlay", false),
    "Tooth and bone level overlay generated.",
  );
});

test("clean mode from cache produces cache-hit status", () => {
  assert.equal(
    deriveAnalyzeStatus("dynamic tooth and bone level overlay", true),
    "Tooth and bone level overlay loaded from cache.",
  );
});

test("tooth-only warning surfaces the tooth message", () => {
  assert.equal(
    deriveAnalyzeStatus(
      "dynamic tooth and bone level overlay; no reliable tooth mask found",
      false,
    ),
    "Analysis completed, but no reliable tooth mask was found.",
  );
});

test("bone-only warning surfaces the bone message (the regression case)", () => {
  assert.equal(
    deriveAnalyzeStatus(
      "dynamic tooth and bone level overlay; no reliable bone level found",
      false,
    ),
    "Analysis completed, but no reliable bone level was found.",
  );
});

test("both warnings surface a combined message", () => {
  assert.equal(
    deriveAnalyzeStatus(
      "dynamic tooth and bone level overlay; no reliable tooth mask found; no reliable bone level found",
      false,
    ),
    "Analysis completed, but no reliable tooth mask or bone level was found.",
  );
});

test("warnings override fromCache so cache hits still surface them", () => {
  assert.equal(
    deriveAnalyzeStatus("dynamic tooth and bone level overlay; no reliable bone level found", true),
    "Analysis completed, but no reliable bone level was found.",
  );
});
