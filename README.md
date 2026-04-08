# xrayview

`xrayview` is a DICOM X-ray visualization and analysis workstation built with a
Wails desktop shell, a React/TypeScript frontend, and a Go backend.

## Important Notice

This tool is for image visualization only.

It is not a medical device and must not be used for medical diagnosis,
clinical decisions, or treatment planning.

## Repository Layout

- `frontend/` - React workstation UI and frontend build/test scripts
- `desktop/` - supported Wails desktop shell
- `go-backend/` - supported Go backend service and CLI
- `contracts/` - language-neutral backend contract schema
- `go/contracts/` - generated Go contract bindings
- `images/` - sample DICOM assets used for development and smoke validation

## What The App Does

- Open local DICOM studies (`.dcm`, `.dicom`)
- Render PNG previews for the workstation viewer
- Apply grayscale processing, palettes, and compare output
- Export processed results as DICOM Secondary Capture files
- Run background render, process, and analyze jobs with cancellation
- Measure line annotations with calibration-aware distances when spacing metadata is available
- Suggest tooth annotations from the Go analysis pipeline
- Persist a recent-studies catalog

The user-facing workflow is DICOM in and DICOM out. PNG previews are an
internal display artifact for the desktop UI.

## Developer Onboarding

Install dependencies and verify the core repo paths:

```bash
npm install
npm run contracts:check
npm run go:backend:test
go -C desktop test ./...
```

### Browser Mock Mode

Use the React app without the live desktop shell:

```bash
npm run dev
```

Browser/Vite runs default to `mock` mode.

### Desktop App

Build and launch the supported Wails shell:

```bash
npm run wails:run
```

Build release-style binaries without launching:

```bash
npm run wails:build
```

Build outputs:

- frontend assets: `desktop/build/frontend/dist/`
- desktop shell binary: `desktop/build/bin/xrayview`
- bundled Go backend sidecar: `desktop/build/bin/xrayview-go-backend`

### Release Smoke

Validate the supported release path end to end:

```bash
npm run release:smoke
```

That flow checks contract drift, runs the Go backend test suite, builds the
frontend and Wails shell, and launches the desktop binary long enough to confirm
the bundled backend sidecar comes up when the environment supports GUI launch
smoke.

## Runtime Modes

Supported runtime modes:

- `mock`
- `desktop`

Defaults:

- browser/Vite: `mock`
- Wails desktop shell: `desktop`

Overrides:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
XRAYVIEW_BACKEND_RUNTIME=mock npm run wails:run
XRAYVIEW_BACKEND_RUNTIME=desktop XRAYVIEW_GO_BACKEND_URL=http://127.0.0.1:38181 npm run wails:run
```

`XRAYVIEW_GO_BACKEND_URL` must be an absolute loopback `http://` URL such as
`http://127.0.0.1:38181`.

## Go Backend

The Go backend sidecar binds to `127.0.0.1:38181` by default and exposes:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

The transport is intentionally local-only:

- the backend binds only to loopback hosts
- the Wails shell talks to the Go backend over loopback HTTP
- browser/mock mode does not expose the live backend transport

Current Go-owned command surface:

- `get_processing_manifest`
- `open_study`
- `start_render_job`
- `start_process_job`
- `start_analyze_job`
- `get_job`
- `cancel_job`
- `measure_line_annotation`

## CLI

The supported headless CLI lives at `go-backend/cmd/xrayview-cli`.

Examples:

```bash
go -C go-backend run ./cmd/xrayview-cli -- --describe-presets
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --describe-study
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preview-output /tmp/xrayview-preview.png
go -C go-backend run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
```

The repository includes a public dental radiograph sample at
`images/sample-dental-radiograph.dcm`. See `images/README.md` for provenance
details.

## Contracts

The contract source of truth lives in
`contracts/backend-contract-v1.schema.json`.

Generate bindings:

```bash
npm run contracts:generate
```

This regenerates:

- `frontend/src/lib/generated/contracts.ts`
- `go/contracts/contractv1/bindings.go`

## Architecture Notes

- `frontend/` owns the workstation UI and mock-mode behavior.
- `desktop/` owns native shell concerns: window lifecycle, dialogs, preview
  asset serving, and Go sidecar lifecycle management.
- `go-backend/` owns the supported backend runtime: decode, render, process,
  analyze, export, jobs, cache, and persistence.
- `contracts/` and `go/contracts/` keep the frontend and Go backend on the same
  command payload shapes.
