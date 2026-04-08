# xrayview Go Backend

This module is the current Go sidecar backend for the migration path. Phase 7 established the local HTTP transport, phase 8 let the Tauri shell manage this process automatically for the `go-sidecar` runtime, phase 9 moved the processing manifest endpoint into Go, phase 10 moved `open_study` registration into Go, phase 11 proved metadata reading in Go, phase 12 locked the pixel-decode strategy around a narrow Rust helper instead of a premature pure-Go commitment, phase 13 added the temporary Rust decode helper plus a Go invocation layer, phase 14 introduced the shared Go-native imaging model, phase 15 ported the core Rust grayscale windowing semantics, phase 16 rendered grayscale PNG previews fully in Go on top of that decode boundary, phase 17 exposed live Go-owned render jobs over the sidecar HTTP command surface, phase 18 ported the grayscale processing controls into reusable Go code, phase 19 completed the preview-side processing pipeline with palette and compare support, phase 20 exposed live Go-owned process jobs, phase 21 moved the memory cache into Go, phase 22 aligned the disk path policy, phase 23 extracted the Go job registry, phase 24 completed recent-study persistence, phase 25 moved `measure_line_annotation` to Go, phase 26 moved annotation suggestion mapping to Go, phase 27 ported the reusable tooth-analysis primitives into Go, phase 28 exposed live Go-owned analyze jobs, phase 29 proved pure-Go Secondary Capture export, phase 30 added an optional narrow Rust export helper fallback without changing Go ownership of the workflow, phase 31 made the default desktop `processStudy` path Go-owned even while the broader desktop runtime remains Rust-first, phase 32 routed the default desktop `measureLineAnnotation` path through Go as well, phase 33 made the default desktop `openStudy` path Go-owned too, phase 34 made the default desktop `analyzeStudy` path Go-owned as well, phase 35 moved the supported headless CLI workflows onto the Go CLI while preserving the existing flag surface, and phase 37 hardens the Tauri release path so packaged desktop builds and release smoke tests now verify that this bundled sidecar actually launches.

Current scope:

- load config from environment
- initialize Rust-compatible cache and state roots under a shared disk layout
- expose a local loopback HTTP/JSON server
- return the frozen processing manifest for `get_processing_manifest`
- validate DICOM metadata and register studies for `open_study`
- serve the default desktop `openStudy` flow even when the selected frontend runtime is `legacy-rust`
- extract `open_study` metadata needed for migration parity: rows, columns, spacing tags, window defaults, photometric interpretation, and transfer syntax UID
- inspect decode-relevant DICOM metadata for migration planning
- invoke the temporary Rust decode helper from Go and validate its normalized source-study payload
- normalize decoded source studies into a shared `internal/imaging` model with explicit image-format metadata
- validate source-image and preview-image buffer geometry before later render-pipeline work
- resolve embedded, manual, and full-range grayscale window modes with Rust-equivalent mapping behavior
- render grayscale preview pixels from decoded source studies in Go
- apply grayscale processing math in Go for invert, brightness, contrast, and histogram equalization
- apply `hot` and `bone` palettes in Go
- compose side-by-side compare previews in Go
- encode rendered preview buffers as PNG output
- encode Secondary Capture DICOM output in Go
- optionally route Secondary Capture writing through a narrow Rust helper when `XRAYVIEW_SECONDARY_CAPTURE_EXPORTER=rust-helper`
- execute `start_render_job` in Go and store preview artifacts under the cache tree
- execute `start_process_job` in Go and store processed preview artifacts under the cache tree
- serve the default desktop `processStudy` flow even when the selected frontend runtime is `legacy-rust`
- execute `start_analyze_job` in Go and store analysis preview artifacts under the cache tree
- serve the default desktop `analyzeStudy` flow even when the selected frontend runtime is `legacy-rust`
- return live `get_job` snapshots for render jobs
- return live `get_job` snapshots for process jobs
- return live `get_job` snapshots for analyze jobs
- support render/process job cancellation, dedupe, and cache hits in the Go job registry
- support analyze-job cancellation, dedupe, and cache hits in the Go job registry
- populate `measurementScale` when spacing tags are present
- execute `measure_line_annotation` in Go with Rust-parity pixel and calibrated length rounding
- serve the default desktop `measureLineAnnotation` flow even when the selected frontend runtime is `legacy-rust`
- map `ToothAnalysis` results into editable suggested annotations in Go
- run reusable tooth-analysis primitives in Go over grayscale previews: normalization, toothness, morphology, candidate scoring, geometry extraction, and measurement bundling
- execute full tooth analysis in Go and return suggested annotations
- own recent-study catalog persistence on study open, including duplicate-path collapse, 10-entry truncation, and corrupted-catalog recovery
- own the supported headless CLI workflow surface previously exposed by the Rust CLI
- publish health/runtime metadata
- reserve the command namespace expected by the frontend `go-sidecar` adapter
- enforce local-only host/origin rules for the sidecar transport

Current non-goals:

- no Go pixel decode yet
- phase 12 intentionally does not claim pure-Go decode readiness from the current narrow sample corpus
- the Rust export helper remains a temporary fallback rather than the primary export path

## Commands

```bash
go run ./cmd/xrayviewd
go run ./cmd/xrayview-cli -- --describe-presets
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --describe-study
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preview-output /tmp/xrayview-preview.png
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --output /tmp/xrayview-output.dcm --preset xray
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --analyze-tooth --preview-output /tmp/xrayview-analysis.png

# Migration utility subcommands
go run ./cmd/xrayview-cli print-config
go run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
go run ./cmd/xrayview-cli decode-source ../images/sample-dental-radiograph.dcm
go run ./cmd/xrayview-cli render-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview.png
go run ./cmd/xrayview-cli render-preview --full-range ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview-full-range.png
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-processed.png --brightness 10 --contrast 1.4 --equalize
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-processed-bone.png --brightness 10 --contrast 1.4 --equalize --palette bone
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-compare.png --brightness 10 --contrast 1.4 --equalize --palette bone --compare
go run ./cmd/xrayview-cli export-secondary-capture ../images/sample-dental-radiograph.dcm /tmp/xrayview-exported.dcm --brightness 10 --contrast 1.4 --equalize
go run ./cmd/xrayview-cli list-commands
```

The supported headless DICOM workflow now uses the top-level flag interface shown above. The older subcommands remain available for migration-specific decode, render, and transport inspection work.

When you run the desktop app through `npm run tauri:dev` or
`npm run tauri:build`, the shell now prepares and launches this binary for any
desktop runtime that needs Go-backed behavior. In the default `legacy-rust`
runtime that currently means `openStudy`, `processStudy`, `analyzeStudy`, and
`measureLineAnnotation`; only `renderStudy` still uses the Rust bridge after
lazily registering a matching Rust-side study record. In
`go-sidecar` mode, more of the desktop command surface is routed through this
binary. Manual
`go run ./cmd/xrayviewd` is mainly useful for direct transport inspection during
migration work.

## Transport

Default base URL:

- `http://127.0.0.1:38181`

Exposed routes:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

Current command behavior:

- `get_processing_manifest` returns the frozen processing manifest payload
- `open_study` validates DICOM metadata, returns a Go-generated `StudyRecord`, updates the Go-owned recent-study catalog, and now backs the default desktop open flow
- `start_render_job` runs the phase 17 render pipeline through the Go job service
- `start_process_job` runs the Go preview-processing pipeline, writes the processed DICOM through the configured export writer, and returns the completed output path
- `start_analyze_job` runs the Go analysis pipeline and returns suggested annotations through the normal job/result path
- `get_job` and `cancel_job` now work for Go-owned render, process, and analyze jobs
- `measure_line_annotation` recomputes pixel and calibrated lengths in Go using the registered study spacing metadata

Current metadata-reader limits:

- full pixel decode remains out of scope for this phase
- deflated transfer syntax is still rejected in the prototype reader
- the committed sample corpus contains only native single-frame monochrome explicit-VR-little-endian studies
- phase 12 therefore locks decode strategy to Go orchestration plus a narrow Rust decode helper for phase 13
- the helper emits normalized source-study JSON for Go consumption while export and analyze work continue migrating behind it

Transport guarantees:

- loopback-only backend bind addresses
- CORS/preflight handling for Tauri/local dev origins
- runtime metadata that identifies the transport as `local-http-json`

## Environment

- `XRAYVIEW_GO_BACKEND_HOST`
- `XRAYVIEW_GO_BACKEND_PORT`
- `XRAYVIEW_GO_BACKEND_LOG_LEVEL`
- `XRAYVIEW_GO_BACKEND_BASE_DIR`
- `XRAYVIEW_GO_BACKEND_CACHE_DIR`
- `XRAYVIEW_GO_BACKEND_PERSISTENCE_DIR`
- `XRAYVIEW_GO_BACKEND_SHUTDOWN_TIMEOUT`
- `XRAYVIEW_RUST_DECODE_HELPER_BIN`
- `XRAYVIEW_SECONDARY_CAPTURE_EXPORTER`
- `XRAYVIEW_RUST_EXPORT_HELPER_BIN`

Default disk layout when only `XRAYVIEW_GO_BACKEND_BASE_DIR` is set or when no
path overrides are provided:

- `<temp>/xrayview/cache`
- `<temp>/xrayview/cache/artifacts/<namespace>/<key>.<extension>`
- `<temp>/xrayview/state/<name>`
