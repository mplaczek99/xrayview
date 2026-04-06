# Phase 0 Repository Behavior Inventory

This document completes phase 0 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). It records what the repository does today based on the current code, not on intended future architecture.

Primary code references:

- [backend/src/app/mod.rs](backend/src/app/mod.rs)
- [backend/src/app/state.rs](backend/src/app/state.rs)
- [backend/src/study/source_image.rs](backend/src/study/source_image.rs)
- [backend/src/save.rs](backend/src/save.rs)
- [backend/src/api/contracts.rs](backend/src/api/contracts.rs)
- [backend/src/bin/xrayview-backend.rs](backend/src/bin/xrayview-backend.rs)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs)

## 1. Current Runtime Shape

The desktop app is still a single-process Tauri application with the Rust backend linked directly into the shell:

- `frontend/src/` renders the React UI and talks to `frontend/src/lib/backend.ts`.
- `frontend/src/lib/backend.ts` decides between `tauri` and `mock` runtime modes.
- In desktop mode it calls Tauri commands with `invoke(...)`.
- `frontend/src-tauri/src/main.rs` exposes custom Tauri commands and owns a process-global `BackendAppState`.
- `BackendAppState` lives in [backend/src/app/state.rs](backend/src/app/state.rs) and owns the study registry, job registry, memory cache, disk cache, and recent-study catalog store.
- The backend crate is still the contract authority. TypeScript contracts are generated from [backend/src/api/contracts.rs](backend/src/api/contracts.rs) by [backend/src/bin/generate-contracts.rs](backend/src/bin/generate-contracts.rs), triggered from [frontend/scripts/generate-contracts.mjs](frontend/scripts/generate-contracts.mjs) during frontend `dev` and `build`.

There is no Go process yet. There is also no IPC or HTTP boundary between frontend and backend yet.

## 2. Backend Capability List

### 2.1 Exposed product capabilities

The Rust backend currently owns these capabilities:

- DICOM study open/registration by path, returning a generated `studyId` plus input metadata via [backend/src/app/state.rs](backend/src/app/state.rs).
- Measurement-scale extraction from DICOM metadata via [backend/src/app/mod.rs](backend/src/app/mod.rs) and [backend/src/study/source_image.rs](backend/src/study/source_image.rs).
- Preview rendering to PNG via [backend/src/app/mod.rs](backend/src/app/mod.rs) and [backend/src/render/render_plan.rs](backend/src/render/render_plan.rs).
- Processing pipeline execution via [backend/src/processing.rs](backend/src/processing.rs) and [backend/src/processing/pipeline.rs](backend/src/processing/pipeline.rs):
  - invert
  - brightness
  - contrast
  - histogram equalization
  - pseudocolor palettes (`none`, `hot`, `bone`)
  - side-by-side compare output
- Derived DICOM export as Secondary Capture via [backend/src/export/secondary_capture.rs](backend/src/export/secondary_capture.rs) and [backend/src/save.rs](backend/src/save.rs).
- Automatic tooth analysis and suggested annotation generation via [backend/src/tooth_measurement.rs](backend/src/tooth_measurement.rs) and [backend/src/analysis/auto_tooth.rs](backend/src/analysis/auto_tooth.rs).
- Manual line measurement enrichment via [backend/src/analysis/measurement_service.rs](backend/src/analysis/measurement_service.rs).
- Background jobs with dedupe, cancellation, polling snapshots, and in-memory result caching via [backend/src/jobs/registry.rs](backend/src/jobs/registry.rs) and [backend/src/app/state.rs](backend/src/app/state.rs).
- Recent-study catalog writes via [backend/src/persistence/catalog.rs](backend/src/persistence/catalog.rs).
- TypeScript contract generation via [backend/src/bin/generate-contracts.rs](backend/src/bin/generate-contracts.rs).

### 2.2 Decode and export support envelope

Current DICOM decode/export behavior is narrower than the product summary alone implies:

- `describe_study(...)` reads only up to `PIXEL_DATA`; it is a metadata pass, not a full image decode.
- Full source decode supports:
  - native 8-bit monochrome
  - native 16-bit monochrome
  - native 32-bit monochrome
  - native 8-bit RGB converted to grayscale
  - encapsulated pixel sequences through `dicom_pixeldata`
- Explicitly unsupported today:
  - 16-bit color source decode
  - 32-bit color source decode
  - non-`RGB` color photometric interpretations in the native 8-bit color path
- Default windowing is taken from `WindowCenter` and `WindowWidth` when present.
- `MONOCHROME1` is rendered inverted.
- Rescale slope/intercept are applied during source decode.
- Measurement scale lookup order is:
  - `PixelSpacing`
  - `ImagerPixelSpacing`
  - `NominalScannedPixelSpacing`
- Export writes a Secondary Capture dataset with new SOP/series UIDs but preserves a selected set of source tags, including patient/study identity fields and spacing tags.

### 2.3 Capabilities present but not actually surfaced to the desktop UI

These behaviors exist in Rust but are not currently exposed as normal desktop UX features:

- The recent-study catalog is written on study open, but there is no Tauri command or frontend UI to read it back.
- Tauri emits job events, but the frontend does not subscribe to them; the UI reconciles via polling.
- The CLI exposes metadata and plain-preview entrypoints that do not exist as first-class desktop shell commands.

## 3. Entrypoint Inventory

### 3.1 Tauri command entrypoints

The desktop shell exposes these commands in [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs):

| Tauri command | Current role | Backend target |
| --- | --- | --- |
| `pick_dicom_file` | Shell-only native open dialog | none |
| `pick_save_dicom_path` | Shell-only native save dialog | none |
| `get_processing_manifest` | Returns preset manifest | `processing_manifest()` |
| `open_study` | Validates and registers a study | `BackendAppState::open_study_command(...)` |
| `start_render_job` | Starts preview render job | `BackendAppState::start_render_job(...)` |
| `start_process_job` | Starts processing/export job | `BackendAppState::start_process_job(...)` |
| `start_analyze_job` | Starts analysis job | `BackendAppState::start_analyze_job(...)` |
| `get_job` | Returns current job snapshot | `BackendAppState::get_job(...)` |
| `cancel_job` | Requests cancellation and returns snapshot | `BackendAppState::cancel_job(...)` |
| `measure_line_annotation` | Recomputes line measurement | `BackendAppState::measure_line_annotation(...)` |

Notes:

- `open_study` is the only command wrapped in `spawn_blocking`; the start-job commands return immediately after scheduling a worker thread.
- The shell is thin. All product logic still lives in the Rust backend crate.

### 3.2 CLI entrypoints

[backend/src/bin/xrayview-backend.rs](backend/src/bin/xrayview-backend.rs) exposes these CLI modes:

- `--describe-presets`
  - prints JSON processing manifest
- `--describe-study --input <path>`
  - prints JSON `StudyDescription`
- `--analyze-tooth --input <path> [--preview-output <png>]`
  - runs tooth analysis and prints JSON analysis
- plain preview mode:
  - selected when `--preview-output` is set
  - `--output` is not set
  - preset is still `default`
  - no processing flags are set
- processing mode:
  - every remaining `--input` flow that is not one of the metadata/analysis modes

Processing mode uses [backend/src/app/mod.rs](backend/src/app/mod.rs):

- if both `output_path` and `preview_output` are absent, it synthesizes `input_processed.dcm`
- if only `preview_output` is set, it writes only preview PNG
- if only `output_path` is set, it writes only DICOM
- if both are set, it writes both

### 3.3 Contract generation entrypoint

Rust remains the source of truth for generated frontend types:

- [frontend/scripts/generate-contracts.mjs](frontend/scripts/generate-contracts.mjs) runs `cargo run --manifest-path backend/Cargo.toml --bin generate-contracts -- <output-path>`.
- [backend/src/bin/generate-contracts.rs](backend/src/bin/generate-contracts.rs) writes the string emitted by [backend/src/api/contracts.rs](backend/src/api/contracts.rs).

This is a hard current-state coupling between Rust and frontend builds.

## 4. Frontend -> Backend Call Path Inventory

This section lists the actual frontend/backend call paths used by the desktop app today.

| User/system trigger | Frontend code path | Adapter call(s) | Tauri/backend path | Notes |
| --- | --- | --- | --- | --- |
| App boot | [frontend/src/app/App.tsx](frontend/src/app/App.tsx) -> `workbenchActions.ensureManifest()` | `loadProcessingManifest()` | `get_processing_manifest` -> `processing_manifest()` | Runs once on mount. |
| Open DICOM button | [frontend/src/components/viewer/ViewTab.tsx](frontend/src/components/viewer/ViewTab.tsx) -> `workbenchActions.openStudy()` | `pickDicomFile()` then `openStudy()` then `startRenderStudyJob()` | `pick_dicom_file`, `open_study`, `start_render_job` | Opening a study always queues a render job after registration. |
| Pending job reconciliation loop | [frontend/src/features/jobs/useJobs.ts](frontend/src/features/jobs/useJobs.ts) | `getJob(jobId)` every 1500 ms while pending | `get_job` | The frontend does not listen for emitted Tauri job events. |
| Measure teeth button | [frontend/src/components/viewer/ViewTab.tsx](frontend/src/components/viewer/ViewTab.tsx) -> `workbenchActions.measureActiveStudy()` | `startAnalyzeStudyJob()` then `getJob(...)` polling | `start_analyze_job`, `get_job` | Analysis always operates on an already-opened `studyId`. |
| Draw/update manual line | [frontend/src/features/viewer/ViewerCanvas.tsx](frontend/src/features/viewer/ViewerCanvas.tsx) -> store action | `measureLineAnnotation(studyId, annotation)` | `measure_line_annotation` | Stateless backend enrichment; annotation persistence stays in frontend store. |
| Choose save destination | [frontend/src/components/processing/ProcessingTab.tsx](frontend/src/components/processing/ProcessingTab.tsx) -> `workbenchActions.pickProcessingOutputPath()` | `pickSaveDicomPath(defaultName)` | `pick_save_dicom_path` | Frontend appends `.dcm` if needed. |
| Run processing | [frontend/src/components/processing/ProcessingTab.tsx](frontend/src/components/processing/ProcessingTab.tsx) -> `workbenchActions.runActiveStudyProcessing(...)` | `startProcessStudyJob(...)` then `getJob(...)` polling | `start_process_job`, `get_job` | The backend adapter sends only explicit overrides beyond the chosen preset baseline. |
| Cancel from processing UI or Job Center | [frontend/src/components/processing/ProcessingTab.tsx](frontend/src/components/processing/ProcessingTab.tsx), [frontend/src/features/jobs/JobCenter.tsx](frontend/src/features/jobs/JobCenter.tsx) | `cancelJob(jobId)` | `cancel_job` | Pending jobs keep getting polled until terminal. |

Important current-state note:

- [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs) emits `job:progress`, `job:completed`, `job:failed`, and `job:cancelled`.
- No frontend code currently calls Tauri `listen(...)`.
- The real UI contract for job updates is polling through `get_job`, not event delivery.

## 5. Job Lifecycle Inventory

### 5.1 Job kinds and states

Current job kinds from [backend/src/api/contracts.rs](backend/src/api/contracts.rs):

- `renderStudy`
- `processStudy`
- `analyzeStudy`

Current job states:

- `queued`
- `running`
- `cancelling`
- `completed`
- `failed`
- `cancelled`

### 5.2 Start semantics

Job creation is handled in [backend/src/jobs/registry.rs](backend/src/jobs/registry.rs):

- Each job kind uses a fingerprint.
- If another active job with the same fingerprint already exists, `start_job(...)` returns the existing snapshot instead of creating a second worker.
- If an in-memory cached `JobResult` already exists and all referenced artifact files still exist on disk, the backend creates an immediate completed snapshot with `from_cache = true`.
- Otherwise it creates a queued job and spawns a worker thread.

Fingerprint inputs today:

- render:
  - namespace `render-study-v1`
  - payload only includes `inputPath`
- analyze:
  - namespace `analyze-study-v1`
  - payload only includes `inputPath`
- process:
  - namespace `process-study-v2`
  - payload includes `inputPath`, `outputPath`, preset id, explicit overrides, compare flag, and palette

### 5.3 Progress stages

Progress staging is hard-coded in [backend/src/app/state.rs](backend/src/app/state.rs).

Render job stages:

- `validating` at 10%
- `loadingStudy` at 35%
- `renderingPreview` at 75%
- `writingPreview` at 90%
- terminal `completed` at 100%

Process job stages:

- `validating` at 10%
- `loadingStudy` at 30%
- `processingPixels` at 65%
- `writingPreview` at 84%
- `writingDicom` at 95%
- terminal `completed` at 100%

Analyze job stages:

- `validating` at 10%
- `loadingStudy` at 35%
- `renderingPreview` at 65%
- `measuringTooth` at 88%
- terminal `completed` at 100%

Cached jobs skip the normal stages and return:

- `state = completed`
- `progress.stage = cacheHit`
- `progress.message = Loaded from cache`

### 5.4 Cancellation behavior

Current cancellation behavior from [backend/src/jobs/registry.rs](backend/src/jobs/registry.rs) and [backend/src/app/state.rs](backend/src/app/state.rs):

- cancelling a queued job marks it `cancelled` immediately with `Cancelled before start`
- cancelling a running job marks it `cancelling` with `Cancellation requested`
- worker threads only honor cancellation at explicit checkpoints between major stages
- cancellation is cooperative; there is no forced interruption in the middle of a decode/process/write step
- when a worker notices cancellation, it publishes a terminal `cancelled` snapshot with `Cancelled by user`

### 5.5 Failure behavior

- Terminal failures produce `state = failed` and attach a serialized `BackendError`.
- `BackendErrorCode` values are:
  - `invalidInput`
  - `notFound`
  - `cancelled`
  - `conflict`
  - `cacheCorrupted`
  - `internal`
- Many errors are classified heuristically from message text in [backend/src/error.rs](backend/src/error.rs).

### 5.6 Important cache semantics

Current cache behavior is not just "fast rerun":

- The cache itself is in-memory only.
- It stores serialized `JobResult` values, not just artifact paths.
- A cache hit reuses the previously stored `JobResult` payload as-is.
- The returned `JobSnapshot.study_id` is set for the current study, but the embedded `result.payload.studyId` comes from the original cached result.

Implication:

- reopening the same file and hitting cache can produce a snapshot whose top-level `studyId` matches the current study while the nested result payload still contains the older study id
- this affects render, analyze, and process cache hits because none of those cache-hit paths rewrite the stored payload before returning it

That mismatch is a real current behavior and should be treated as part of the migration baseline.

## 6. Artifact Path Inventory

### 6.1 Disk cache root

The disk cache root is:

- `env::temp_dir()/xrayview`

Implemented in [backend/src/cache/disk.rs](backend/src/cache/disk.rs).

### 6.2 Artifact namespaces and filenames

Artifacts are written under:

- `cache/artifacts/render/<fingerprint>.png`
- `cache/artifacts/process/<fingerprint>.png`
- `cache/artifacts/process/<fingerprint>.dcm` when the user did not choose an output path
- `cache/artifacts/analyze/<fingerprint>.png`

Notes:

- render jobs always write a PNG preview
- analyze jobs always write a PNG preview in desktop mode because `BackendAppState::analyze_study(...)` always supplies a preview path
- process jobs always write a preview PNG in desktop mode and write a DICOM either to the user-selected path or to the temp cache path above

### 6.3 Persistence paths

The recent-study catalog lives at:

- `state/catalog.json` under the disk cache root

Implemented in [backend/src/persistence/catalog.rs](backend/src/persistence/catalog.rs).

Behavior:

- newest entry first
- duplicate paths are deduped by `input_path`
- capped at 10 entries
- corrupt JSON is renamed to `catalog.corrupt.json`
- `open_study(...)` ignores persistence write failures (`let _ = ...`)

### 6.4 Preview path delivery to the frontend

Preview delivery is coupled to Tauri's asset protocol:

- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts) turns preview file paths into browser-loadable URLs with `convertFileSrc(...)`
- [frontend/src-tauri/tauri.conf.json](frontend/src-tauri/tauri.conf.json) only allows asset loading from:
  - `$TEMP/xrayview/cache/artifacts/*`
  - `$TEMP/xrayview/cache/artifacts/**/*`

Implications:

- preview images must remain under the temp artifact tree to load in the desktop UI
- user-selected DICOM output paths are not subject to asset loading because the UI only displays preview PNGs, not the output DICOM file

## 7. Current Shell/Backend/Frontend Data Flow

### 7.1 Open study flow

1. React UI calls `pickDicomFile()` in [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts).
2. Tauri opens a native file picker through `pick_dicom_file`.
3. React calls `openStudy(inputPath)`.
4. Tauri `open_study` calls `BackendAppState::open_study_command(...)`.
5. `open_study(...)` in [backend/src/app/state.rs](backend/src/app/state.rs):
   - runs `describe_study(...)`
   - extracts measurement scale only
   - allocates a new UUID-backed `studyId`
   - stores the study in the in-memory registry
   - writes the recent-study catalog best-effort
6. React immediately starts a render job for the returned `studyId`.

Important detail:

- opening a study does not load or cache full source pixels
- every render/process/analyze execution re-reads and decodes the source file from disk

### 7.2 Render flow

1. Frontend starts `start_render_job`.
2. Backend checks memory cache and active fingerprints.
3. If a real job is needed, a thread:
   - validates input path existence
   - fully loads and decodes the source study
   - renders default grayscale preview
   - writes PNG into the temp artifact tree
4. Frontend polls `get_job` until terminal.
5. The resulting preview path is translated with `convertFileSrc(...)` for display.

### 7.3 Process flow

1. Frontend builds a `ProcessStudyCommand` in [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts).
2. That command carries:
   - `studyId`
   - optional user output path
   - chosen preset id
   - only the explicit overrides that differ from the baseline preset
3. Backend resolves the preset plus overrides in [backend/src/app/mod.rs](backend/src/app/mod.rs).
4. Worker thread:
   - validates request
   - decodes source pixels
   - renders default grayscale source preview
   - applies grayscale processing
   - optionally applies palette
   - optionally creates compare output
   - writes preview PNG
   - exports a derived DICOM Secondary Capture
5. Frontend polls job state and then displays the preview plus the output path.

### 7.4 Analyze flow

1. Frontend starts `start_analyze_job`.
2. Backend checks cache/dedupe.
3. Worker thread:
   - validates input path
   - decodes source study
   - renders grayscale preview
   - writes analysis preview PNG
   - runs tooth-analysis heuristics on grayscale bytes
   - derives suggested editable annotations from the analysis result
4. Frontend polls until completion and then replaces auto-tooth suggestions in local state.

### 7.5 Manual measurement flow

1. The viewer produces a line annotation in frontend state.
2. Frontend sends it to `measure_line_annotation`.
3. Backend recalculates pixel length and optional calibrated length.
4. Frontend stores the returned annotation locally.

The backend does not persist annotations. This is a stateless measurement helper call.

## 8. Migration-Relevant Observations And Current-State Mismatches

These are not proposals. They are behavior notes that matter because phase 0 is meant to ground later migration work in reality.

- Rust is still the build-time contract authority. Frontend `dev` and `build` both depend on the Rust contract generator.
- The desktop UI contract for jobs is polling, not event subscription, even though Tauri emits events.
- Preview loading is coupled to Tauri asset protocol scope and the exact temp artifact directory layout.
- `open_study(...)` only extracts measurement scale and registers a study. It does not pre-decode or cache image pixels.
- Recent-study persistence is write-only from the current desktop UX.
- `validate_input_file(...)` only checks file existence. It does not enforce `.dcm` or `.dicom` extensions even though the README describes extension validation.
- The open-file dialog does not apply a DICOM extension filter.
- CLI tooth analysis does not actually require `--preview-output`; the preview path is optional in code.
- `StudyDescription` currently contains only `measurementScale`; it does not include image dimensions even though the README describes dimensions in `--describe-study` output.
- Cache hits can return nested result payloads whose `studyId` belongs to an older study open of the same source path.
- Memory cache survives only for the current process lifetime. Disk artifacts can remain on disk without being part of any restored cache on restart.

## 9. Phase 0 Exit Criteria Check

Phase 0 asked for:

- backend capability list: covered in section 2
- entrypoint list: covered in section 3
- job lifecycle list: covered in section 5
- artifact path list: covered in section 6
- shell/backend/frontend data flow notes: covered in section 7
- compare docs to actual code and list every frontend/backend call path: covered in sections 4 and 8

This file is the completed written inventory for phase 0.
