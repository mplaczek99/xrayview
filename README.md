# xrayview

`xrayview` is a DICOM X-ray visualization and analysis workstation built with Tauri (React/TypeScript frontend, Rust backend). The repository now also includes a phase 6 Go backend workspace that boots as a local HTTP sidecar skeleton for the ongoing migration.

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

### Go backend skeleton

```bash
npm run go:backend:test
npm run go:backend:build
npm run go:backend:serve
```

The phase 6 Go backend binds to `127.0.0.1:38181` by default and exposes:

- `GET /healthz`
- `GET /api/v1/runtime`
- `GET /api/v1/commands`
- `POST /api/v1/commands/{command}`

At this stage the command routes are transport placeholders only. They return structured backend errors for the phase 5 command names until later phases move real behavior into Go.

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

`XRAYVIEW_GO_BACKEND_URL` is only used for the `go-sidecar` runtime. The frontend entry scripts also accept the Vite-prefixed forms `VITE_XRAYVIEW_BACKEND_RUNTIME` and `VITE_XRAYVIEW_GO_BACKEND_URL`.
The `go-sidecar` adapter is part of the migration path; it expects a compatible local HTTP backend and is not the default packaged runtime yet.

## Releases

Prebuilt desktop packages are published on GitHub Releases.

- Linux: download the `.AppImage`, run `chmod +x <asset>.AppImage`, then run it
- Windows: download the `.msi` installer and run it

The desktop packages embed the Rust backend directly; there is no separate
backend sidecar to install or keep in sync.

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
