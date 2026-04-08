# xrayview Go Backend

This module owns the supported `xrayview` backend runtime. The Wails desktop
shell starts `cmd/xrayviewd` for live desktop mode, and `cmd/xrayview-cli`
provides the supported headless workflow surface.

## Current Scope

- load config from environment
- initialize cache and persistence roots
- expose a local loopback HTTP/JSON transport
- validate DICOM metadata and register studies for `open_study`
- decode supported source studies directly in Go
- render previews in Go with embedded/manual/full-range window handling
- apply grayscale processing, palettes, and compare output in Go
- write DICOM Secondary Capture export output in Go
- execute render, process, and analyze jobs with dedupe, cancellation, and cache hits
- recompute line measurements in Go
- own recent-study catalog persistence
- publish runtime metadata for the desktop shell

## Current Limits

- deflated transfer syntax is still rejected
- encapsulated multi-frame source decode is still unsupported
- the committed sample corpus does not prove broad compressed-transfer-syntax parity

## Commands

```bash
go run ./cmd/xrayviewd
go run ./cmd/xrayview-cli -- --describe-presets
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --describe-study
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preview-output /tmp/xrayview-preview.png
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --output /tmp/xrayview-output.dcm --preset xray
go run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --analyze-tooth --preview-output /tmp/xrayview-analysis.png
```

Focused diagnostics remain available for backend inspection:

```bash
go run ./cmd/xrayview-cli print-config
go run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
go run ./cmd/xrayview-cli decode-source ../images/sample-dental-radiograph.dcm
go run ./cmd/xrayview-cli render-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-preview.png
go run ./cmd/xrayview-cli process-preview ../images/sample-dental-radiograph.dcm /tmp/xrayview-processed.png --brightness 10 --contrast 1.4 --equalize
go run ./cmd/xrayview-cli export-secondary-capture ../images/sample-dental-radiograph.dcm /tmp/xrayview-exported.dcm --brightness 10 --contrast 1.4 --equalize
go run ./cmd/xrayview-cli list-commands
```

The supported desktop shell now lives in `../desktop`. Use `npm run
wails:build` to build the shell plus this backend, or `npm run wails:run` to
build and launch the desktop app. Manual `go run ./cmd/xrayviewd` is mainly
useful for direct transport inspection and backend-focused debugging.

## Transport

Default base URL:

- `http://127.0.0.1:38181`

Routes:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

Current command behavior:

- `get_processing_manifest` returns the frozen processing manifest payload
- `open_study` validates metadata, returns a Go-generated study record, and updates the recent-study catalog
- `start_render_job` runs the Go render pipeline
- `start_process_job` runs the Go processing pipeline and writes Secondary Capture output
- `start_analyze_job` runs the Go analysis pipeline
- `get_job` and `cancel_job` operate on Go-owned render, process, and analyze jobs
- `measure_line_annotation` recomputes pixel and calibrated lengths using registered study spacing metadata

Transport guarantees:

- loopback-only bind addresses
- local desktop/dev origin handling
- runtime metadata that identifies the transport as `local-http-json`

## Environment

- `XRAYVIEW_GO_BACKEND_HOST`
- `XRAYVIEW_GO_BACKEND_PORT`
- `XRAYVIEW_GO_BACKEND_LOG_LEVEL`
- `XRAYVIEW_GO_BACKEND_BASE_DIR`
- `XRAYVIEW_GO_BACKEND_CACHE_DIR`
- `XRAYVIEW_GO_BACKEND_PERSISTENCE_DIR`
- `XRAYVIEW_GO_BACKEND_SHUTDOWN_TIMEOUT`

Default disk layout when only `XRAYVIEW_GO_BACKEND_BASE_DIR` is set or when no
path overrides are provided:

- `<temp>/xrayview/cache`
- `<temp>/xrayview/cache/artifacts/<namespace>/<key>.<extension>`
- `<temp>/xrayview/state/<name>`
