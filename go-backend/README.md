# xrayview Go Backend

This module is the current Go sidecar backend for the migration path. Phase 7 established the local HTTP transport, phase 8 let the Tauri shell manage this process automatically for the `go-sidecar` runtime, phase 9 moved the processing manifest endpoint into Go, and phase 10 moved `open_study` registration into Go.

Current scope:

- load config from environment
- initialize cache and persistence roots
- expose a local loopback HTTP/JSON server
- return the frozen processing manifest for `get_processing_manifest`
- validate DICOM metadata and register studies for `open_study`
- extract `open_study` metadata needed for migration parity: rows, columns, spacing tags, window defaults, photometric interpretation, and transfer syntax UID
- populate `measurementScale` when spacing tags are present
- write the recent-study catalog hook on study open
- publish health/runtime metadata
- reserve the command namespace expected by the frontend `go-sidecar` adapter
- enforce local-only host/origin rules for the sidecar transport

Current non-goals:

- no Go pixel decode yet
- no Go DICOM export yet
- no job execution yet
- no render/process/analyze execution yet

## Commands

```bash
go run ./cmd/xrayviewd
go run ./cmd/xrayview-cli print-config
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

Current metadata-reader limits:

- full pixel decode remains out of scope for this phase
- deflated transfer syntax is still rejected in the prototype reader

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
