# xrayview

`xrayview` is a DICOM X-ray visualization and analysis workstation built with Tauri (React/TypeScript frontend, Rust backend). The repository now also includes a phase 24 Go backend sidecar path with shell-managed startup/shutdown, a live processing-manifest endpoint, Go-backed `open_study` registration, a temporary Rust decode helper owned by Go, live Go render/process job paths, and Go-owned recent-study persistence.

The desktop UI lives in `frontend/`. The Rust backend in `backend/` powers all DICOM decoding, image processing, rendering, measurement, and export. The backend is library-first — Tauri calls Rust directly in-process (no subprocess). The CLI binary remains available for headless DICOM workflows.

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
go -C go-backend run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
```

The Go backend sidecar binds to `127.0.0.1:38181` by default and exposes:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

The transport is intentionally local-only:

- the backend binds only to loopback hosts
- the frontend accepts only loopback `http://` sidecar URLs
- the server handles the CORS/preflight flow required by the Tauri webview

Current Go command behavior:

- `get_processing_manifest` returns the frozen processing manifest payload
- `open_study` validates and registers studies in Go and updates the Go-owned recent-study catalog
- `start_render_job`, `get_job`, and `cancel_job` are live for Go-owned preview rendering
- `inspect-decode` reports decode-relevant DICOM metadata for migration planning
- `render-preview` exercises the phase 16 decode-to-preview pipeline from the Go CLI
- the remaining Go command routes still return structured placeholder errors until later phases move real behavior into Go

See [GO_BACKEND_PHASE7_DEFINE_LOCAL_BACKEND_TRANSPORT.md](GO_BACKEND_PHASE7_DEFINE_LOCAL_BACKEND_TRANSPORT.md), [GO_BACKEND_PHASE8_ADD_TAURI_GO_PROCESS_MANAGEMENT.md](GO_BACKEND_PHASE8_ADD_TAURI_GO_PROCESS_MANAGEMENT.md), [GO_BACKEND_PHASE9_IMPLEMENT_GO_PROCESSING_MANIFEST_ENDPOINT.md](GO_BACKEND_PHASE9_IMPLEMENT_GO_PROCESSING_MANIFEST_ENDPOINT.md), [GO_BACKEND_PHASE10_IMPLEMENT_GO_STUDY_REGISTRY_AND_OPEN_STUDY.md](GO_BACKEND_PHASE10_IMPLEMENT_GO_STUDY_REGISTRY_AND_OPEN_STUDY.md), [GO_BACKEND_PHASE11_PROTOTYPE_GO_DICOM_METADATA_READER.md](GO_BACKEND_PHASE11_PROTOTYPE_GO_DICOM_METADATA_READER.md), [GO_BACKEND_PHASE12_DECIDE_DICOM_DECODE_STRATEGY.md](GO_BACKEND_PHASE12_DECIDE_DICOM_DECODE_STRATEGY.md), [GO_BACKEND_PHASE13_BUILD_TEMPORARY_RUST_DECODE_HELPER.md](GO_BACKEND_PHASE13_BUILD_TEMPORARY_RUST_DECODE_HELPER.md), [GO_BACKEND_PHASE14_IMPLEMENT_GO_PREVIEW_IMAGE_MODEL.md](GO_BACKEND_PHASE14_IMPLEMENT_GO_PREVIEW_IMAGE_MODEL.md), [GO_BACKEND_PHASE15_PORT_WINDOWING_LOGIC_TO_GO.md](GO_BACKEND_PHASE15_PORT_WINDOWING_LOGIC_TO_GO.md), [GO_BACKEND_PHASE16_PORT_BASE_RENDER_PIPELINE_TO_GO.md](GO_BACKEND_PHASE16_PORT_BASE_RENDER_PIPELINE_TO_GO.md), and [GO_BACKEND_PHASE17_CUT_RENDER_STUDY_TO_GO.md](GO_BACKEND_PHASE17_CUT_RENDER_STUDY_TO_GO.md).

### Desktop app

```bash
# Install dependencies (root postinstall handles frontend/)
npm install

# Run the desktop app with hot-reload
npm run tauri:dev

# Build desktop bundles (Linux needs WebKitGTK, patchelf)
npm run tauri:build
```

If you do not choose a save destination, the app keeps the processed DICOM in a
managed temporary path and shows that path after processing completes.

### Release validation

```bash
# Smoke test without generating installers
npm run release:smoke

# With installer/AppImage verification
npm run release:smoke -- --bundle
```

### Browser-only mock mode

Iterate on the UI without the Rust backend:

```bash
npm run dev
```

### Backend Runtime Selection

The frontend now supports three backend runtime modes:

- `mock`
- `legacy-rust`
- `go-sidecar`

Defaults:

- browser/Vite only: `mock`
- Tauri desktop: `legacy-rust`

You can override the backend runtime with:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
XRAYVIEW_BACKEND_RUNTIME=legacy-rust npm run tauri:dev
XRAYVIEW_BACKEND_RUNTIME=go-sidecar XRAYVIEW_GO_BACKEND_URL=http://127.0.0.1:38181 npm run tauri:dev
```

`XRAYVIEW_GO_BACKEND_URL` is only used for the `go-sidecar` runtime and must be a loopback `http://` URL such as `http://127.0.0.1:38181`. The frontend entry scripts also accept the Vite-prefixed forms `VITE_XRAYVIEW_BACKEND_RUNTIME` and `VITE_XRAYVIEW_GO_BACKEND_URL`.
The `go-sidecar` adapter is part of the migration path; the Tauri shell now starts and stops that local backend automatically, `get_processing_manifest`, `open_study`, `start_render_job`, `start_process_job`, `get_job`, and `cancel_job` are live in Go, process previews now render fully through the Go-owned pipeline, DICOM export is still deferred to later migration phases, and it is not the default packaged runtime yet.
Phase 12 explicitly keeps full pixel decode off the Go side for now and routes the next migration step through a narrow Rust helper until a broader study corpus proves pure-Go decode is justified.

## Releases

Prebuilt desktop packages are published on GitHub Releases.

- Linux: download the `.AppImage`, run `chmod +x <asset>.AppImage`, then run it
- Windows: download the `.msi` installer and run it

The desktop packages still default to the embedded Rust backend path. When the
app is built for `go-sidecar`, the shell bundles and launches the Go backend
alongside the desktop app.

## Basic Usage

The repository includes a public dental radiograph sample at `images/sample-dental-radiograph.dcm`. This DICOM file is derived from the Wikimedia Commons panoramic image `Dental Panorama X-ray.jpg` (CC BY 4.0). See `images/README.md` for provenance details.

```bash
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm
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
- `--analyze-tooth` — Runs auto-tooth detection and outputs results as JSON (requires `--preview-output`)

## CLI Examples

```bash
# Basic processing (output auto-named input_processed.dcm)
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm

# Explicit output path
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --output images/output.dcm

# Tone adjustments
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --invert --brightness 15
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --contrast 1.6 --equalize

# Preset with override
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --preset xray --brightness 5

# Pseudocolor
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --palette hot

# Side-by-side comparison
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --preset xray --compare

# Describe study metadata
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --describe-study

# Auto-tooth analysis
cargo run --manifest-path backend/Cargo.toml -- --input images/sample-dental-radiograph.dcm --analyze-tooth --preview-output /tmp/preview.png
```

## Validation Rules

- `--input` is required and must be a `.dcm` or `.dicom` file
- `--output` (if provided) must end with `.dcm` or `.dicom`
- `--palette` must be `none`, `hot`, or `bone`
- `--preset` must be `default`, `xray`, or `high-contrast`

## Architecture

### Transitional Workspace Layout

- **`backend/`** — Library-first crate with modular layout: `api/`, `app/`, `study/`, `render/`, `processing/`, `analysis/`, `annotations/`, `export/`, `jobs/`, `cache/`, `persistence/`. Also provides a thin CLI binary.
- **`frontend/src-tauri/`** — Tauri desktop shell. Depends on the backend as a library (direct in-process calls, no subprocess).
- **`go-backend/`** — Phase 6 Go backend module with the initial sidecar process skeleton.
- **`go/contracts/`** — Go module for generated contract bindings owned by the language-neutral schema.

### Contract generation

The contract source of truth now lives in [contracts/backend-contract-v1.schema.json](contracts/backend-contract-v1.schema.json). Running `npm --prefix frontend run generate:contracts` regenerates:

- [frontend/src/lib/generated/contracts.ts](frontend/src/lib/generated/contracts.ts)
- [go/contracts/contractv1/bindings.go](go/contracts/contractv1/bindings.go)

Phase 2 froze this surface as backend contract v1. Phase 3 moves generation out of Rust: `cargo test --manifest-path backend/Cargo.toml --test contracts` now checks that committed generated bindings match the schema and that representative Rust payloads still validate against that schema.

### Data flow

1. User opens a DICOM file -> `open_study` Tauri command -> backend registers study, decodes DICOM, returns metadata + study ID
2. Render/process/analyze requests reference study by ID -> dispatched as async jobs -> progress emitted via Tauri events -> results cached as artifacts
3. Frontend loads rendered PNG previews via Tauri's asset protocol
4. Viewer renders on Canvas 2D with annotation overlay in image-space coordinates

## Test

```bash
# Backend unit + integration tests
cargo test --manifest-path backend/Cargo.toml

# Go backend skeleton tests
npm run go:backend:test

# Frontend type-check
npm --prefix frontend run build
```
