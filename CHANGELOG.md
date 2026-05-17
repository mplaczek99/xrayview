# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.1] - 2026-05-17

### Added

- Analyze overlay display toggle so users can switch between outlined and filled tooth/bone overlay rendering modes. New `FilledPreview` variant on `ToothOverlayResult`, the `analyzeOverlayFilled` flag in `WorkbenchStudy`, and an "Outline / Filled" toggle in `ViewTab`
- Global workbench status surfaced in the app chrome (top bar) instead of per-tab, so job progress and errors remain visible while switching tabs
- `analyze-preview` utility subcommand on `xrayview-cli` for rendering analyze overlays from a DICOM file with an optional `--filled` flag (`backend/cmd/xrayview-cli/main.go`)

### Changed

- Generated analyze artifacts (`Preview` and `FilledPreview`) are now scoped to individual job sessions instead of cached globally, ensuring session isolation across runs (`backend/internal/jobs/service.go`)
- Bone-level overlay rendered as a continuous background region behind tooth overlays for clearer visual separation
- Tooth feature table model retrained with a stricter Dice coefficient gate to reduce false positives
- `JobCenter` panel collapsed by default for a cleaner initial workbench layout

### Fixed

- Viewport position is preserved when toggling tooth/bone overlays (previously snapped back to fit on every toggle)
- Bone overlay section borders now render correctly: hole artifacts suppressed, contours aligned to section masks, and outline positioning matched to the underlying mask across differing image dimensions (`backend/internal/analysis/teeth.go`)
- BMP and TIFF studies decoded into the internal PNG path now preserve their 8-bit value range and overlay alpha correctly
- SSE stream broadcasts no longer drop frames or stall when clients write past the configured write timeout (`backend/internal/httpapi/sse.go`)
- Viewer pointer interaction listeners properly reattach after a preview reload, fixing drag/pan loss after switching the active study
- Save-location and study file pickers in the desktop shell surface errors and cancellation back to the workbench store instead of failing silently
- Processing control slider edits no longer submit stale form state — pending controls are merged and flushed before dispatch; introduced `setProcessingControl` and a synchronous flush in `runActiveStudyProcessing`
- Process job cache key now includes the output file path so different save destinations no longer collide on a shared cached artifact
- Measurement scale cache key aligned with the preview cache derivation so measurements no longer reuse stale scale data
- Boolean processing preset overrides (invert, equalize) in the legacy CLI no longer default to `false` when unset — proper tri-state handling via a new `optionalBoolFlag` type in `backend/cmd/xrayview-cli/legacy_cli.go`
- Decode cache and source preview cache invalidate when the underlying study file is replaced (size, mtime, or file state change), preventing stale cached artifacts on overwrite
- Picked output path is now applied to the captured study via study ID instead of re-reading the active study after dialog close, eliminating a race when the active study changes mid-dialog
- Catalog read failures are reported to the user instead of silently failing on recent-studies load (`backend/internal/persistence/catalog.go`)
- Preview buffers are correctly released when a job is cancelled mid-stage
- HTTP body buffer pool reset in tests for proper isolation

### Removed

- Viewer hover coordinate readout from `ViewerCanvas` and `usePointerInteractions`

### Security

- Desktop preview asset serving now restricts file paths to the configured cache root, preventing path traversal escapes from the sidecar's asset endpoint (`desktop/app.go`, `desktop/sidecar.go`)

## [0.4.0] - 2026-05-10

### Added

- Tooth and bone level analysis pipeline, reintroduced with a new learned-model approach (the v0.3.1 release removed the legacy heuristic version):
  - `start_analyze_job` command, `AnalyzeStudyCommand` / `AnalyzeStudyCommandResult` (with a `mode` string describing the run), `JobKindAnalyzeStudy`, and the `analyzeStudy` `JobResultPayload` variant in the shared contract
  - `backend/internal/analysis/teeth.go` and `teeth_test.go` covering tooth detection, contour smoothing, speck filtering, and bone-level outline extraction
  - Embedded learned-model assets under `backend/internal/analysis/`: `bone_exemplar_model.bin.gz`, `bone_feature_table_model.bin.gz`, `feature_table_model.bin.gz`, and `learned_model.bin`, each loaded by a sibling `*.go` file with magic-string and gzip framing
  - Asset-generation tools `backend/internal/analysis/tools/bonetable/main.go` and `backend/internal/analysis/tools/learnedmodel/main.go`
  - Row-parallel work helpers in `backend/internal/analysis/parallel.go` used across scoring, mask generation, and stroke coverage
  - Frontend wiring: `WorkbenchStudy.analysisPreview` / `analysisJobId`, `AnalysisResult` type, `applyAnalyzeJob` in `frontend/src/app/store/applyJob.ts`, and a working "Analyze" button in `ViewTab` that dispatches `runActiveStudyAnalysis` and overlays the analysis preview when ready
- Configurable backend job worker pool via `XRAYVIEW_BACKEND_WORKERS` (default `min(4, runtime.NumCPU()-1)`, floor 1); documented in `backend/README.md`
- New `BackendService` interface in `backend/internal/contracts/service.go` consumed by transport adapters, replacing the per-command nil-check fan-out in `desktop/app.go` with a single generic `dispatch` helper
- uint16 source image storage path: `imaging.SourceStorage` enum (`float32` / `uint16`), `SourceImage.Uint16Pixels`, `FitsUint16`, `PixelCount()`, and `StorageKind()`. The DICOM decoder records uint16 fit during decode and emits uint16 storage when the modality range allows, halving in-memory pixel buffer size; the render pipeline has a dedicated uint16 path
- SSE dropped-frame observability on the broadcaster, plus an SSE bench test
- Cached preview-root resolution on the `/preview` endpoint (avoids `filepath.EvalSymlinks` on every request)
- Cached render LUTs across job runs
- `runtime.forEachJob` streaming callback so the frontend job poller can apply snapshots without an intermediate array
- Pending job IDs tracked as a dedicated `ReadonlySet<string>` on `WorkbenchState` for O(1) reads (previously derived by filtering all jobs each tick)
- Annotation drag override (`draftLineOverride`) so live drags render smoothly without committing intermediate state through the store
- Frontend module split for testability and rerender hygiene:
  - `frontend/src/app/store/applyJob.ts` and `selectors.ts` extracted from `workbenchStore.ts`
  - `frontend/src/lib/backendUtils.ts`, `desktopBackend.ts`, `jobIds.ts`, and `mockBackend.ts` (formerly `backend.ts`) split from the monolithic backend module
  - `frontend/src/features/jobs/progressEstimator.ts`, `progressFormatting.ts`, and `useProgressClock.ts` carved out of `progressTiming.ts`
  - `frontend/src/features/viewer/usePointerInteractions.ts`, `useViewportFrame.ts`, and `useWheelZoom.ts` carved out of `ViewerCanvas.tsx`
- Validation scripts under `frontend/scripts/`:
  - `validate-annotation-edit-drag.mjs`
  - `validate-fast-poll-cadence.mjs`
  - `validate-pending-job-count-selector.mjs`
  - `validate-pointer-interactions.mjs`
  - `validate-processing-tab-rerenders.mjs`
  - `validate-runtime-get-jobs-normalization.mjs`
  - `validate-singleton-job-id-dedupe.mjs`
- Backend benchmark tests: `backend/internal/httpapi/sse_bench_test.go`, `backend/internal/persistence/catalog_bench_test.go`

### Changed

- Worker pool size is now resolved at startup from `runtime.NumCPU()` and the `XRAYVIEW_BACKEND_WORKERS` override instead of the previous hard-coded 3
- Backend job service lifecycle collapsed into a generic `startJob` / `jobSpec` helper that shares the validate → fingerprint → cache → reserve → launch flow across render, analyze, and process jobs
- Memory cache `StoreSourcePreview` now defensively copies the caller's preview pixels so callers can return pooled buffers immediately after the call (previously it took ownership)
- `useJobs` polling cadence reworked: an `ACTIVE_POLL_MS` (500 ms) base for live jobs with a `RECENT_TRANSITION_POLL_MS` (200 ms) burst after a state change, plus event-driven suppression when SSE updates are fresh
- `ViewTab` now prefers `analysisPreview` over `originalPreview` when an analysis result exists, and the Analyze button shows an "Analyzing..." label while the job is in flight
- Annotation selection is preserved across pan gestures
- `GrayscaleControls` and the `ProcessingTab` study selection path memoized to cut needless rerenders
- `JobCenter` now titles `analyzeStudy` jobs as "Analyze Teeth And Bone"
- Sidecar HTTP client `MaxIdleConns` / `MaxIdleConnsPerHost` increased
- Catalog record persistence streamlined (fewer marshal allocations on each write)
- Study registry eviction comments and behavior clarified
- Pooled HTTP request body buffers now bounded so a single oversized request can't poison the pool
- Streaming snapshots used in job polling instead of full slice copies
- Fast path added for singleton job batches (`getJobs` with one ID)
- Map-based feature table lookups replace the linear scan

### Performance

This release contains a large body of optimization work:

- **DICOM decode**: uint16 storage halves source-buffer memory; uint16 fit recorded once during decode; decode cache footprint reduced
- **Render**: cached LUTs across runs, preview buffer reuse via `bufpool`, dedicated uint16 render path
- **Analysis**: row-parallel scoring, mask reuse, reduced component allocations, optimized binary morphology, optimized stroke coverage and contour smoothing
- **Processing**: parallelized comparison rendering with a precomputed gray LUT, faster grayscale blur borders, faster palette application
- **HTTP / SSE**: improved broadcast frame handling, dropped-frame metric exposed, pooled request body buffer cap, larger sidecar idle connection pool
- **Jobs**: streaming snapshot reads, singleton-batch fast path, faster registry job reads, configurable worker pool
- **Frontend**: `ProcessingTab` rerender reduction via memoized selectors and stable callbacks; pending job IDs tracked as a set; SSE-driven polling skips when events are fresh; `runtime.forEachJob` avoids intermediate allocations
- **Pointer interactions**: stabilized drag listeners and annotation edit drags without losing pointer capture

### Fixed

- Annotation selection cleared when a pan gesture started; selection now survives panning
- Annotation edit drags occasionally lost their target on rapid pointer moves
- Pointer drag listeners could leak after some interrupted gestures
- Tooth detection produced jagged outlines and tiny specks; a smoothing pass plus speck filter cleans both up
- DICOM decode cache held more memory than necessary on warm sessions

### Removed

- Dead tooth-analysis helpers carried forward from the earlier pipeline that were no longer reachable
- Internal optimization-plan and cleanup-plan tracking documents that were authored and consumed within this release cycle

## [0.3.1] - 2026-04-23

### Added

- `PolylineAnnotation` in the shared contract and an `AnnotationBundle.polylines` field, rendered by `AnnotationLayer` as either an SVG `polyline` or `polygon` depending on the `closed` flag
- Loopback-only `GET /preview` endpoint on the Go backend (`backend/internal/httpapi/preview.go`) that serves cache artifacts, with `filepath.EvalSymlinks` + `filepath.Rel` containment checks against the configured cache root
- Node.js 18.18+ support: `engines` field on the root and `frontend/` packages, Vite downgraded to `^5.4.18`, `@vitejs/plugin-react` downgraded to `^4.3.4`, README prerequisite updated
- Linux desktop build prerequisite check in `desktop/scripts/build.mjs` covering `gtk+-3.0` and either `webkit2gtk-4.1` or `webkit2gtk-4.0`, with an actionable error message when packages are missing
- Stable `data-testid` attributes across processing controls, view-tab toolbar buttons, and Job Center rows for browser automation
- Vite dev server explicit `host: "127.0.0.1"` binding
- Shared `frontend/src/lib/commandBuilders.ts` module owning `buildProcessStudyCommand`
- `.gitignore` entries for `.playwright-cli/` and `backend/internal/analysis/_debug/`

### Changed

- `images/README.md` now documents local BMP/TIF asset directories instead of the bundled dental radiograph
- `backend/internal/httpapi` tests reorganised around the new `preview_test.go`; the aggregated `router_test.go` suite was retired
- `ViewTab` / `ViewSidebar` CSS renamed from `study-analysis*` to `study-layout*` to reflect the simplified layout after analysis removal
- Backend job service worker pool comments and `bufpool` docs updated to reflect two job kinds (render, process)

### Fixed

- Brightness and Contrast number inputs no longer accept out-of-range values. Previously the slider clamped natively on render but the number input did not, so typing `999` left slider, state, and displayed input desynced and the backend rejected the job. Contrast also collapsed empty or negative input to `0`, below the `0.1` minimum
- Mid-edit states on Brightness/Contrast (empty field, lone `-`, lone `.`) no longer parse as NaN and yank the slider to the minimum — partial input now preserves the last committed value while real out-of-range numbers still clamp
- Windows release archives in `build-release-artifacts.yml` now use POSIX paths
- Archive workflow no longer relies on `grep -P`

### Removed

- Legacy tooth analysis pipeline:
  - `start_analyze_job` command, `AnalyzeStudyCommand` / `AnalyzeStudyCommandResult`, `JobKindAnalyzeStudy`, and the `autoTooth` `AnnotationSource` variant
  - Contract types `ToothAnalysis`, `ToothImageMetadata`, `ToothCalibration`, `ToothCandidate`, `ToothMeasurementBundle`, `ToothMeasurementValues`, `ToothGeometry`, `BoundingBox`, `LineSegment`, `Point`
  - `backend/internal/analysis/`, `backend/internal/annotations/suggestions.go`, the analyze-result memory cache, and the `--analyze-tooth` legacy CLI flag
  - Frontend plumbing: `measureActiveStudy`, `replaceSuggestedAnnotations`, `selectActiveStudyJobs`, `applyAnalyzeJob`, `ToothAnalysisResult`, `analysisJobId` on `WorkbenchStudy`, tooth measurement sections in `ViewSidebar`, overlay rendering in `DicomViewer`, and `.viewer-stage__overlay*` / `.measurement-card--analysis` styles
- Bundled sample DICOM fixtures: `images/sample-dental-radiograph.dcm`, `images/sample-dental-radiograph_processed.dcm`, and the pre-recorded analyze preview / study snapshot under `images/sample-dental-radiograph/`
- `AGENT_MIDDLEWARE_PLAN.md`
- `frontend/src/lib/wails.ts` global augmentation and related unused runtime types

## [0.3.0] - 2026-04-18

### Added

- Go backend service (`backend/`) replacing the Rust crate, organized into `internal/` packages: `analysis`, `annotations`, `app`, `bufpool`, `cache`, `config`, `contracts`, `dicommeta`, `export`, `httpapi`, `imaging`, `jobs`, `logging`, `persistence`, `processing`, `render`, `studies`
- Dedicated HTTP server entrypoint (`backend/cmd/xrayviewd`) and headless CLI (`backend/cmd/xrayview-cli`) sharing the `internal/` library
- Wails-based desktop shell (`desktop/`) replacing the Tauri shell, with sidecar lifecycle management and local `/preview` artifact serving
- Shared `contracts/` Go module with `backend-contract-v1.schema.json` as the language-neutral source of truth, generating both TypeScript (`frontend/src/lib/generated/contracts.ts`) and Go (`contracts/contractv1/bindings.go`) bindings via `npm run contracts:generate`
- Loopback HTTP transport (`127.0.0.1:38181`) between desktop shell and backend, replacing the in-process Tauri command bridge
- Server-Sent Events stream for job progress updates, replacing HTTP long-polling
- Job request batching and deduplication at the frontend command layer
- Exponential backoff for any remaining job-status polling fallback
- Fixed-size worker pool for job execution, replacing per-job goroutines
- Context-aware DICOM decode cancellation honoring job cancel requests mid-decode
- Configurable HTTP server timeouts on the backend
- Explicit HTTP transport with connection pooling on the desktop sidecar client
- TTL-gated `os.Stat` calls on cache hits to reduce filesystem syscalls
- HTTP cache-control headers for preview artifacts
- BMP and TIFF study import support alongside DICOM
- Frontend runtime selector (`runtime.ts`, `runtimeConfig.ts`) for `mock` vs `desktop` modes, with `XRAYVIEW_BACKEND_RUNTIME` / `XRAYVIEW_BACKEND_URL` overrides
- `desktop/` benchmark suite (`app_bench_test.go`) and Go backend benchmark fixtures (`jobs/bench_test.go`)
- Frontend validation scripts under `frontend/scripts/validate-*.mjs` covering selectors, batched updates, debounce controls, GPU transforms, exponential backoff, SSE polling reduction, and annotation memoization
- Release launch smoke test (`frontend/scripts/release-launch-smoke.mjs`) for Wails packaged builds
- Parallel build orchestration (`frontend/scripts/parallel-build.mjs`) running `tsc` and Vite concurrently
- TypeScript incremental compilation (`tsconfig.json` with `incremental: true`)
- Vite vendor/app chunk splitting and lazy-loaded `ProcessingTab`
- Pre-recorded analyze and process snapshot fixtures under `images/sample-dental-radiograph/` for browser-only mock mode
- Recent-studies catalog seeded with `recent-study-catalog.json`
- Playwright CLI tooling configuration (`frontend/.playwright/cli.config.json`)

### Changed

- Migrated the entire backend from Rust to Go; backend, desktop, and contracts are now three independent Go modules wired via `replace` directives (no `go.work`)
- Migrated the desktop shell from Tauri to Wails v2, with native dialogs and window lifecycle owned by `desktop/app.go`
- Replaced in-process Tauri command invocation with HTTP command dispatch over a loopback-only listener
- Reworked `frontend/src/lib/backend.ts` around the HTTP transport and generated contract types
- Restructured `frontend/src/app/store/workbenchStore.ts` to consume SSE job updates and batched state writes
- Memoized `AnnotationLayer` rendering and selector reads to reduce `ViewTab` re-renders
- Debounced processing-control updates to coalesce rapid slider changes into a single job dispatch
- Routed CSS image positioning through GPU-accelerated transforms instead of layout-affecting properties
- Sorted detection results once at the source instead of re-sorting per consumer
- Pre-allocated maps in hot paths (analysis aggregation) to avoid growth churn
- Tightened cache key derivation so equivalent processing requests collapse into a single cached artifact
- Updated `README.md` to document the Wails/Go architecture, repository layout, and setup steps
- Reformatted the Go backend with `gofmt`
- Updated GitHub Actions workflows (`build-release-artifacts.yml`, `publish-release.yml`) for the Go/Wails toolchain
- Fixed CI Go build cache key to cover both `backend/` and `desktop/` modules
- Documentation pass added human-style comments across `analysis`, `jobs`, `dicommeta`, `export`, `service`, and HTTP transport packages

### Performance

Performed MANY optimizations, including...
- Eliminated string/bytes copies in the HTTP command request/response path on the desktop sidecar
- Reused buffer pool (`backend/internal/bufpool`) for hot allocation sites
- TTL-gated cache stat calls to skip redundant `os.Stat` on warm cache hits
- Connection pooling via explicit `http.Transport` on the sidecar client
- Worker-pool job execution avoiding unbounded goroutine spawn under burst load
- Frontend bundle split into vendor and app chunks with `ProcessingTab` lazy-loaded on demand
- TypeScript incremental builds and parallelized `tsc` + Vite to shorten frontend build wall time

### Removed

- Rust backend crate (`backend/src/**`, `backend/Cargo.toml`, `backend/Cargo.lock`, `backend/tests/cli.rs`)
- Tauri desktop shell (`frontend/src-tauri/`, including `Cargo.toml`, `Cargo.lock`, `tauri.conf.json`, capabilities, icons, `src/main.rs`, `build.rs`)
- Tauri build/dev orchestration scripts (`frontend/scripts/tauri-build.mjs`, `frontend/scripts/tauri-dev.mjs`, `frontend/scripts/prepare-tauri-target.mjs`)
- Go workspace file (no `go.work`); cross-module deps now use `replace` directives
- HTTP long-polling for job state (superseded by SSE)
- `BUGFIX_ROADMAP.md`, `OPTIMIZATION_PLAN.md`, and the commenting plan documents (work merged into the codebase)

## [0.2.2] - 2026-04-04

### Added

- Dedicated view sidebar with a more compact measurement workflow
- Batch measurement for all detected teeth, including completion timing
- Job progress timing utilities with smoother ETA feedback

### Changed

- Reworked the View and Processing tabs to simplify the workstation layout
- Split grayscale controls into a dedicated panel and removed command preview / advanced pipeline ordering from processing
- Refined visual styling and mock-study data used in browser-only development

### Fixed

- Processing completion status icon state in the Job Center
- Tooth measurement and auto-detection integration across the backend contract and frontend workbench flow

## [0.2.1] - 2026-04-03

### Added

- Library-first backend architecture (`lib.rs`) with modular layout: `api/`, `app/`, `study/`, `render/`, `processing/`, `analysis/`, `annotations/`, `export/`, `jobs/`, `cache/`, `persistence/`
- API contracts system (`api/contracts.rs`) as single source of truth for TypeScript types, with auto-generation via `generate-contracts.mjs`
- Study registry and workbench store for managing open DICOM sessions
- Async job system (`jobs/registry.rs`) with Tauri event-driven progress (`job:progress`, `job:completed`, `job:failed`, `job:cancelled`) and Job Center UI
- Source image pipeline (`study/source_image.rs`) for canonical DICOM pixel data handling
- Render plan and windowing modules for structured preview generation
- Processing pipeline module for composable grayscale filter chains
- Canvas 2D viewer with pan/zoom (`ViewerCanvas.tsx`, `viewport.ts`)
- Annotation layer with line measurement tool (`AnnotationLayer.tsx`, `tools.ts`)
- Calibration-aware measurement service (`analysis/measurement_service.rs`) with physical unit (mm) support
- Auto-tooth detection proposals (`analysis/auto_tooth.rs`)
- Tooth measurement backend workflow
- Secondary capture export module
- Disk and memory caching for rendered artifacts
- Study session persistence catalog
- Structured backend error type with Tauri serialization
- Backend app state (`AppState`) for in-process Tauri integration
- CLI integration tests (`backend/tests/cli.rs`)
- Mock study data for browser-only dev mode
- Release smoke test script (`release-smoke-test.mjs`)
- CSS design token system (`base.css`, `tokens.css`, `utilities.css`)

### Changed

- Restructured backend from monolithic `main.rs` to library crate with thin CLI binary (`bin/xrayview-backend.rs`)
- Replaced Tauri sidecar/shell subprocess bridge with direct in-process backend calls via managed `BackendAppState`
- Updated Tauri asset protocol scope from temp files to `xrayview/cache/artifacts/`
- Rebuilt `App.tsx` around two-tab View/Processing workbench with Zustand-style store
- Substantially rebuilt `ViewTab.tsx` and `ProcessingTab.tsx` for new backend integration
- Expanded `backend.ts` with Tauri invoke wrappers for all new commands

### Fixed

- Viewer canvas not responding to pan/zoom after loading (resize observer only ran on first mount; cached images on remount not detected)
- Processing UI not aligned with backend behavior
- Tooth measurement not triggering on demand
- Temp file race condition from concurrent backend requests (serialized with semaphore)

### Performance

- Pre-computed 256-entry palette lookup table (~4M per-pixel function calls eliminated for 2048x2048 images)
- Specialized 16-bit render path with 65536-entry LUT, eliminating per-pixel float operations
- Zero-copy pixel extraction via direct `PrimitiveValue` pattern matching (saves 8 MB allocation for 2048x2048 16-bit images)
- `into_dynamic_image` consumes by value to avoid cloning the pixel buffer (4-16 MB)
- Early DICOM source object drop to free 8-16 MB during pixel processing

### Removed

- Monolithic `backend/src/main.rs` (replaced by library crate + CLI binary)
- Tauri shell plugin and sidecar subprocess mechanism
- `prepare-sidecar.mjs` script
- `PanelCard.tsx`, `ProcessingLab.tsx`, `TopBar.tsx`, `ColorizeTab.tsx` UI components
