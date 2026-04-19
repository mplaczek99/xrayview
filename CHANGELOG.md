# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
