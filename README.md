<h1 align="center">xrayview</h1>

<p align="center">
  A BMP bitewing X-ray visualization workstation<br>
  built with a <strong>Tauri</strong> desktop shell, an <strong>HTMX/TypeScript</strong> frontend, and a <strong>Rust</strong> backend (in-process).
</p>

> [!CAUTION]
> This tool is for **image visualization only**.
> It is not a medical device and must not be used for medical diagnosis,
> clinical decisions, or treatment planning.

---

## Features

- Open local bitewing X-rays in BMP format
- Render BMP previews for the workstation viewer
- Apply grayscale processing, palettes, and side-by-side comparison
- Run background render and process jobs with cancellation
- Measure line annotations
- Persist a recent-studies catalog

> The user-facing workflow is **BMP in, BMP previews for display and processing**.

---

## Repository Layout

```
xrayview/
├── frontend/        HTMX/TypeScript workstation UI (Vite)
├── desktop-tauri/   Tauri 2 desktop shell (Rust crate; links backend-rs as a library)
├── backend-rs/      Rust backend library + headless CLI binary
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

`backend-rs/` is a library used in-process by the desktop shell. It also ships
a headless CLI binary (`xrayview-backend-rs`) for scripted/manual inspection
of BMP studies; the CLI calls the same library code directly — there is no
local HTTP server.

```bash
npm run backend:build
npm run backend:test
```

### Command surface (Tauri IPC)

| Command | Purpose |
|---|---|
| `get_processing_manifest` | Available processing presets |
| `open_study` | Open a BMP bitewing X-ray |
| `start_render_job` | Render a preview |
| `start_process_job` | Run processing pipeline |
| `start_analyze_job` | Generate deterministic analysis overlays |
| `get_job` | Poll job state |
| `get_jobs` | List job state |
| `cancel_job` | Cancel a running job |
| `measure_line_annotation` | Calibration-aware line measurement |

Each command is reached via `invoke("<command>", { command: <payload> })`
from the frontend.

---

## CLI

The headless CLI runs through the backend binary.

### Utility subcommands

```bash
# Info
npm run backend:cli -- print-config      # resolved config as JSON
npm run backend:cli -- version           # service + contract version
npm run backend:cli -- list-commands     # supported backend commands

# BMP inspection
npm run backend:cli -- decode-source /path/to/image.bmp

# Render & process
npm run backend:cli -- render-preview /path/to/image.bmp /tmp/preview.bmp
npm run backend:cli -- render-preview --full-range /path/to/image.bmp /tmp/preview.bmp
npm run backend:cli -- process-preview --invert --equalize /path/to/image.bmp /tmp/processed.bmp
npm run backend:cli -- analyze-preview /path/to/image.bmp /tmp/analyze.bmp
```

<details>
<summary>Legacy workflow flags</summary>

```bash
npm run backend:cli -- --describe-presets
npm run backend:cli -- --input /path/to/image.bmp --describe-study
npm run backend:cli -- --input /path/to/image.bmp --preview-output /tmp/preview.bmp
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
| `backend-rs/` | Rust library: BMP decode, render, processing, annotations, jobs. Also ships a headless CLI binary |
| `contracts/` | Shared command payload shapes via JSON schema |

```
┌─────────────┐    Tauri IPC    ┌──────────────────────────────────────┐
│  HTMX UI    │ ◄─────────────► │ desktop-tauri (Rust shell)           │
│  (frontend) │                  │   ↳ backend-rs::App (in-process)     │
└─────────────┘                  └──────────────────────────────────────┘
        ▲                                            ▲
        └───────────────── contracts ────────────────┘
```
