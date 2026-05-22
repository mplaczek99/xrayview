# Removal Plan: Strip DICOM and TIFF, Keep Only BMP

**Status:** Draft v2 — revised 2026-05-22 after self-review
**Branch target:** new branch off `dev` (e.g. `dev/strip-dicom-tiff`)
**Author/owner:** mplaczek99
**Date drafted:** 2026-05-22

### Revision notes (v2)
- Added `ProcessStudyCommand.outputPath` input-field removal (missed in v1).
- Enumerated all 10 CLI subcommands and flagged `decode-source`, `process-preview`, `analyze-preview` as currently DICOM-coupled.
- Resolved D7: the legacy `--input` flow's DICOM-write branch is **in scope** (it can't survive a "no DICOM output" decision).
- Resolved D4: bump `x-contract-version` in place; **do not rename** the schema file. Keeps the diff focused; rename is a separate follow-up.
- Enumerated test deletions per file rather than handwaving "remove broken tests".
- Tightened DoD: opaque `.dcm` strings in `persistence.rs` tests get renamed to `.bmp` so the final grep gate stays strict.
- Softened line-count claim and effort estimate.

### Revision notes (v3)
- Re-grouped the `cli.rs:1021` encapsulation-branch bullet under `inspect_decode` (where it actually lives) and noted that `inspect_decode` itself is ~600 lines, not a small function.
- Fixed line anchors: `inspect_decode` starts at 442 (not 449); the "preview/DICOM output" help text is at line 803 (not 798).
- Committed to deleting the legacy `--output` flag entirely (`parse_legacy_args` line 98, `execute_legacy` line 175 usage, `is_plain_preview_request` line 345 check) — confirmed by grep that those were its only consumers.
- Added concrete cache-key sites to §2c: `app.rs:841` (`outputPath` in `process_fingerprint`), `app.rs:1706` (`fingerprint_output_path` helper), `app.rs:713` (`resolve_process_output_path`), call sites at 622/1104, test fixtures at 2530/2592/2635.
- Added §2g audit for the `image` crate's `jpeg` feature — confirmed by grep that `dicom.rs` is the sole consumer, so the feature can be dropped (binary-size win).
- Added a risk-table row for legacy CLI users with `--input/--output` scripts.
- Added a `version`-subcommand test note to §2b.
- Corrected the "11 frontend files" count to 12 and softened it ("approximately a dozen").
- Tightened the §1 non-goals language to not contradict the `persistence.rs` string-rename in §2f.
- Strengthened the cache-wipe risk-table mitigation from "document" to "mandatory in PR description".

---

## 1. Goal

The application's only supported input is a **single bitewing X-ray in BMP**. Remove all code, data, dependencies, UI affordances, contract fields, sample assets, and documentation that exist solely to support **DICOM** (`.dcm`, `.dicom`) or **TIFF** (`.tif`, `.tiff`).

### In scope
- DICOM Part 10 reader (file meta, transfer syntax dispatch, dataset parsing, tag constants).
- DICOM Secondary Capture writer / encoder and the `export-secondary-capture` CLI subcommand.
- TIFF decoder and all TIFF IFD parsing helpers.
- All test fixtures that build synthetic DICOM/TIFF files.
- File-dialog filters, UI strings, and contract fields that name those formats — including the input field `ProcessStudyCommand.outputPath` (vestigial once the writer is gone).
- DICOM/TIFF sample assets and references to them in docs.
- The DICOM-write branch inside the legacy `--input` CLI flow (`cli.rs::execute_legacy`, lines 265–271).
- Migration of `decode-source` / `render-preview` / `process-preview` / `analyze-preview` CLI subcommands away from DICOM-coupled entry points (they currently call `dicom::read_file` / `dicom::render_grayscale_preview_file*`).

### Out of scope (do **not** touch)
- BMP decoder and its tests.
- Preview rendering pipeline (`render.rs`), processing pipeline (`processing.rs`), analysis (`analysis.rs`), annotations, cache, persistence — these are format-agnostic.
- Tauri shell, frontend runtime selection, job/event plumbing.
- The standalone HTTP backend transport (still used by CLI/tests).

### Non-goals
- No format additions (no PNG/JPEG input).
- No code-file moves beyond `dicom.rs` → `bmp.rs` (see §6). Cosmetic string renames inside files (e.g., `.dcm` → `.bmp` in `persistence.rs` test fixtures) are explicitly allowed.
- No version bump of the application; **do** bump the contract version (see §4.1).

---

## 2. Why this is worth doing

- **Binary size & cold-start memory:** `backend-rs/src/dicom.rs` is **3,860 lines** today; the DICOM/TIFF surface is the largest single hand-rolled module in the crate. Gutting it will delete the **majority** of the file (the BMP decoder and a small format-detection shim survive). The exact line-count delta and binary-size delta will be measured post-cut and reported in the PR description.
- **Compile time:** `dicom.rs` is the slowest TU in the backend; shrinking it speeds every `cargo check` and `cargo build` in the inner loop.
- **Memory safety surface:** The DICOM Part 10 reader and TIFF IFD parser both do byte-offset arithmetic on untrusted input. Deleting them eliminates that attack surface entirely.
- **Maintenance:** No more keeping `backend-rs/src/contracts.rs` in lockstep with DICOM-specific schema fields, no more dual-codepath ("standalone image vs Part 10") branching in the entry function.
- **User clarity:** A single supported input format — bitewing BMP — removes a class of "why doesn't this open" questions.

These align with the project's stated priorities (speed, performance, memory usage, memory safety).

---

## 3. Decisions to confirm before starting

These are open questions whose answers shape the plan. Confirm before editing code.

| # | Question | Recommended answer |
|---|---|---|
| D1 | Rename `backend-rs/src/dicom.rs`? After removal only the BMP decoder + a thin format-detection shim remain. | **Yes** — rename to `bmp.rs`. Keeps intent obvious; small file. |
| D2 | What replaces the processed output? Today the pipeline writes a DICOM Secondary Capture `.dcm` and a PNG preview. With DICOM gone, do we keep the PNG only, or also write a processed BMP? | **PNG preview only.** The frontend already loads the preview via the asset protocol; no user-facing "save processed file" feature is required. Drop the `Save Processed DICOM` dialog entirely. |
| D3 | Does `ProcessStudyCommandResult.dicomPath` get removed or replaced with a generic `outputPath`? Also: `ProcessStudyCommand` has an input field `outputPath` (schema line 349, `contracts.rs:309` `output_path: Option<String>`) that the frontend uses to tell the backend where to write the DICOM. With DICOM output gone, this input is also dead. | **Both removed outright.** `dicomPath` (result field) AND `outputPath` (command field) both go. The processing pipeline now produces only the preview path, fully owned by the cache directory. |
| D4 | Bump contract version `x-contract-version` from `1` to `2`? And rename `backend-contract-v1.schema.json` → `…-v2…`? | **Bump in place; do not rename the file.** Filename rename ripples through `contracts/scripts/schema-tools.mjs:10` (hard-coded path) and the schema's `$id` URL; not worth the diff for an internal-only contract. The `1`→`2` flips the `BACKEND_CONTRACT_VERSION` constant in `frontend/src/lib/generated/contracts.ts` (line 5), `backend-rs/src/contracts.rs:4`, and three `http.rs` print sites — grep `BACKEND_CONTRACT_VERSION` for the full list. File rename is a follow-up. |
| D5 | Keep the `inspect-decode` CLI subcommand? It was primarily a DICOM-header debug tool. | **Delete it.** A BMP equivalent is trivially `xxd` / `file`. One less thing to maintain. |
| D6 | What about the other CLI subcommands — `decode-source`, `render-preview`, `process-preview`, `analyze-preview`, `export-secondary-capture`, `print-config`, `list-commands`, `version`? (See `cli.rs:39–58` for the full dispatch.) | `export-secondary-capture` → **delete** (it exists only to write DICOM). `decode-source`, `render-preview`, `process-preview`, `analyze-preview` → **keep, but migrate** their source-decode call sites from `dicom::read_file` / `dicom::render_grayscale_preview_file*` to the BMP-only equivalents. `print-config`, `list-commands`, `version`, `help` → **keep as-is**, no DICOM coupling. |
| D7 | Should the legacy compatibility flag-based CLI path (`--input`, `--preset`, …) remain? It currently writes a Secondary Capture DICOM at `cli.rs:265–271`. | **Keep the flag-based path; remove its DICOM-write branch.** The DICOM output must go to be consistent with D2; the rest of the legacy flow (loading a study, running a preset, writing the PNG preview) is format-agnostic and stays. This is in scope. |
| D8 | Tauri asset protocol scope is currently `["**"]`. Tighten it as part of this work? | **No** — separate task. CLAUDE.md already flags this. |

If any answer above flips, the corresponding step in §5 changes; nothing else in the plan should need rewriting.

---

## 4. Execution phases

Do these in order. Each phase ends at a "green gate" — a checkable build/test state — so the branch is never left in a half-broken intermediate.

### Phase 0 — Branch and baseline
1. `git switch -c dev/strip-dicom-tiff` off `dev`.
2. `npm install` from repo root (chains `frontend` workspace).
3. Capture a baseline so we have a comparison point for the "why" claims:
   - `cargo build --manifest-path backend-rs/Cargo.toml --release` → record `ls -l backend-rs/target/release/...` size of any libs/bins.
   - `cargo build --manifest-path backend-rs/Cargo.toml --release --timings` → save `cargo-timing.html`.
   - `cloc backend-rs/src` (or `wc -l backend-rs/src/*.rs`) → save line counts.
4. **Green gate:** `npm run release:smoke` passes on the unmodified branch.

### Phase 1 — Contracts (schema → Rust → TS)
The contract is the source of truth and the rest of the codebase fans out from it. Touch this first; everything downstream is a compile error to chase.

1. **Schema** — `contracts/backend-contract-v1.schema.json` (keep the v1 filename per D4)
   - Line 6: bump `x-contract-version` from `1` to `2`. Do **not** rename the file or touch the `$id` URL; that ripple (`schema-tools.mjs:10` is hard-coded) is a separate follow-up.
   - Line 349–351: delete the `outputPath` property from `ProcessStudyCommand.properties` (input field, per D3).
   - Line 377: delete `"dicomPath": { "type": "string" }` from `ProcessStudyCommandResult.properties`.
   - Line 391: delete `"dicomPath"` from the `required` array.
   - Scan the rest of the schema for any other DICOM-named field, transfer-syntax enum, secondary-capture option, etc. (grep `dicom|tiff|secondary` over the file).
2. **Regenerate TS bindings** — `npm run contracts:generate`. This writes `frontend/src/lib/generated/contracts.ts`; do not hand-edit. Confirm line 5 of the output now reads `export const BACKEND_CONTRACT_VERSION = 2 as const;`.
3. **Drift check** — `npm run contracts:check` should now pass against the new schema.
4. **Rust contracts** — `backend-rs/src/contracts.rs`
   - Line 4: bump `pub const BACKEND_CONTRACT_VERSION: u32 = 1;` to `= 2;`.
   - Line 309: delete `pub output_path: Option<String>,` from `ProcessStudyCommand`.
   - Line 327: delete `pub dicom_path: String,` from `ProcessStudyCommandResult`.
   - Recompile: `cargo check --manifest-path backend-rs/Cargo.toml`. Expect errors in `app.rs`, `cli.rs`, and `http.rs` that touch the deleted fields — those are fixed in Phase 2. The version bump compiles silently (constant is only printed, never compared).
5. **Green gate:** `cargo check` reports errors *only* in files that use `dicom_path`, `output_path`, or the deleted writer; not in unrelated modules.

### Phase 2 — Rust backend gutting
This is the biggest phase by line count. Do it as **one commit per file** so the diff is reviewable, but in a single push.

#### 2a. `backend-rs/src/dicom.rs` (3,860 lines → ~300–500 lines, then rename per D1)
Delete:
- All DICOM tag constants and transfer syntax UID constants near the top.
- `Metadata`, `SpacingPair`, and any other struct whose fields are DICOM-specific (rows, columns, pixel-spacing, window center/width, photometric interpretation enums beyond what BMP needs).
- The DICOM Part 10 reader: `read()` and every helper that parses file meta, explicit/implicit VR, item delimiters, sequences, encapsulated pixel data, deflated transfer syntax.
- The Secondary Capture writer: `encode_secondary_capture`, `encode_secondary_capture_with_options`, `write_secondary_capture_file*`, `write_secondary_capture_file_meta`, `generate_secondary_capture_uid`, and all DICOM element-writing helpers.
- The TIFF decoder: `decode_tiff` and every helper (`read_tiff_ifd`, `tiff_required_u32`, `tiff_optional_u32`, `tiff_entry_u32_values`, `tiff_entry_value_bytes`, `tiff_header`, `write_tiff_short_entry`, `write_tiff_long_entry`, `write_tiff_offset_entry`).
- Test fixtures: `build_test_dicom`, `build_renderable_test_dicom`, `build_tiff_gray`, `build_tiff_rgb`, and every test that consumes them.

Keep & simplify:
- The BMP decoder (`decode_bmp` and its helpers).
- Format-detection entry points (`read_file`, `render_grayscale_preview_*`) reduced to BMP-only. Remove the "is it a standalone image vs Part 10" branch — only BMP remains.
- `supports_standalone_image_path` collapses to "is the extension `.bmp` (case-insensitive)?" — consider inlining at the single call site and deleting the helper.
- `RenderedPreview` struct (imported by `cache.rs`) stays, unless it has DICOM-only fields, in which case prune those fields.

Rename: per D1, move the file to `backend-rs/src/bmp.rs` and update `backend-rs/src/lib.rs`:
- `mod dicom;` → `mod bmp;`
- Every `use crate::dicom::...` across the crate becomes `use crate::bmp::...`. Affected files (confirmed by grep): `app.rs`, `cache.rs`, `cli.rs`, `http.rs`.

#### 2b. `backend-rs/src/cli.rs` (1,616 lines)
The full subcommand dispatch lives at lines 39–58 (10 arms). Treat each individually:

- **Delete** the dispatch arms for `"inspect-decode"` (line 41, per D5) and `"export-secondary-capture"` (line 46, per D6).
- **Delete** the corresponding functions:
  - `inspect_decode` — starts at line **442** and runs roughly to line **1050**, i.e. ~600 lines. This is the second-largest single delete in the cut (after `dicom.rs`). The encapsulation branch at **line 1021** (`if profile.pixel_data_encoding == dicom::PIXEL_DATA_ENCODING_ENCAPSULATED`) lives inside this function and goes with it.
  - `export_secondary_capture` (line 535)
  - `secondary_capture_options_for_input` (line 752)
- **Migrate** the four kept-but-DICOM-coupled subcommand functions to BMP-only source decode. Each currently calls `dicom::read_file` and/or `dicom::render_grayscale_preview_file*`; those will be renamed during the `dicom.rs` → `bmp.rs` cut (see §2a) and the entry point will accept BMP only:
  - `decode_source` (line 457) — calls `dicom::read_file` at line 462 and `dicom::render_grayscale_preview_file` at line 463.
  - `render_preview` (line 470) — calls `dicom::render_grayscale_preview_file_with_window_mode` at line 472.
  - `process_preview` (line 500) — calls `dicom::render_grayscale_preview_file_with_window_mode` at line 502.
  - `analyze_preview` (line 578) — calls `dicom::render_grayscale_preview_file_for_tooth_analysis` at line 580.
- **Legacy flag path (`execute_legacy`, line 154)** — per D7:
  - Line 162: `dicom::read_file(&input_path)?` → use the BMP entry point.
  - Lines 225, 249: `dicom::render_grayscale_preview_file(input_path)?` → BMP equivalent.
  - **Delete lines 265–271** (the `secondary_capture_options_for_input` + `dicom::write_secondary_capture_file_with_options` block). The legacy path no longer writes DICOM output.
  - **Delete the `--output` flag entirely.** Confirmed by grep that the only consumers were the deleted DICOM writer (line 175), the parser (line 98), and the `is_plain_preview_request` check (line 345). All three sites go. Also drop the `--output` mention from `print_legacy_usage`.
- **Help text** — the `print_usage` block (`print_usage` starts at line **779**, subcommand subsection at line **807**) and `print_legacy_usage` block must drop:
  - References to the two deleted subcommands.
  - The `--output` flag mention (legacy help).
  - The "preview/DICOM output" phrasing at line **803**.
  - `--input <study.dcm>` example text → `--input <image.bmp>` (lines 793/798/803).
- **Imports** — line 13: `use crate::dicom::{self, Metadata, RenderWindowMode, RenderedPreview};` updates to `use crate::bmp::{...}` after the rename. `Metadata` may no longer exist; check before keeping the import.
- **Tests** — explicit deletions (verified line numbers from grep):
  - `1312`: `dicom::tests::build_renderable_test_dicom(Some("0.20\\0.30"))` — delete or rewrite using a BMP fixture.
  - `1352` (`render_preview_full_range_ignores_embedded_window`): keep the test concept if it still applies to BMP windowing; delete if it's DICOM-window-specific.
  - `1360`: `build_windowed_renderable_test_dicom(...)` — delete (no BMP equivalent of a "windowed renderable DICOM").
  - `1408`: `build_renderable_test_dicom(Some("0.20\\0.30"))` — delete.
  - `1429` (`decode_source_reports_source_identity_metadata`): rewrite to assert on BMP source metadata or delete.
  - `1435`: `build_renderable_test_dicom_with_source_metadata(...)` — delete.
  - `legacy_preview_and_process_write_expected_artifacts` (around 1494–1563): remove the DICOM-output assertions and the staging of `.dcm` outputs. If the test only existed to cover DICOM paths, delete it entirely.
  - Any test that asserts on the `version` subcommand's printed output (`cli.rs:50` formats `contract-v{BACKEND_CONTRACT_VERSION}`) — grep for `contract-v1` in the test module and update to `contract-v2`.

#### 2c. `backend-rs/src/app.rs` (2,889 lines, ~20 `dicom_path` + `output_path` sites)
This is the second-biggest gut after `dicom.rs` and `inspect_decode`. The grep-verified sites cluster into four regions: a short synchronous `process_study` path (~620–644), a long background job pipeline (~1100–1270), a cache/cleanup region (1745, 1885), and the cache-key/fingerprint plumbing (713/841/1706). Plus three tests.

- **Imports** — `use crate::dicom::...` → `use crate::bmp::...` (or drop entirely if no BMP symbols are imported here).
- **Short `process_study` path (~lines 620–644):**
  - Line 621: delete the `dicom_path = …` allocation.
  - Lines 626, 633–636, 639: delete the `secondary_capture_options(...)` call, the `dicom::write_secondary_capture_file_with_options` write, and the `track_artifact_bytes(&dicom_path)` call.
  - Line 644: remove `dicom_path: dicom_path.display().to_string(),` from the result construction.
- **`secondary_capture_options` method (~line 796):** delete the entire method (it only feeds the writer).
- **Background job pipeline (~lines 1100–1270):**
  - Lines 1103–1105: the tuple returned by the prepare step drops its third element (`dicom_path`).
  - Line 1124: destructuring changes from `(resolved, preview_path, dicom_path)` to `(resolved, preview_path)`.
  - Line 1207: `cleanup_files(&[preview_path.as_path(), dicom_path.as_path()])` → drops the `dicom_path` slot.
  - Lines 1228–1244: delete the entire "Writing processed DICOM" stage (the `track_progress` block, the `secondary_capture_options` call, the `dicom::write_secondary_capture_file_with_options` call, the `track_artifact_bytes` call).
  - Line 1251: `cleanup_files(&[preview_path.as_path(), dicom_path.as_path()])` → drops `dicom_path`.
  - Line 1270: remove `dicom_path: dicom_path.display().to_string(),` from the final result.
- **Cache-hit and cleanup region:**
  - Line 1745: `artifact_exists(&payload.preview_path) && artifact_exists(&payload.dicom_path)` → drop the `dicom_path` conjunct. (After the contract change, `payload` won't have a `dicom_path` field anyway, so the compiler enforces this.)
  - Line 1885: `cleanup_path(&payload.dicom_path)` → delete the call.
- **Cache-key (`process_fingerprint`) construction — concrete sites:** grep verified that `output_path` participates in the fingerprint hash.
  - Line 841: `"outputPath": fingerprint_output_path(command.output_path.as_deref()),` inside `process_fingerprint` (line 831) — delete this entry from the JSON the fingerprint hashes.
  - Line 1706: `fn fingerprint_output_path(output_path: Option<&str>) -> Option<String>` helper — delete the function.
  - Line 713: `fn resolve_process_output_path(...)` — delete the function (only consumed by the now-deleted DICOM-write paths at 622 and 1104).
  - Lines 622 and 1104: call sites for `resolve_process_output_path(command.output_path.as_deref(), …)` — go away with the surrounding DICOM-write blocks (already enumerated above).
  - Test fixtures at lines 2530, 2592, 2635: `output_path: None,` lines disappear automatically when the struct field is removed; no manual edit needed.
- **Tests (~line 2556–2622):**
  - Line 2556: `assert!(fs::metadata(&payload.dicom_path).unwrap().is_file());` — delete.
  - Line 2557: `let output_metadata = dicom::read_file(&payload.dicom_path).unwrap();` and any following assertions on `output_metadata` — delete (the test was validating Secondary Capture round-trip).
  - Line 2622: `assert_eq!(second_payload.dicom_path, first_payload.dicom_path);` — delete (cache-hit identity test, redo against `preview_path` instead).

#### 2d. `backend-rs/src/cache.rs` (23 KB)
- Update the `use crate::dicom::RenderedPreview;` import to `use crate::bmp::RenderedPreview;` (assuming `RenderedPreview` survives the gut).
- Verify no cache-artifact path templates name `.dcm` or `.tif`. Grep for those substrings in this file.

#### 2e. `backend-rs/src/http.rs` (52 KB)
- This file is mostly transport; the only expected breakage is in test modules that construct DICOM fixtures via `build_renderable_test_dicom`. Replace those with BMP fixtures or delete tests that only validated DICOM round-trips.
- Three sites print `BACKEND_CONTRACT_VERSION` (lines 170, 356, 798). No code change needed — they automatically pick up `2`. Sanity-check the strings in any test that compares `contract-v1` against the printed output and update them to `contract-v2`.
- Grep for `dicom_path` in this file; any HTTP response that surfaced the field as part of a job-status payload must drop it (the contracts.rs change forces this).
- Grep for `dicom::` in the test module; replace each fixture-builder call with a BMP equivalent, or delete the surrounding test if it was format-specific.

#### 2f. `backend-rs/src/persistence.rs`
- Lines 258, 261, 266–267, 322–323, 336–337, 355, 360, 377, 380, 383, 388–389, 401, 409, 412, 428: rename all `.dcm` test-fixture strings to `.bmp`. These are opaque strings (functionally inert), but they're caught by the DoD grep gate (§9), so renaming keeps the gate strict. ~10 minutes of mechanical find/replace.

#### 2g. `backend-rs/Cargo.toml`
- Current state (line 9): `image = { version = "0.25", default-features = false, features = ["jpeg", "png"] }`. No `tiff` feature — good.
- **Drop the `jpeg` feature.** Verified by grep that the only consumer of JPEG decode/encode in the crate is `dicom.rs` (likely an encapsulated-pixel-data path). After the cut, no caller remains. New line should read `features = ["png"]`.
- Keep `png` — used by the preview output pipeline.
- Re-confirm after the cut by running `cargo tree -p xrayview-backend-rs --depth 2` and visually diffing to baseline. The expected dependency-graph delta is the loss of `jpeg-decoder` / `zune-jpeg` (or whichever JPEG backend `image 0.25` pulls in).

**Green gate for Phase 2:** `npm run backend:test` passes; `cargo clippy --manifest-path backend-rs/Cargo.toml --all-targets -- -D warnings` reports no dead-code or unused-import warnings.

### Phase 3 — Desktop (Tauri) shell
Grep already confirms `desktop-tauri/` has **zero** direct DICOM/TIFF references. The shell delegates to backend commands by name; once the contract field is gone and the backend rebuilds, the shell rebuilds without changes.

Sanity steps:
1. `cargo check --manifest-path desktop-tauri/Cargo.toml` — must succeed.
2. Re-read `desktop-tauri/src/commands.rs` and confirm no `#[tauri::command]` wrapper is named after a deleted backend method. If any wrapper exists for `export_secondary_capture`, delete it and its registration in `desktop-tauri/src/main.rs` / `setup`.
3. `desktop-tauri/capabilities/default.json` and `desktop-tauri/tauri.conf.json` — no DICOM/TIFF-specific permissions. No edits.

**Green gate:** `npm run tauri:build -- --no-bundle` succeeds.

### Phase 4 — Frontend
The ~12 frontend files identified by grep (`src/**` plus `scripts/`) fall into three buckets: type-shape changes (driven by regenerated contracts), file-dialog filters, and UI strings.

#### 4a. Type-shape ripples (from Phase 1)
- `frontend/src/lib/generated/contracts.ts` — already regenerated; do not hand-edit. `BACKEND_CONTRACT_VERSION` flips to `2` automatically.
- `frontend/src/lib/types.ts` (lines 42–45): remove `dicomPath: string` from `ProcessResult`. Also remove any `outputPath?: string` field on the `ProcessCommand`-shaped type that mirrors the schema's `ProcessStudyCommand`.
- `frontend/src/lib/runtime.ts` (~line 74, `asProcessResult`): remove `dicomPath: payload.dicomPath`.
- `frontend/src/app/store/applyJob.ts`: remove the line that reads `job.result.payload.dicomPath`.
- Grep the frontend for any caller that **passes** `outputPath` into the process-study command (likely in the store action that dispatches the job). Remove the field from the payload it constructs — the schema no longer accepts it (`additionalProperties: false`).
- Grep for any TS code that compares `BACKEND_CONTRACT_VERSION === 1`. None expected (the constant is informational), but verify.

#### 4b. File-dialog filters
- `frontend/src/lib/tauri.ts`
  - Lines 4–13: rewrite `DICOM_FILTERS` to a single BMP filter:
    ```ts
    const BMP_FILTER = [{ name: "Bitewing X-ray (BMP)", extensions: ["bmp"] }];
    ```
  - Lines 15–20: **delete** `DICOM_SAVE_FILTERS` entirely (per D2, no save dialog).
  - Line 32: dialog title `"Open Study or BMP/TIFF"` → `"Open Bitewing X-ray"`.
  - Line 51 (`Save Processed DICOM`): delete the save function it belongs to.
  - Rename the file's exported helpers from `pickDicomFile`/`pickSaveDicomPath` to `pickBmpFile` (drop the save path entirely).
- `frontend/src/lib/shell.ts`: rename `pickDicomFile` → `pickBmpFile`; delete `pickSaveDicomPath`. Update all call sites.
- `frontend/src/lib/backendUtils.ts`: delete `ensureDicomExtension`, `buildOutputName`, and the `.dcm`/`.dicom` regex around lines 6–12. These existed only to name DICOM output files.

#### 4c. UI strings
- `frontend/src/app/htmxView.ts` lines 170, 192, 437, 608: replace every occurrence of `"DICOM study or BMP/TIFF image"` (and variants) with `"bitewing X-ray (BMP)"`. Same for "Open"/"Load" prompt copy.
- `frontend/src/app/store/workbenchStore.ts`: same string replacement on the description constant.
- `frontend/src/lib/mockBackend.ts`: remove `MOCK_PROCESSED_DICOM_PATH` and any mock paths ending in `.dcm`. Replace with a BMP-style mock path if a mock output is still needed (per D2, processing produces only a preview PNG, so even the mock output path can go).
- `frontend/src/lib/mockRuntime.ts`: scan for any DICOM/TIFF strings and prune.

#### 4d. Validation scripts
- `frontend/scripts/validate-*.mjs`: open each, grep for "dicom" or "tiff" or "tif" — if any script asserts on those formats, update or delete.

**Green gate for Phase 4:**
- `npm --prefix frontend run build` succeeds (includes `tsc --noEmit`).
- `node frontend/scripts/<script>.mjs` runs cleanly for each validation script.
- `npm run dev` boots, the open-file dialog shows only the BMP filter, opening a sample BMP from `images/BMP/` works end-to-end.
- `npm run tauri:dev` exercises the same flow against the in-process backend.

### Phase 5 — Documentation and sample assets
1. `README.md`
   - Line 17: rewrite the feature bullet to mention only BMP bitewing input.
   - Line 20: delete the "Export processed results as DICOM Secondary Capture" bullet.
   - Line 82 onward (Artifact table): drop the row for the DICOM output if listed.
   - Line 135: change the `open_study` description to "Open a BMP bitewing X-ray".
   - Line 172: delete the `export-secondary-capture` CLI example block.
   - Search for `sample-dental-radiograph.dcm` and remove (CLAUDE.md and AGENTS.md already flag it as stale).
2. `CLAUDE.md` (use grep to find current line numbers — they drift; the audit values below are best-effort)
   - "CLI has two surfaces" paragraph (around line 53): remove `export-secondary-capture` and `inspect-decode` from the list of utility subcommands. Confirm the remaining list matches the post-cut `cli.rs` dispatch.
   - "Deflated transfer syntax and encapsulated multi-frame DICOM source decode are not supported" bullet (in Runtime & Build Gotchas section): delete — the whole DICOM caveat is moot.
   - "Sample assets" bullet (around line 63): update to "Current `images/` contents are BMP samples".
   - Any architecture diagram or runtime-mode description that mentions DICOM input: revise to BMP-only.
3. `AGENTS.md` line 29: update the stale-asset note to reflect BMP-only.
4. `images/README.md` line 12 and around: drop the "Bundled sample DICOM artifacts have been removed" line (it's about an even older removal) only if it now reads confusingly. Otherwise leave as historical.
5. `backend-rs/README.md`: update the capabilities section to BMP-only.
6. `CHANGELOG.md`: append a new entry under the current section noting the removal of DICOM and TIFF support, with a one-line rationale and a pointer to this plan. Do **not** rewrite historical entries.
7. Sample assets:
   - `images/TIF/` — already gone per `ls`; nothing to delete.
   - `images/BMP/` — keep.
   - `images/PNG/` — keep (used by tests and previews).
   - Search the tree for any committed `.dcm` or `.tif`/`.tiff` files: `find . -type f \( -iname '*.dcm' -o -iname '*.dicom' -o -iname '*.tif' -o -iname '*.tiff' \) -not -path './node_modules/*' -not -path './.git/*' -not -path './*/target/*'`. Delete every hit.

**Green gate:** `npm run release:smoke` passes on the cleaned-up branch.

### Phase 6 — Final verification and cleanup
1. `cargo clippy --manifest-path backend-rs/Cargo.toml --all-targets -- -D warnings` — no dead-code, no unused-imports.
2. `cargo clippy --manifest-path desktop-tauri/Cargo.toml --all-targets -- -D warnings`.
3. `npm --prefix frontend run build` — clean.
4. `npm run backend:test` — clean.
5. `npm run playwright:install && npm run test:e2e` — Playwright suite passes; update any spec that asserts the file-picker shows `.dcm` or `.tif`.
6. `npm run tauri:build -- --no-bundle` — release build succeeds. Measure binary size delta vs Phase 0 baseline.
7. Manual smoke (Tauri shell):
   - Launch with `npm run tauri:dev`.
   - Open a BMP from `images/BMP/`; preview renders.
   - Annotate, process, observe the preview update.
   - Confirm no "Save Processed DICOM" affordance is reachable in the UI.
8. Confirm the final repo has zero hits for `dicom`/`tiff` outside `CHANGELOG.md`:
   ```bash
   git grep -niE 'dicom|tiff|\.dcm\b|\.tif\b' -- ':!CHANGELOG.md' ':!REMOVAL_PLAN.md'
   ```
   Expect empty output. If anything remains, it's either a missed reference or a deliberate one — review case-by-case.

---

## 5. File-by-file change matrix

Cross-reference for the phases above. Lines are best-effort from the audit; treat as starting points, not exact targets.

| File | Action | Lines / symbols | Phase |
|---|---|---|---|
| `contracts/backend-contract-v1.schema.json` | Edit (no rename) | 6 (`x-contract-version`), 349–351 (`outputPath`), 377 (`dicomPath` prop), 391 (`dicomPath` required) | 1 |
| `frontend/src/lib/generated/contracts.ts` | Regenerate | — | 1 |
| `backend-rs/src/contracts.rs` | Edit | 4 (`BACKEND_CONTRACT_VERSION`), 309 (`output_path`), 327 (`dicom_path`) | 1 |
| `backend-rs/src/dicom.rs` | Gut + rename to `bmp.rs` | most of the file | 2a |
| `backend-rs/src/cli.rs` | Edit | 13 import; 39–58 dispatch (drop 41 + 46); 154 `execute_legacy` (drop SC write 265–271, drop `--output` parsing at 98/175/345); **442–~1050** `inspect_decode` delete (~600 lines, includes encapsulation branch at 1021); 457/470/500/578 keep+migrate; 535 `export_secondary_capture` delete; 752 `secondary_capture_options_for_input` delete; help text 779/803/807 + legacy help; tests at 1312, 1352, 1360, 1408, 1429, 1435, 1494–1563, plus any `contract-v1` string | 2b |
| `backend-rs/src/app.rs` | Edit | imports; 621/626/633–639/644 (short path); 713 `resolve_process_output_path` delete; 796 `secondary_capture_options` delete; 841 `outputPath` in fingerprint; 1103–1270 (job pipeline incl. cache-key call at 1104); 1706 `fingerprint_output_path` delete; 1745 (cache-hit); 1885 (cleanup); 2556/2557/2622 (tests) — ~20 sites | 2c |
| `backend-rs/src/cache.rs` | Edit | 9 import path | 2d |
| `backend-rs/src/http.rs` | Edit | 170/356/798 print-version sanity-check; test module fixtures; any `dicom_path` surfaced in payloads | 2e |
| `backend-rs/src/persistence.rs` | Edit (cosmetic) | 258–428: rename `.dcm` test strings to `.bmp` | 2f |
| `backend-rs/src/lib.rs` | Edit | `mod dicom;` → `mod bmp;` | 2a |
| `backend-rs/Cargo.toml` | Edit | line 9: drop `jpeg` feature from `image` (sole consumer was `dicom.rs`) | 2g |
| `desktop-tauri/src/*` | Audit only | grep `dicom_path`/`export_secondary` for any `#[tauri::command]` wrapper | 3 |
| `frontend/src/lib/tauri.ts` | Edit | 4–13 (filters), 15–20 (delete save filters), 32 / 51 (titles) | 4b |
| `frontend/src/lib/shell.ts` | Rename helpers | `pickDicomFile` → `pickBmpFile`; delete `pickSaveDicomPath` | 4b |
| `frontend/src/lib/backendUtils.ts` | Delete functions | 6–12 (`ensureDicomExtension`, `buildOutputName`, `.dcm`/`.dicom` regex) | 4b |
| `frontend/src/lib/types.ts` | Edit | remove `dicomPath` from `ProcessResult`; remove `outputPath` from `ProcessCommand` if present | 4a |
| `frontend/src/lib/runtime.ts` | Edit | `asProcessResult` drops `dicomPath` | 4a |
| `frontend/src/lib/mockBackend.ts` | Edit | `MOCK_PROCESSED_DICOM_PATH` removed; mock no longer returns `dicomPath` | 4c |
| `frontend/src/lib/mockRuntime.ts` | Edit | grep for refs | 4c |
| `frontend/src/app/htmxView.ts` | Edit | 170, 192, 437, 608 (UI strings) | 4c |
| `frontend/src/app/store/workbenchStore.ts` | Edit | description string; any `outputPath` payload field removed | 4c |
| `frontend/src/app/store/applyJob.ts` | Edit | reads `dicomPath` | 4a |
| `frontend/scripts/validate-*.mjs` | Audit / edit | grep individually | 4d |
| `README.md` | Edit | 17, 20, 82, 135, 172 | 5 |
| `CLAUDE.md` | Edit | grep for current line numbers; ~53 (CLI surfaces), gotchas (deflated TS), ~63 (sample assets) | 5 |
| `AGENTS.md` | Edit | stale-asset note | 5 |
| `images/README.md` | Edit | TIF section | 5 |
| `backend-rs/README.md` | Edit | capabilities section | 5 |
| `CHANGELOG.md` | Append entry | top of unreleased section | 5 |
| `images/TIF/` | Already absent | — | 5 |
| Any `.dcm`/`.tif`/`.tiff` files | Delete | discovered via `find` | 5 |

---

## 6. Module rename details (D1)

`backend-rs/src/dicom.rs` → `backend-rs/src/bmp.rs`:

1. `git mv backend-rs/src/dicom.rs backend-rs/src/bmp.rs` (preserves history).
2. In `backend-rs/src/lib.rs`, change `pub mod dicom;` to `pub mod bmp;`.
3. Across the crate, replace `use crate::dicom::` with `use crate::bmp::` (confirmed call sites: `app.rs`, `cache.rs`, `cli.rs`, `http.rs`). One pass with `sed` or `rg --files-with-matches | xargs sed` is sufficient; review the diff before committing.
4. Inside `bmp.rs`, rename any DICOM-flavored public items that survive the gut. Examples to watch for:
   - `read_file` → `decode_bmp_file` (or keep `read_file` if it's clearly the module's entry point).
   - `render_grayscale_preview_file` → keep, but ensure docstring no longer mentions DICOM.
   - `RenderedPreview` — keep, ensure fields are format-agnostic.
5. Search for stale doc-comments inside the renamed file (`//`, `///`, `//!`) that still talk about DICOM Part 10 or transfer syntax — delete them.

---

## 7. Risks and mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| Hidden caller of a deleted public function (e.g., a `#[tauri::command]` wrapper) breaks the desktop build silently. | Low | Build fails late. | Phase 3's `cargo check` on `desktop-tauri` runs immediately after Phase 2. |
| A Playwright spec asserts on the old file-picker filter and fails post-change. | Medium | Test red. | Phase 6 explicitly updates specs; run the suite before opening a PR. |
| Cache artifacts on dev machines (and any serialized job payloads on disk) reference `dicom_path` / `output_path` fields no longer in the contract. | Medium-High | Stale-cache deserialize errors on first run. | **Mandatory** cache-wipe step in the PR description: `rm -rf` the cache directory reported by `xrayview-backend-rs print-config` before the first launch of a post-cut build. Not optional. |
| Contract version bump (D4) breaks any external tooling pinned to v1. | Low (no known external consumers) | External breakage. | Document the bump in the CHANGELOG entry; if external consumers turn up, hold the bump and keep version `1`. |
| **Legacy CLI scripts** that rely on `xrayview-backend-rs --input X --output Y --preset Z` will no longer produce a `.dcm` (D7 deletes the writer). | Low (single-user app, no known automation) | External breakage of user shell scripts. | Call this out explicitly in the CHANGELOG and the PR description. If anyone has such a script, they need to drop `--output` and stop expecting a DICOM file. |
| Renaming `dicom.rs` makes blame/history harder to follow. | Low | Slight DX hit. | Use `git mv` (not delete-and-recreate) so `git log --follow` still works. |
| `dicom_path` / `output_path` field removal cascades into a frontend feature the audit missed. | Low | TS compile error. | The TS compiler is the safety net; Phase 4a chases every break. |
| Sample asset deletion removes a file referenced by a future test someone is writing on another branch. | Low | Merge conflict, easily fixed. | The `find` query in Phase 5 reports what's being deleted; review before `rm`. |

### Rollback
The plan is staged into atomic-ish phases with green gates. If a phase fails review, `git revert` the corresponding commit(s); the schema/contract phase is the only one where revert order matters (revert Phase 1 last, since downstream phases depend on it).

---

## 8. Effort estimate

Wall-clock, assuming one focused engineer familiar with the codebase. Estimates revised upward after the v2 audit revealed deeper `app.rs` entanglement (~17 `dicom_path` sites, not ~6) and the additional scope from D6 (four CLI subcommands to migrate, not just delete) and D7 (legacy flag path's DICOM-write branch).

| Phase | Estimate |
|---|---|
| 0 — Branch + baseline | 15 min |
| 1 — Contracts | 45 min (includes `outputPath` removal) |
| 2 — Rust backend gut + rename + subcommand migration | **4–7 hours** (the bulk; `app.rs` job pipeline is the densest part) |
| 3 — Desktop shell | 15 min |
| 4 — Frontend | 1.5–2.5 hours |
| 5 — Docs + sample assets | 30–45 min |
| 6 — Verification | 1–1.5 hours |
| **Total** | **~8–14 hours** of focused work |

Most of the variance is in Phase 2 — the DICOM/TIFF code is dense, and the cost of accidentally deleting a BMP-relevant helper is a debugging session. Budget conservatively. Treat the upper bound as the realistic estimate; the lower bound assumes no surprises in the `app.rs` cache-key region or the CLI subcommand migration.

---

## 9. Definition of done

- [ ] **Strict grep gate:** `git grep -niE 'dicom|tiff|\.dcm\b|\.tif\b' -- ':!CHANGELOG.md' ':!REMOVAL_PLAN.md'` returns **nothing**. The `persistence.rs` test-fixture rename (§2f) is what allows this gate to be strict; if you skipped that step, this will not pass.
- [ ] `npm run release:smoke` is green.
- [ ] Playwright suite is green; no spec asserts on `.dcm`/`.tif` file-picker filters.
- [ ] Manual smoke in Tauri shell opens a BMP, processes it, shows the preview, with no DICOM/TIFF UI element reachable. The legacy `--input` CLI path runs end-to-end on a BMP without writing a `.dcm`.
- [ ] Release binary size for `desktop-tauri/target/release/xrayview` is reported (delta vs Phase 0 baseline) in the PR description. Not a strict pass/fail gate, but expected to be smaller.
- [ ] Post-cut line-count delta of `dicom.rs` → `bmp.rs` reported in the PR description.
- [ ] `BACKEND_CONTRACT_VERSION` prints `2` from `cli.rs version` and from any `http.rs` status endpoint.
- [ ] CHANGELOG entry merged.
- [ ] `dicom.rs` no longer exists; `bmp.rs` is its replacement.

---

## 10. Open follow-ups (not part of this PR)

- Tighten Tauri asset-protocol scope from `["**"]` to the cache directory only (CLAUDE.md flag).
- Rename `contracts/backend-contract-v1.schema.json` → `…-v2…` and update `contracts/scripts/schema-tools.mjs:10` (hard-coded path) + the schema's `$id` URL to match. Deferred from D4 to keep the removal PR focused.
- Consider deleting the legacy compatibility flag-based CLI path entirely (`--input`, `--preset`, …) once the test suite no longer depends on it. After D7 the path no longer writes DICOM, so it's a thin wrapper around the subcommands and likely vestigial.
- Investigate whether the `image` crate dependency is still needed at all after the cut — if only BMP remains and the BMP decoder is custom, the crate may be dropped entirely (binary-size win).
- Rename `xrayview` → something bitewing-specific if the scope contraction is permanent.
