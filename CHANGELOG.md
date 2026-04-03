# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
