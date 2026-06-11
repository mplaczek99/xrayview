<h1 align="center">xrayview</h1>

<p align="center">
  A BMP bitewing X-ray visualization workstation<br>
  built with a <strong>Go + Wails</strong> desktop shell, an <strong>HTMX/TypeScript</strong> frontend, and a <strong>Go</strong> backend.
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
- Measure line annotations, with manual mm calibration from a known-length reference
- Persist a recent-studies catalog

> The user-facing workflow is **BMP in, BMP previews for display and processing**.

---

## Repository Layout

```
xrayview/
├── frontend/        HTMX/TypeScript workstation UI (Vite)
├── desktop/         Go + Wails 2 desktop shell (binds the backend in-process)
├── backend/         Go backend library, headless CLI, and the `shell` bind seam
├── contracts/       shared JSON schema + generated TypeScript bindings
└── images/          sample image assets for dev & detector tuning
```

---

## Getting Started

### Prerequisites

- [Go](https://go.dev/doc/install) 1.26+
- [Node.js](https://nodejs.org/) 20+
- [Wails CLI](https://wails.io): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
  (ensure `$(go env GOPATH)/bin` is on your `PATH`); run `wails doctor` to check
  platform deps
- Linux desktop builds require GTK/WebKit development packages.
  On Debian/Ubuntu: `libgtk-3-dev`, `libwebkit2gtk-4.1-dev`, `librsvg2-dev`.
  The `webkit2_41` build tag is passed automatically by the desktop scripts.
- Windows desktop builds use WebView2 (bundled/auto-installed by Wails)

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

Build and launch the Wails shell (the Go backend runs in-process):

```bash
npm run desktop:dev             # hot-reload dev launch
npm run desktop:build           # release binary
```

<details>
<summary>Build outputs</summary>

| Artifact | Path |
|---|---|
| Frontend assets | `frontend/dist/`, synced into `desktop/dist/` and embedded in the binary |
| Desktop binary | `desktop/build/bin/xrayview` |

</details>

### Release smoke test

```bash
npm run release:smoke
```

Checks contract drift, runs Go backend tests, builds the frontend, then runs the
Wails desktop build (`npm run desktop:build`).

---

## Runtime Modes

| Mode | Default in | Description |
|---|---|---|
| `mock` | Browser / Vite | Synthetic data, no backend |
| `desktop` | Wails shell | Live in-process Go backend via Wails IPC |

The runtime is normally auto-detected (Wails injects `window.runtime` and
`window.go` into the WebView). To override:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
```

---

## Go Backend

`backend/` contains the backend packages plus the headless
`xrayview-backend` binary for scripted/manual inspection of BMP studies. The
Wails desktop shell runs the backend in-process and binds its methods through the
public `backend/shell` seam; there is no sidecar process and no local HTTP
server. (The stdio `serve-ipc` server remains in `backend/internal/ipc` and is
exercised by tests, but the GUI no longer uses it.)

```bash
npm run backend:build
npm run backend:test
```

### Command surface (Wails IPC)

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
| `set_study_calibration` | Set/clear mm-per-pixel scale from a known-length line |

Each command is reached via `window.go.main.App.<Command>(<payload>)` from the
frontend (the methods are PascalCased, e.g. `OpenStudy`).

---

## CLI

The headless CLI runs through the backend binary.

### Utility subcommands

```bash
# Info
npm run backend:cli -- print-config      # resolved config as JSON
npm run backend:cli -- version           # service + contract version
npm run backend:cli -- list-commands     # supported backend commands
npm run backend:cli -- processing-manifest # processing presets

# BMP inspection
npm run backend:cli -- describe-study /path/to/image.bmp
npm run backend:cli -- decode-source /path/to/image.bmp

# Render & process
npm run backend:cli -- render-preview /path/to/image.bmp /tmp/preview.bmp
npm run backend:cli -- render-preview --full-range /path/to/image.bmp /tmp/preview.bmp
npm run backend:cli -- process-preview --invert --equalize /path/to/image.bmp /tmp/processed.bmp
npm run backend:cli -- analyze-preview /path/to/image.bmp /tmp/analyze.bmp
```

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

Go types in `backend/internal/contracts` are the matching backend source
side (manually kept in sync; not generated).

---

## Architecture

The project is a pure-Go + TypeScript monorepo. The Wails desktop shell hosts the
native webview and runs the Go backend in-process — no sidecar.

| Module | Responsibility |
|---|---|
| `frontend/` | Workstation UI and mock-mode behavior |
| `desktop/` | Go + Wails shell: window lifecycle, file dialog, command bindings, job-event forwarding, preview asset handler |
| `backend/` | Go backend: BMP decode, render, processing, analysis overlays, annotations, jobs, CLI; `shell` is the public bind seam |
| `contracts/` | Shared command payload shapes via JSON schema |

```
┌─────────────┐   Wails IPC     ┌──────────────────────────────────────┐
│  HTMX UI    │ ◄────────────►  │ desktop (Go + Wails)                 │
│  (frontend) │  window.go.*    │   ↳ backend/shell → backend (in-proc)│
└─────────────┘                 └──────────────────────────────────────┘
        ▲                                            ▲
        └───────────────── contracts ────────────────┘
```
