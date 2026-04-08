# xrayview Go Backend

This module is the current Go sidecar backend for the migration path. Phase 7 established the local HTTP transport, phase 8 let the Tauri shell manage this process automatically for the `go-sidecar` runtime, phase 9 moved the processing manifest endpoint into Go, phase 10 moved `open_study` registration into Go, phase 11 proved metadata reading in Go, phase 12 locked the pixel-decode strategy around a narrow Rust helper instead of a premature pure-Go commitment, phase 13 added the temporary Rust decode helper plus a Go invocation layer, phase 14 introduced the shared Go-native imaging model, phase 15 ported the core Rust grayscale windowing semantics, and phase 16 now renders grayscale PNG previews fully in Go on top of that decode boundary.

Current scope:

- load config from environment
- initialize cache and persistence roots
- expose a local loopback HTTP/JSON server
- return the frozen processing manifest for `get_processing_manifest`
- validate DICOM metadata and register studies for `open_study`
- extract `open_study` metadata needed for migration parity: rows, columns, spacing tags, window defaults, photometric interpretation, and transfer syntax UID
- inspect decode-relevant DICOM metadata for migration planning
- invoke the temporary Rust decode helper from Go and validate its normalized source-study payload
- normalize decoded source studies into a shared `internal/imaging` model with explicit image-format metadata
- validate source-image and preview-image buffer geometry before later render-pipeline work
- resolve embedded, manual, and full-range grayscale window modes with Rust-equivalent mapping behavior
- render grayscale preview pixels from decoded source studies in Go
- encode rendered preview buffers as PNG output
- populate `measurementScale` when spacing tags are present
- write the recent-study catalog hook on study open
- publish health/runtime metadata
- reserve the command namespace expected by the frontend `go-sidecar` adapter
- enforce local-only host/origin rules for the sidecar transport

Current non-goals:

- no Go pixel decode yet
- phase 12 intentionally does not claim pure-Go decode readiness from the current narrow sample corpus
- no HTTP command uses the Rust decode helper yet
- no Go DICOM export yet
- no job execution yet
- no live HTTP render/process/analyze execution yet beyond the internal render-preview CLI and reusable phase 16 render package

## Commands

```bash
go run ./cmd/xrayviewd
go run ./cmd/xrayview-cli print-config
go run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
go run ./cmd/xrayview-cli decode-source ../images/sample-dental-radiograph.dcm
go run ./cmd/xrayview-cli render-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview.png
go run ./cmd/xrayview-cli render-preview --full-range ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview-full-range.png
go run ./cmd/xrayview-cli list-commands
```

When you run the desktop app in `go-sidecar` mode through `npm run tauri:dev`
or `npm run tauri:build`, the shell now prepares and launches this binary for
you. Manual `go run ./cmd/xrayviewd` is mainly useful for direct transport
inspection during migration work.

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
- `open_study` validates DICOM metadata, returns a Go-generated `StudyRecord`, and records the recent-study catalog hook
- other command routes still return structured not-implemented backend errors
- the phase 16 render pipeline currently exists as internal Go code plus the `render-preview` CLI path; the HTTP job endpoints remain for phase 17

Current metadata-reader limits:

- full pixel decode remains out of scope for this phase
- deflated transfer syntax is still rejected in the prototype reader
- the committed sample corpus contains only native single-frame monochrome explicit-VR-little-endian studies
- phase 12 therefore locks decode strategy to Go orchestration plus a narrow Rust decode helper for phase 13
- the helper emits normalized source-study JSON for Go consumption, but render/process HTTP commands still remain for later phases

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
