# xrayview Go Backend

This module now owns the supported `xrayview` backend runtime. The Wails shell
starts `cmd/xrayviewd` for live desktop mode, and `cmd/xrayview-cli` exposes the
supported headless workflow surface.

Current scope:

- load config from environment
- initialize cache and persistence roots under the shared disk layout
- expose a local loopback HTTP/JSON server
- return the frozen processing manifest for `get_processing_manifest`
- validate DICOM metadata and register studies for `open_study`
- decode supported source studies directly in Go into the shared `internal/imaging` model
- render grayscale previews in Go with embedded/manual/full-range window handling
- apply grayscale processing, palettes, and compare output in Go
- encode preview PNGs and Secondary Capture DICOM output in Go
- execute render, process, and analyze jobs in Go with dedupe, cancellation, and cache hits
- recompute manual line measurements in Go
- generate suggested annotations from the Go tooth-analysis pipeline
- own recent-study catalog persistence
- expose the supported CLI surface that replaced the removed Rust CLI
- publish health/runtime metadata for the desktop shell
- enforce local-only host/origin rules for the transport

Current non-goals:

- no claim that the committed sample corpus proves broad compressed-transfer-syntax parity
- deflated transfer syntax is still rejected by the Go decoder
- encapsulated multi-frame source decode is still unsupported

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

The supported headless DICOM workflow uses the top-level flag interface shown
above. The older subcommands remain available for focused decode, render, and
transport inspection work.

The supported desktop shell now lives in `../wails-prototype`. Use
`npm run wails:build` to build the shell plus this backend, or `npm run
wails:run` to build and launch the desktop app. Manual `go run ./cmd/xrayviewd`
is mainly useful for direct transport inspection and backend-focused debugging.

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
- `start_process_job` runs the Go preview-processing pipeline, writes the processed DICOM through the Go Secondary Capture writer, and returns the completed output path
- `start_analyze_job` runs the Go analysis pipeline and returns suggested annotations through the normal job/result path
- `get_job` and `cancel_job` now work for Go-owned render, process, and analyze jobs
- `measure_line_annotation` recomputes pixel and calibrated lengths in Go using the registered study spacing metadata

Current metadata-reader limits:

- deflated transfer syntax is still rejected in the Go reader
- encapsulated multi-frame source decode is still unsupported
- the committed sample corpus contains only native single-frame monochrome explicit-VR-little-endian studies
- the supported desktop and CLI workflows now decode directly in Go
- broader compressed-transfer-syntax parity still needs evidence beyond the committed sample corpus

Transport guarantees:

- loopback-only backend bind addresses
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
