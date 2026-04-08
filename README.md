# xrayview

`xrayview` is a DICOM X-ray visualization and analysis workstation built with a Wails desktop shell, a React/TypeScript frontend, a Go-first desktop backend, and a narrow Rust compatibility layer that still exists only for helper functionality the migration has not retired yet.

The desktop UI lives in `frontend/`, and the supported desktop shell now lives in `wails-prototype/`. The Go sidecar in `go-backend/` owns the desktop command surface and the supported CLI under `go-backend/cmd/xrayview-cli`. The Rust backend in `backend/` is no longer part of the live desktop runtime; it remains only where the migration still depends on temporary helper behavior.

## What It Does

### Desktop Workstation

- **View tab** — Canvas 2D viewer with pan/zoom, annotation overlay, and measurement tools
  - Manual line measurements: draw calibrated lines on the image
  - Auto-tooth detection: one-click tooth identification with confidence scores, bounding boxes, and width/height measurements
  - Calibration-aware measurements in physical units (mm) when DICOM pixel spacing is available
  - Editable annotations: adjust line endpoints, delete, and manage annotation lists
- **Processing tab** — Full image processing controls
  - Preset selector (default, xray, high-contrast)
  - Grayscale controls: invert, brightness, contrast, histogram equalization
  - Pseudocolor palettes: none, hot, bone
  - Compare mode: side-by-side original vs processed output
  - Save destination via native file picker, or managed temp path
- **Job Center** — Background job queue with progress bars, cancellation, and cache hit indicators
- **Study persistence** — Recent studies catalog (last 10 opened)

### DICOM Pipeline

- Loads DICOM studies from disk (`.dcm`, `.dicom`)
- Extracts metadata and pixel spacing calibration from multiple DICOM tags
- Renders previews with DICOM window-level/center support
- Applies composable grayscale filter pipeline (invert, brightness, contrast, equalize)
- Applies pseudocolor palettes (hot, bone)
- Builds side-by-side comparison images
- Exports results as derived DICOM Secondary Capture files
- Caches rendered artifacts (memory + disk) with job deduplication

The desktop frontend renders internal PNG previews for display, but the user-facing workflow is DICOM in and DICOM out.

## Important Notice

This tool is for image visualization only.

It is **not** a medical device and must **not** be used for medical diagnosis, clinical decisions, or treatment planning.

## Build & Development

### Backend only

```bash
cd backend
cargo build --release
```

### Go backend transport

```bash
npm run go:backend:test
npm run go:backend:build
npm run go:backend:serve
go -C go-backend run ./cmd/xrayview-cli -- --describe-presets
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --describe-study
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preview-output /tmp/xrayview-preview.png
go -C go-backend run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
```

The Go backend sidecar binds to `127.0.0.1:38181` by default and exposes:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

The transport is intentionally local-only:

- the backend binds only to loopback hosts
- the Wails shell talks to the Go backend over loopback `http://`
- browser/mock mode does not expose the live backend transport

Current Go command behavior:

- `get_processing_manifest` returns the frozen processing manifest payload
- `open_study` validates and registers studies in Go and updates the Go-owned recent-study catalog
- `start_render_job` renders previews through the Go job service
- `start_process_job` processes previews in Go, writes the processed DICOM through the configured export writer, and returns the resolved output path
- `start_analyze_job` runs the Go analysis pipeline and returns suggested annotations
- `get_job` and `cancel_job` work for Go-owned render/process/analyze jobs
- `measure_line_annotation` recomputes pixel and calibrated lengths in Go
- the supported headless CLI workflows now use `go -C go-backend run ./cmd/xrayview-cli -- ...` with the existing Rust-compatible flag surface
- `inspect-decode` reports decode-relevant DICOM metadata for migration planning
- `render-preview` exercises the phase 16 decode-to-preview pipeline from the Go CLI
- the sidecar still depends on the narrow Rust decode helper until a future phase proves pure-Go decode is warranted

See [GO_BACKEND_PHASE7_DEFINE_LOCAL_BACKEND_TRANSPORT.md](GO_BACKEND_PHASE7_DEFINE_LOCAL_BACKEND_TRANSPORT.md), [GO_BACKEND_PHASE8_ADD_TAURI_GO_PROCESS_MANAGEMENT.md](GO_BACKEND_PHASE8_ADD_TAURI_GO_PROCESS_MANAGEMENT.md), [GO_BACKEND_PHASE9_IMPLEMENT_GO_PROCESSING_MANIFEST_ENDPOINT.md](GO_BACKEND_PHASE9_IMPLEMENT_GO_PROCESSING_MANIFEST_ENDPOINT.md), [GO_BACKEND_PHASE10_IMPLEMENT_GO_STUDY_REGISTRY_AND_OPEN_STUDY.md](GO_BACKEND_PHASE10_IMPLEMENT_GO_STUDY_REGISTRY_AND_OPEN_STUDY.md), [GO_BACKEND_PHASE11_PROTOTYPE_GO_DICOM_METADATA_READER.md](GO_BACKEND_PHASE11_PROTOTYPE_GO_DICOM_METADATA_READER.md), [GO_BACKEND_PHASE12_DECIDE_DICOM_DECODE_STRATEGY.md](GO_BACKEND_PHASE12_DECIDE_DICOM_DECODE_STRATEGY.md), [GO_BACKEND_PHASE13_BUILD_TEMPORARY_RUST_DECODE_HELPER.md](GO_BACKEND_PHASE13_BUILD_TEMPORARY_RUST_DECODE_HELPER.md), [GO_BACKEND_PHASE14_IMPLEMENT_GO_PREVIEW_IMAGE_MODEL.md](GO_BACKEND_PHASE14_IMPLEMENT_GO_PREVIEW_IMAGE_MODEL.md), [GO_BACKEND_PHASE15_PORT_WINDOWING_LOGIC_TO_GO.md](GO_BACKEND_PHASE15_PORT_WINDOWING_LOGIC_TO_GO.md), [GO_BACKEND_PHASE16_PORT_BASE_RENDER_PIPELINE_TO_GO.md](GO_BACKEND_PHASE16_PORT_BASE_RENDER_PIPELINE_TO_GO.md), [GO_BACKEND_PHASE17_CUT_RENDER_STUDY_TO_GO.md](GO_BACKEND_PHASE17_CUT_RENDER_STUDY_TO_GO.md), [GO_BACKEND_PHASE28_IMPLEMENT_GO_ANALYZE_JOB.md](GO_BACKEND_PHASE28_IMPLEMENT_GO_ANALYZE_JOB.md), [GO_BACKEND_PHASE30_ADD_TEMPORARY_RUST_EXPORT_HELPER_IF_NEEDED.md](GO_BACKEND_PHASE30_ADD_TEMPORARY_RUST_EXPORT_HELPER_IF_NEEDED.md), [GO_BACKEND_PHASE31_CUT_PROCESS_STUDY_FULLY_TO_GO.md](GO_BACKEND_PHASE31_CUT_PROCESS_STUDY_FULLY_TO_GO.md), [GO_BACKEND_PHASE32_CUT_MEASURE_LINE_ANNOTATION_TO_GO.md](GO_BACKEND_PHASE32_CUT_MEASURE_LINE_ANNOTATION_TO_GO.md), [GO_BACKEND_PHASE33_CUT_OPEN_STUDY_TO_GO_IN_LIVE_DESKTOP_FLOW.md](GO_BACKEND_PHASE33_CUT_OPEN_STUDY_TO_GO_IN_LIVE_DESKTOP_FLOW.md), [GO_BACKEND_PHASE34_CUT_ANALYZE_STUDY_TO_GO_IN_LIVE_DESKTOP_FLOW.md](GO_BACKEND_PHASE34_CUT_ANALYZE_STUDY_TO_GO_IN_LIVE_DESKTOP_FLOW.md), [GO_BACKEND_PHASE35_MOVE_CLI_OWNERSHIP_TO_GO.md](GO_BACKEND_PHASE35_MOVE_CLI_OWNERSHIP_TO_GO.md), [GO_BACKEND_PHASE37_INTRODUCE_GO_FIRST_PACKAGING_FLOW_UNDER_TAURI.md](GO_BACKEND_PHASE37_INTRODUCE_GO_FIRST_PACKAGING_FLOW_UNDER_TAURI.md), and [GO_BACKEND_PHASE40_REPLACE_TAURI_SHELL_WITH_WAILS.md](GO_BACKEND_PHASE40_REPLACE_TAURI_SHELL_WITH_WAILS.md).

### Desktop app

```bash
# Install dependencies (root postinstall handles frontend/)
npm install

# Build and launch the Wails desktop shell
npm run wails:run

# Build the desktop binary plus sidecar
npm run wails:build
```

If you do not choose a save destination, the app keeps the processed DICOM in a
managed temporary path and shows that path after processing completes.

The Wails build writes frontend assets under `wails-prototype/build/frontend/dist/`
and writes the desktop shell plus Go sidecar binaries under
`wails-prototype/build/bin/`.

### Release validation

```bash
# Smoke test the Wails desktop binary
npm run release:smoke

# The flag is accepted, but current validation still targets the built binary
npm run release:smoke -- --bundle
```

The release smoke flow now checks contract drift, runs the Go backend test
suite, builds the Wails desktop binary, and launches it long enough to confirm
the Go sidecar comes up when the current environment can host GUI launch smoke.

### Browser-only mock mode

Iterate on the UI without the live desktop shell:

```bash
npm run dev
```

### Backend Runtime Selection

The frontend now supports two runtime modes:

- `mock`
- `desktop`

Defaults:

- browser/Vite only: `mock`
- Wails desktop: `desktop`

You can override the backend runtime with:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
npm run wails:run
XRAYVIEW_BACKEND_RUNTIME=mock npm run wails:run
XRAYVIEW_BACKEND_RUNTIME=desktop XRAYVIEW_GO_BACKEND_URL=http://127.0.0.1:38181 npm run wails:run
```

`XRAYVIEW_GO_BACKEND_URL` configures the local Go sidecar for desktop runs and must be a loopback `http://` URL such as `http://127.0.0.1:38181`. The Wails run/build scripts also accept the Vite-prefixed forms `VITE_XRAYVIEW_BACKEND_RUNTIME` and `VITE_XRAYVIEW_GO_BACKEND_URL`.
The Wails shell starts and stops the local Go backend automatically for `desktop` runs. That means `openStudy`, `renderStudy`, `processStudy`, `analyzeStudy`, `measureLineAnnotation`, and the normal job polling/cancellation path now run through the bundled Go backend by default. The legacy `go-sidecar` value is still accepted as an alias for existing automation, but the frontend now treats that as the internal desktop path instead of a user-facing runtime name.
Phase 12 still keeps full pixel decode off the Go side for now and routes that narrow responsibility through a temporary Rust helper until a broader study corpus proves pure-Go decode is justified.

## Releases

The supported repository release flow currently validates the built Wails
desktop binary under `wails-prototype/build/bin/`. Installer/AppImage bundling
has not been reintroduced yet in the Wails shell path.

## Basic Usage

The repository includes a public dental radiograph sample at `images/sample-dental-radiograph.dcm`. This DICOM file is derived from the Wikimedia Commons panoramic image `Dental Panorama X-ray.jpg` (CC BY 4.0). See `images/README.md` for provenance details.

```bash
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm
```

If `--output` is omitted, the tool writes a file next to the input using this pattern:

- `study.dcm` -> `study_processed.dcm`

## CLI Flags

### Input / Output

- `--input` — Path to the source DICOM study (`.dcm` or `.dicom`, required)
- `--output` — Output DICOM path (optional; defaults to `input_processed.dcm`)
- `--preview-output` — PNG preview output path (optional)

### Presets

- `--preset` — Named visualization preset (`default`, `xray`, `high-contrast`)
  - Explicit CLI flags override preset values

| Preset | Brightness | Contrast | Equalize | Palette |
|---|---|---|---|---|
| `default` | 0 | 1.0 | false | none |
| `xray` | 10 | 1.4 | true | bone |
| `high-contrast` | 0 | 1.8 | true | none |

### Grayscale Filter Controls

- `--invert` — Inverts the grayscale image
- `--brightness` — Integer brightness delta (positive brightens, negative darkens)
- `--contrast` — Floating-point contrast factor (`1.0` = unchanged, `>1.0` = more contrast)
- `--equalize` — Enables histogram equalization
- Grayscale processing always runs in this fixed order: `invert`, `brightness`, `contrast`, `equalize`

### Comparison Output

- `--compare` — Writes a side-by-side comparison (original left, processed right) into the derived DICOM output

### Pseudocolor

- `--palette` — Pseudocolor palette (`none`, `hot`, `bone`)

### Study Inspection & Analysis

- `--describe-presets` — Outputs a JSON manifest of all processing presets
- `--describe-study` — Outputs JSON study metadata (dimensions, measurement scale, calibration info)
- `--analyze-tooth` — Runs auto-tooth detection and outputs results as JSON (optionally writes a preview PNG via `--preview-output`)

## CLI Examples

```bash
# Basic processing (output auto-named input_processed.dcm)
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm

# Explicit output path
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --output /tmp/output.dcm

# Tone adjustments
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --invert --brightness 15
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --contrast 1.6 --equalize

# Preset with override
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preset xray --brightness 5

# Pseudocolor
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --palette hot

# Side-by-side comparison
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preset xray --compare

# Describe study metadata
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --describe-study

# Auto-tooth analysis
go -C go-backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --analyze-tooth --preview-output /tmp/preview.png
```

## Validation Rules

- `--input` is required and must be a `.dcm` or `.dicom` file
- `--output` (if provided) must end with `.dcm` or `.dicom`
- `--palette` must be `none`, `hot`, or `bone`
- `--preset` must be `default`, `xray`, or `high-contrast`

## Architecture

### Transitional Workspace Layout

- **`backend/`** — Library-first crate with modular layout: `api/`, `app/`, `study/`, `render/`, `processing/`, `analysis/`, `annotations/`, `export/`, `jobs/`, `cache/`, `persistence/`. Still provides helper binaries for the migration path, but the supported headless CLI workflows now live in `go-backend/cmd/xrayview-cli`.
- **`wails-prototype/`** — Wails desktop shell. Hosts the live desktop window, native dialogs, preview-path serving, and the shell-to-sidecar command bridge.
- **`go-backend/`** — Phase 6 Go backend module with the initial sidecar process skeleton.
- **`go/contracts/`** — Go module for generated contract bindings owned by the language-neutral schema.

### Contract generation

The contract source of truth now lives in [contracts/backend-contract-v1.schema.json](contracts/backend-contract-v1.schema.json). Running `npm run contracts:generate` regenerates:

- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- [go/contracts/contractv1/bindings.go](go/contracts/contractv1/bindings.go)

Run `npm run contracts:check` to verify the committed generated bindings still match the schema without routing through the legacy Rust backend. Rust-side contract tests remain available as backend compatibility coverage, but they are no longer part of the frontend-owned contract generation path.

### Data flow

1. User opens a DICOM file -> frontend asks the Wails shell for a native file dialog -> shell forwards `open_study` to the Go sidecar -> backend registers study, decodes DICOM metadata, and returns metadata plus study ID
2. Render/process/analyze requests reference study by ID -> frontend sends command requests through the Wails shell -> Go sidecar executes jobs and returns typed job snapshots/results
3. Frontend loads rendered PNG previews through the Wails `/preview` asset route
4. Viewer renders on Canvas 2D with annotation overlay in image-space coordinates

## Test

```bash
# Shared contract binding drift check
npm run contracts:check

# Backend unit + integration tests
cargo test --manifest-path backend/Cargo.toml

# Go backend skeleton tests
npm run go:backend:test

# Wails shell tests
GOCACHE=/tmp/xrayview-go-build-cache GOTMPDIR=/tmp/xrayview-go-tmp go -C wails-prototype test ./...

# Frontend type-check + browser build
npm --prefix frontend run build

# Desktop shell build
npm run wails:build
```
