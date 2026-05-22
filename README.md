<h1 align="center">xrayview</h1>

<p align="center">
  A DICOM X-ray visualization workstation<br>
  built with a <strong>Tauri</strong> desktop shell, an <strong>HTMX/TypeScript</strong> frontend, and a <strong>Rust</strong> backend (in-process).
</p>

> [!CAUTION]
> This tool is for **image visualization only**.
> It is not a medical device and must not be used for medical diagnosis,
> clinical decisions, or treatment planning.

---

## Features

- Open local DICOM studies (`.dcm`, `.dicom`) plus BMP/TIFF source images
- Render PNG previews for the workstation viewer
- Apply grayscale processing, palettes, and side-by-side comparison
- Export processed results as DICOM Secondary Capture files
- Run background render and process jobs with cancellation
- Measure line annotations with calibration-aware distances when pixel spacing metadata is available
- Persist a recent-studies catalog

> The user-facing workflow is **DICOM in, DICOM out**. PNG previews are an
> internal display artifact for the desktop UI.

---

## Repository Layout

```
xrayview/
├── frontend/        HTMX/TypeScript workstation UI (Vite)
├── desktop-tauri/   Tauri 2 desktop shell (Rust crate; links backend-rs as a library)
├── backend-rs/      Rust backend library + standalone HTTP/CLI binary
├── contracts/       shared JSON schema + generated TypeScript bindings
└── images/          sample image assets for dev & detector tuning
```

---

## Getting Started

### Prerequisites

- [Rust](https://www.rust-lang.org/tools/install) 1.77+
- [Node.js](https://nodejs.org/) 18.18+ or 20+
- Linux desktop builds require GTK/WebKit development packages
  On Debian/Ubuntu: `libgtk-3-dev`, `libwebkit2gtk-4.1-dev`, `librsvg2-dev`
- Windows desktop builds use WebView2 (auto-installed by the Tauri bundler)

### Install & verify

```bash
npm install
npm run contracts:check
npm run backend:test
```

### Browser mock mode

Run the HTMX UI with synthetic data - no backend needed:

```bash
npm run dev
```

### Desktop app

Build and launch the Tauri shell with the in-process Rust backend:

```bash
npm run tauri:dev               # dev launch
npm run tauri:build             # release binary + installer bundles
npm run tauri:build -- --no-bundle   # release binary only
```

<details>
<summary>Build outputs</summary>

| Artifact | Path |
|---|---|
| Frontend assets | `frontend/dist/` (bundled into the Tauri binary) |
| Desktop binary | `desktop-tauri/target/release/xrayview` |
| Installer bundles | `desktop-tauri/target/release/bundle/<format>/...` |

</details>

### Release smoke test

```bash
npm run release:smoke
```

Checks contract drift, runs backend tests, builds the frontend, then runs
`tauri build --no-bundle`. Pass `release:smoke:bundle` to include installer
bundles.

---

## Runtime Modes

| Mode | Default in | Description |
|---|---|---|
| `mock` | Browser / Vite | Synthetic data, no backend |
| `desktop` | Tauri shell | Live Rust backend in-process via Tauri IPC |

The runtime is normally auto-detected (`window.__TAURI_INTERNALS__` is injected
by the WebView). To override:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
```

---

## Rust Backend

`backend-rs/` is a library used in-process by the desktop shell and also ships
a standalone HTTP binary (`xrayview-backend-rs`) for CLI/tests. The standalone
binary binds `127.0.0.1:38181` by default and is loopback-only.

```bash
npm run backend:build
npm run backend:serve
npm run backend:test
```

### Command surface (Tauri IPC + standalone HTTP)

| Command | Purpose |
|---|---|
| `get_processing_manifest` | Available processing presets |
| `open_study` | Open a DICOM / BMP / TIFF study |
| `start_render_job` | Render a preview |
| `start_process_job` | Run processing pipeline |
| `start_analyze_job` | Generate deterministic analysis overlays |
| `get_job` | Poll job state |
| `get_jobs` | List job state |
| `cancel_job` | Cancel a running job |
| `measure_line_annotation` | Calibration-aware line measurement |

In the desktop shell each command is reached via `invoke("<command>", { command: <payload> })`
from the frontend; the standalone HTTP binary exposes them under
`POST /api/v1/commands/{command}`.

---

## CLI

The headless CLI runs through the standalone backend binary.

### Utility subcommands

```bash
# Info
npm run backend:cli -- print-config      # resolved config as JSON
npm run backend:cli -- version           # service + contract version
npm run backend:cli -- list-commands     # supported backend commands

# DICOM inspection
npm run backend:cli -- inspect-decode /path/to/study.dcm
npm run backend:cli -- decode-source  /path/to/study.dcm

# Render & process
npm run backend:cli -- render-preview /path/to/study.dcm /tmp/preview.png
npm run backend:cli -- render-preview --full-range /path/to/study.dcm /tmp/preview.png
npm run backend:cli -- process-preview --invert --equalize /path/to/study.dcm /tmp/processed.png

# Export
npm run backend:cli -- export-secondary-capture --palette hot /path/to/study.dcm /tmp/export.dcm
```

<details>
<summary>Legacy workflow flags</summary>

```bash
npm run backend:cli -- --describe-presets
npm run backend:cli -- --input /path/to/study.dcm --describe-study
npm run backend:cli -- --input /path/to/study.dcm --preview-output /tmp/preview.png
```

</details>

> See `images/README.md` for available sample assets and provenance.

---

## Contracts

The single source of truth is `contracts/backend-contract-v1.schema.json`.

```bash
npm run contracts:generate    # regenerate TS bindings
npm run contracts:check       # verify bindings are up to date
```

Generated file (do not edit manually):

- `frontend/src/lib/generated/contracts.ts`

Rust types in `backend-rs/src/contracts.rs` are the matching source on the Rust
side (manually kept in sync; not generated).

---

## Architecture

The project is a monorepo with a Rust backend library and a Tauri 2 desktop
shell that links it in-process.

| Module | Responsibility |
|---|---|
| `frontend/` | Workstation UI and mock-mode behavior |
| `desktop-tauri/` | Tauri shell: window lifecycle, file dialogs, IPC command wrappers, job-event forwarding |
| `backend-rs/` | Rust library: DICOM decode, render, processing, annotations, jobs. Also ships a standalone HTTP/CLI binary |
| `contracts/` | Shared command payload shapes via JSON schema |

```
┌─────────────┐    Tauri IPC    ┌──────────────────────────────────────┐
│  HTMX UI    │ ◄─────────────► │ desktop-tauri (Rust shell)           │
│  (frontend) │                  │   ↳ backend-rs::App (in-process)     │
└─────────────┘                  └──────────────────────────────────────┘
        ▲                                            ▲
        └───────────────── contracts ────────────────┘
```
