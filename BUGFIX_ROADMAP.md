• # Bug Fix Roadmap

  ## Executive Summary

  - Current backend tests and the frontend production build pass, but the highest-risk defects are mostly workflow, data-
    correctness, and platform-behavior gaps that the current test suite does not cover.
  - The biggest risk areas are file-output safety, cache identity/invalidation, DICOM decode/export correctness, and a few async/
    state-flow problems on the frontend.
  - This codebase does not need a rewrite, but it does need a moderate refactor around three seams: safe file I/O, cache/source
    provenance, and image decode/export policy.

  ## Severity Overview

  ### Critical

  #### Unsafe output path collisions can overwrite the source study or cross-write preview/output files

  Title: Unsafe output path collisions can overwrite the source study or cross-write preview/output files
  Severity: Critical
  Location: backend/src/app/mod.rs:84, backend/src/preview.rs:61, backend/src/save.rs:28, backend/src/bin/xrayview-backend.rs:103
  What is wrong: render_preview, analyze_study, and process_study accept caller-supplied write targets but never reject input ==
  preview, input == output, or preview == output. The CLI forwards those paths directly.
  Why it matters: A plain preview request can overwrite a DICOM with PNG data, and processing can overwrite the original source
  DICOM in place. That is direct user-data corruption.
  Likely root cause: Output safety is not centralized; write APIs trust caller paths instead of enforcing backend invariants.
  Recommended fix: Add canonical-path collision validation in the backend before any write; reject same-file collisions between
  input, preview, and output; route writes through temp files plus atomic rename; add regression tests for every collision
  combination.
  Estimated difficulty: Medium
  Dependencies, if any: None
  Whether it belongs in an early or later phase: Early, Phase 1

  #### Path-only cache keys can return the wrong study after a file changes in place

  Title: Path-only cache keys can return the wrong study after a file changes in place
  Severity: Critical
  Location: backend/src/app/state.rs:163, backend/src/app/state.rs:217, backend/src/app/state.rs:293, backend/src/cache/
  memory.rs:13
  What is wrong: Render, process, and analyze fingerprints are based on inputPath plus request controls. Cache validity only
  checks whether output artifacts still exist, not whether the source file at that path is still the same file.
  Why it matters: If a DICOM is replaced or regenerated at the same path during one app session, the app can silently show stale
  previews, stale tooth analysis, and stale processed output for the old file.
  Likely root cause: The cache is keyed for request deduplication, not source provenance.
  Recommended fix: Capture a source identity at open time and include it in all job fingerprints and cache entries. Minimum
  viable identity is canonical path + size + mtime; preferred identity is a content hash, with StudyInstanceUID as helper
  metadata rather than the sole key.
  Estimated difficulty: Medium
  Dependencies, if any: None
  Whether it belongs in an early or later phase: Early, Phase 1

  ### High

  #### Compare-mode exports preserve invalid calibration metadata after geometry changes

  Title: Compare-mode exports preserve invalid calibration metadata after geometry changes
  Severity: High
  Location: backend/src/processing/pipeline.rs:17, backend/src/study/source_image.rs:15, backend/src/save.rs:122
  What is wrong: Compare mode builds a side-by-side image with doubled width, but export still preserves the original pixel-
  spacing calibration tags from the source study.
  Why it matters: The derived DICOM claims physical spacing that no longer matches the exported image geometry. Any downstream
  measurement or calibration-aware viewer can be misled.
  Likely root cause: Metadata preservation is unconditional and does not account for geometry-changing transforms.
  Recommended fix: Make export metadata mode-aware. For compare outputs, strip calibration/pixel-spacing tags unless geometry is
  explicitly recomputed and still meaningful. Add a regression test that asserts compare exports do not preserve the original
  spacing blindly.
  Estimated difficulty: Medium
  Dependencies, if any: Safer output plumbing from Phase 1 makes this easier to implement cleanly
  Whether it belongs in an early or later phase: Early, Phase 2

  #### Linux packaged window is undecorated with no replacement controls

  Title: Linux packaged window is undecorated with no replacement controls
  Severity: High
  Location: frontend/src-tauri/src/main.rs:146, frontend/src/app/App.tsx:83
  What is wrong: The Linux shell disables native window decorations, but the frontend does not implement a custom title bar, drag
  region, or close/minimize/maximize controls.
  Why it matters: The packaged Linux app can become awkward or impossible to move and manage normally, which is effectively a
  release blocker on that platform.
  Likely root cause: The shell layer changed window behavior without a matching UI-shell implementation.
  Recommended fix: Either keep native decorations on Linux or add a custom draggable title bar with explicit window controls and
  platform smoke tests.
  Estimated difficulty: Medium
  Dependencies, if any: None
  Whether it belongs in an early or later phase: Early, Phase 2

  #### DICOM decode correctness for non-little-endian and compressed inputs needs verification

  Title: DICOM decode correctness for non-little-endian and compressed inputs needs verification
  Severity: High
  Location: backend/src/study/source_image.rs:168, backend/src/study/source_image.rs:237, backend/src/study/source_image.rs:432
  What is wrong: The native fallback reads 16-bit and 32-bit raw samples as little-endian, and the encapsulated-image path
  converts through DynamicImage without carrying slope/intercept and signed-value handling through the same decode logic.
  Why it matters: Valid studies can render incorrectly or inconsistently depending on transfer syntax and encoding. The risk is
  highest for non-little-endian native data and compressed monochrome studies with nontrivial rescale metadata.
  Likely root cause: The code mixes manual sample parsing with a separate image-conversion path instead of using one transfer-
  syntax-aware pipeline end to end.
  Recommended fix: Mark this as needs verification, then add fixtures for explicit big-endian and compressed grayscale studies.
  After that, unify decode rules so endian handling, signedness, rescale slope/intercept, and default windowing are applied
  consistently.
  Estimated difficulty: High
  Dependencies, if any: Test fixtures for the unsupported/untested study variants
  Whether it belongs in an early or later phase: Early, Phase 2

  #### Cancellation can be acknowledged but still finish successfully

  Title: Cancellation can be acknowledged but still finish successfully
  Severity: High
  Location: backend/src/app/state.rs:355, backend/src/app/state.rs:426, backend/src/app/state.rs:531, backend/src/app/
  state.rs:648, backend/src/jobs/registry.rs:202
  What is wrong: Jobs only check cancellation between major phases. If the user cancels during preview writing, DICOM writing, or
  the final analysis pass, the job can still publish Completed.
  Why it matters: The UI can tell the user a cancel was requested, then still commit outputs and finish as success. That is a
  correctness and trust problem, not just a UX issue.
  Likely root cause: Cooperative cancellation boundaries are too coarse, and write/commit steps are not staged.
  Recommended fix: Re-check cancellation immediately before commit points, write to temporary targets first, and only publish
  completion after a cancellation-safe final commit.
  Estimated difficulty: Medium
  Dependencies, if any: Depends on Phase 1 output staging work
  Whether it belongs in an early or later phase: Early, Phase 2

  ### Medium

  #### Annotation measurement updates are not sequenced

  Title: Annotation measurement updates are not sequenced
  Severity: Medium
  Location: frontend/src/features/viewer/ViewerCanvas.tsx:261, frontend/src/app/store/workbenchStore.ts:676, frontend/src/lib/
  backend.ts:623
  What is wrong: Manual line create/update requests are fire-and-forget. Older async responses can overwrite newer annotation
  edits, and failed creates leave selection state pointing at an annotation that never made it into store state.
  Why it matters: Fast line edits can revert unexpectedly or leave confusing “selected but missing” state.
  Likely root cause: There is no per-annotation request versioning or optimistic-state reconciliation.
  Recommended fix: Track request versions per annotation, drop stale responses, and clear ghost selection on failed creates. If
  measurement stays purely geometric, consider measuring locally first and using the backend as validation rather than the source
  of truth.
  Estimated difficulty: Medium
  Dependencies, if any: None
  Whether it belongs in an early or later phase: Later, Phase 3

  #### Direct writes are non-atomic for derived outputs and persisted state

  Title: Direct writes are non-atomic for derived outputs and persisted state
  Severity: Medium
  Location: backend/src/save.rs:152, backend/src/preview.rs:61, backend/src/persistence/catalog.rs:89
  What is wrong: DICOM files, preview PNGs, and the recent-study catalog JSON are written directly to final paths.
  Why it matters: Crashes, power loss, or disk-full conditions can leave corrupt user outputs or broken catalog state behind.
  Likely root cause: The code uses direct convenience writes instead of a staged persistence layer.
  Recommended fix: Use sibling temp files plus atomic rename for all persistent writes. Keep the old catalog until the
  replacement is fully durable.
  Estimated difficulty: Medium
  Dependencies, if any: Shares plumbing with Phase 1 file-output safety work
  Whether it belongs in an early or later phase: Later, Phase 3

  #### Tauri job events are emitted but never consumed by the frontend

  Title: Tauri job events are emitted but never consumed by the frontend
  Severity: Medium
  Location: frontend/src-tauri/src/main.rs:117, frontend/src/features/jobs/useJobs.ts:1, frontend/src/app/store/
  workbenchStore.ts:667
  What is wrong: The backend emits job progress/completion/cancellation events, but the frontend never subscribes and instead
  polls every 1.5 seconds. Comments still assume listeners exist.
  Why it matters: Progress and cancel state are always delayed, and the codebase now has two incomplete synchronization models.
  Likely root cause: The in-process backend refactor kept the event publisher, but the frontend subscription layer was not
  finished.
  Recommended fix: Pick one model. The better path is event-driven updates with polling only as fallback reconciliation. If that
  is not worth it, remove the unused event layer and clean up the misleading assumptions.
  Estimated difficulty: Medium
  Dependencies, if any: None
  Whether it belongs in an early or later phase: Later, Phase 3

  ### Low

  #### Viewer edge clamping allows coordinates outside the real image bounds

  Title: Viewer edge clamping allows coordinates outside the real image bounds
  Severity: Low
  Location: frontend/src/features/viewer/viewport.ts:77, frontend/src/features/viewer/ViewerCanvas.tsx:304
  What is wrong: Viewer coordinates are clamped to width and height rather than the last valid pixel index, and hover display
  accepts the same upper bound.
  Why it matters: Edge annotations can land one pixel outside the image, and hover readouts can display impossible coordinates.
  Likely root cause: Image dimensions are treated as inclusive rather than exclusive upper bounds.
  Recommended fix: Standardize viewer boundary semantics and clamp to the last valid pixel coordinate.
  Estimated difficulty: Low
  Dependencies, if any: None
  Whether it belongs in an early or later phase: Later, Phase 4

  #### Job and study state accumulate indefinitely in long sessions

  Title: Job and study state accumulate indefinitely in long sessions
  Severity: Low
  Location: backend/src/jobs/registry.rs:20, backend/src/study/registry.rs:10, frontend/src/app/store/workbenchStore.ts:35
  What is wrong: Terminal jobs, job timing samples, and opened studies are append-only. There is no close lifecycle or retention
  policy.
  Why it matters: Long-lived sessions can accumulate avoidable memory and state noise, and later cleanup becomes harder because
  nothing owns object lifetimes.
  Likely root cause: The store/registry design assumes short sessions and no explicit study closure.
  Recommended fix: Add bounded retention for terminal jobs, a study-close lifecycle, and cleanup hooks that trim history without
  breaking the active workbench.
  Estimated difficulty: Medium
  Dependencies, if any: Best done after the event/state model is stabilized
  Whether it belongs in an early or later phase: Later, Phase 4

  ## Ordered Implementation Phases

  ### Phase 1: Critical Stabilization

  #### 1. Centralize output path safety and staged writes

  What to change: Add one backend path-validation layer that rejects input/preview/output collisions and stage preview/DICOM
  writes through temp files before rename.
  Why it must happen first: It removes the only direct source-data-destruction path in the repository.
  What risk it removes: Overwriting the source DICOM, cross-writing preview/output targets, and partially written user outputs.
  What later work depends on it: Cancellation correctness, compare-export metadata fixes, and broader persistence hardening.

  #### 2. Re-key caches on source identity instead of source path

  What to change: Store a source identity when a study is opened and use it in render/process/analyze fingerprints and cache
  validation.
  Why it must happen first: It prevents the app from silently returning the wrong image or analysis for a changed file.
  What risk it removes: Stale previews, stale tooth measurements, and stale processed results being presented as current truth.
  What later work depends on it: Reliable compare-export validation, meaningful cache tests, and any future persistent caching.

  ### Phase 2: High Priority Reliability Fixes

  #### 1. Repair export metadata policy for geometry-changing outputs

  What to change: Make export metadata conditional on output mode, especially compare mode, and stop preserving source spacing/
  calibration when the derived image geometry changes.
  Why it must happen first: Export correctness is a user-trust issue, and compare output is currently structurally misdescribed.
  What risk it removes: Invalid downstream measurements and misleading calibration metadata in exported DICOMs.
  What later work depends on it: Any broader export refactor or future derived-output variants.

  #### 2. Fix or constrain unsupported DICOM decode paths

  What to change: Add fixtures for big-endian and compressed grayscale studies, then unify decode handling so endianness,
  signedness, rescale slope/intercept, and default window behavior are consistent.
  Why it must happen first: The app’s core value is correct image interpretation; unsupported-but-accepted studies should not
  silently render wrong.
  What risk it removes: Incorrect preview intensity, inconsistent processing behavior, and hard-to-debug modality-dependent
  failures.
  What later work depends on it: More trustworthy processing presets, analysis behavior, and export validation.

  #### 3. Restore a usable Linux desktop shell

  What to change: Re-enable native decorations on Linux or add proper custom window chrome and drag regions.
  Why it must happen first: It is a platform-level reliability issue for packaged desktop use.
  What risk it removes: Linux release builds that are hard to move, close, or manage normally.
  What later work depends on it: Any serious Linux release validation or user-facing desktop polish.

  #### 4. Tighten cancellation around commit points

  What to change: Re-check cancellation immediately before final preview/DICOM commit and avoid publishing success after a late
  cancel request.
  Why it must happen first: Users need cancellation to be truthful before more incremental UI work is worth doing.
  What risk it removes: “Cancelled” jobs that still finish and commit outputs.
  What later work depends on it: Cleaner job UX and simpler event/state synchronization.

  ### Phase 3: Structural Hardening

  #### 1. Sequence annotation measurement requests and reconcile optimistic state

  What to change: Add per-annotation request versioning, stale-response dropping, and explicit failure cleanup for create/update
  flows.
  Why it must happen first: The current edit path is small but fragile and can regress into stale geometry under real
  interaction.
  What risk it removes: Out-of-order measurement writes, ghost selections, and hard-to-reproduce annotation glitches.
  What later work depends on it: Any richer annotation editing or persistence feature.

  #### 2. Choose a single job synchronization model

  What to change: Implement frontend subscriptions for emitted Tauri job events and keep polling only as fallback, or remove the
  event channel entirely and simplify around polling.
  Why it must happen first: The current split model is already causing misleading comments and delayed state.
  What risk it removes: Laggy progress, delayed cancellation feedback, and hidden synchronization assumptions.
  What later work depends on it: Job retention cleanup, cleaner status-bar logic, and easier debugging.

  #### 3. Finish persistence hardening for internal state

  What to change: Extend staged-write behavior to catalog persistence and any other app-managed durable files.
  Why it must happen first: Once user-output safety is fixed, internal durability should follow the same rules.
  What risk it removes: Corrupt recent-study state after interrupted writes.
  What later work depends on it: Any future persistence expansion, such as restoring recent studies or session state.

  ### Phase 4: Edge Cases and Cleanup

  #### 1. Fix viewer boundary semantics

  What to change: Clamp coordinates to valid pixel indices and align hover/annotation helpers to the same inclusive/exclusive
  rules.
  Why it must happen after the earlier phases: It is real but low-risk compared with file safety and result correctness.
  What risk it removes: One-pixel edge drift and impossible coordinate readouts.
  What later work depends on it: Cleaner annotation math if more tools are added later.

  #### 2. Add retention and close semantics for jobs and studies

  What to change: Bound terminal-job retention, prune stale timing data, and add explicit study-close behavior.
  Why it must happen after the earlier phases: It is mainly a long-session stability issue, not an immediate correctness blocker.
  What risk it removes: Unbounded state growth and increasingly noisy workbench state.
  What later work depends on it: Multi-study workflows and any serious session-management features.

  ## Recommended Execution Order

  Start by building two shared foundations before patching isolated symptoms.

  First, create a backend file-output safety layer that canonicalizes paths, rejects collisions, and stages writes through temp
  files. Do this before fixing compare exports or cancellation, because both problems are much easier and safer once output
  commits are explicit.

  Second, refactor cache identity so it is tied to source identity rather than source path. Do this before touching any higher-
  level cache behavior, because every render/process/analyze fix depends on being able to trust what “same study” means.

  After those two refactors, implement compare-export metadata rules, repair unsupported DICOM decode paths, and restore Linux
  window usability. Then tighten cancellation semantics, because cancellation behavior should be defined around the new staged-
  write model rather than patched around direct writes.

  Finish with frontend state-flow hardening: sequence annotation requests, wire job events properly, then trim job/study
  retention and clean up edge-boundary math.

  ## Shared Root Causes

  The first recurring root cause is that file and cache identity are path-based shortcuts rather than explicit provenance models.
  That causes both the destructive path-collision bug and the stale-cache bug.

  The second recurring root cause is that output policy is not centralized. Preview writing, DICOM export, metadata preservation,
  and persistence all make local decisions, which is why compare exports, cancellation semantics, and atomicity problems all
  spread across multiple modules.

  The third recurring root cause is a split imaging pipeline. Some studies go through manual sample parsing and some go through
  DynamicImage conversion, so transfer syntax, signedness, and rescale logic are not enforced consistently. A focused refactor is
  smarter here than continuing to patch each encoding case separately.

  The fourth recurring root cause is async state without versioned reconciliation. The frontend uses fire-and-forget mutation
  calls plus polling, but it also has an unused event model in the backend. That is why annotation races, delayed job state, and
  comment/code mismatches exist at the same time.

  A partial rewrite is not warranted, but patching around these seams one bug at a time would be inefficient. The safer approach
  is to refactor the file-output policy, cache identity model, and decode pipeline first, then land the bug fixes on top of those
  clearer boundaries.

  ## Final Recommendation

  Best path forward: moderate refactor.

  The architecture is salvageable, and most of the code is organized well enough to keep. The right move is not a rewrite of the
  whole product, but a deliberate refactor of three shared layers: safe file I/O, source-aware caching, and DICOM decode/export
  correctness. Once those are in place, the remaining frontend async and edge-case fixes become straightforward instead of
  fragile patchwork.

  ## Next Steps

  - First action: start by writing failing regression tests for output-path collisions in the backend. Do that before touching
    implementation so the most dangerous behavior is pinned down immediately.
  - Verify before starting: confirm the branch is still clean, rerun cargo test --manifest-path backend/Cargo.toml and npm
    --prefix frontend run build, and decide what source-identity rule you want for cache keys before changing cache behavior.
  - Risks to keep in mind: do not test destructive path cases against real DICOMs, do not change cache behavior before source
    identity is defined, and treat compare-export calibration changes as potentially breaking downstream viewer expectations.
