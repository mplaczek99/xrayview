<h1 align="center">xrayview</h1>

<p align="center">
  A DICOM X-ray visualization workstation<br>
  built with a <strong>Wails</strong> desktop shell, a <strong>React/TypeScript</strong> frontend, and a <strong>Rust</strong> backend.
</p>

> [!CAUTION]
> This tool is for **image visualization only**.
> It is not a medical device and must not be used for medical diagnosis,
> clinical decisions, or treatment planning.

---

## Features

- Open local DICOM studies (`.dcm`, `.dicom`)
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
├── frontend/    React/TypeScript workstation UI (Vite, strict mode)
├── desktop/     Wails desktop shell (Go module)
├── backend-rs/  Rust backend service sidecar
├── contracts/   shared JSON schema + generated TS & Go bindings (Go module)
└── images/      sample image assets for dev & detector tuning
```

---

## Getting Started

### Prerequisites

- [Go](https://go.dev/) 1.22+
- [Rust](https://www.rust-lang.org/tools/install) 1.85+
- [Node.js](https://nodejs.org/) 18.18+ or 20+
- Linux desktop builds require GTK/WebKit development packages
  On Debian/Ubuntu: `libgtk-3-dev` plus either `libwebkit2gtk-4.1-dev` or `libwebkit2gtk-4.0-dev`

### Install & verify

```bash
npm install
npm run contracts:check
npm run backend:test
go -C desktop test ./...
```

### Browser mock mode

Run the React UI with synthetic data — no backend needed:

```bash
npm run dev
```

### Desktop app

Build and launch the Wails shell with the live Rust backend:

```bash
npm run wails:run          # dev launch
npm run wails:build        # release-style binaries
```

<details>
<summary>Build outputs</summary>

| Artifact | Path |
|---|---|
| Frontend assets | `desktop/build/frontend/dist/` |
| Desktop shell binary | `desktop/build/bin/xrayview` |
| Backend sidecar | `desktop/build/bin/xrayview-backend` |

</details>

### Release smoke test

```bash
npm run release:smoke
```

Checks contract drift, runs backend tests, builds frontend + Wails shell, and
confirms the bundled sidecar starts up.

---

## Runtime Modes

| Mode | Default in | Description |
|---|---|---|
| `mock` | Browser / Vite | Synthetic data, no backend |
| `desktop` | Wails shell | Live Rust backend over loopback HTTP |

Override with environment variables:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
XRAYVIEW_BACKEND_RUNTIME=desktop XRAYVIEW_BACKEND_URL=http://127.0.0.1:38181 npm run wails:run
```

---

## Rust Backend

The backend sidecar binds to `127.0.0.1:38181` by default. The transport is
**intentionally local-only** — it only binds to loopback and is never exposed
in mock mode.

The default backend scripts target `backend-rs/`:

```bash
npm run backend:build
npm run backend:serve
npm run backend:test
```

### HTTP endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/healthz` | Health check |
| `GET` | `/api/v1/runtime` | Runtime info & supported commands |
| `GET` | `/api/v1/commands` | List available commands |
| `POST` | `/api/v1/commands/{command}` | Execute a command |

### Command surface

| Command | Purpose |
|---|---|
| `get_processing_manifest` | Available processing presets |
| `open_study` | Open a DICOM study |
| `start_render_job` | Render a preview |
| `start_process_job` | Run processing pipeline |
| `start_analyze_job` | Generate deterministic analysis overlays |
| `get_job` | Poll job state |
| `get_jobs` | List job state |
| `cancel_job` | Cancel a running job |
| `measure_line_annotation` | Calibration-aware line measurement |

---

## CLI

The headless CLI runs through the Rust backend binary.

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
npm run contracts:generate    # regenerate bindings
npm run contracts:check       # verify bindings are up to date
```

Generated files (do not edit manually):

- `frontend/src/lib/generated/contracts.ts`
- `contracts/contractv1/bindings.go`

---

## Architecture

The project is a monorepo with a Rust backend sidecar, a React/TypeScript
frontend, and independent Go modules for the Wails shell and shared Go contract
bindings. There is no Go workspace file; Go modules use `replace` directives
for local dependencies.

| Module | Responsibility |
|---|---|
| `frontend/` | Workstation UI and mock-mode behavior |
| `desktop/` | Native shell: window lifecycle, dialogs, preview serving, sidecar management |
| `backend-rs/` | Rust sidecar: HTTP contract, DICOM decode, render, processing, annotations, jobs |
| `contracts/` | Shared command payload shapes via JSON schema |

```
┌─────────────┐     Wails binding     ┌─────────────┐    loopback HTTP    ┌─────────────┐
│  React UI   │ ◄──────────────────► │   Desktop   │ ◄──────────────────► │Rust Backend │
│  (frontend) │                       │   (desktop) │                      │(backend-rs) │
└─────────────┘                       └─────────────┘                      └─────────────┘
                                              ▲                                    ▲
                                              └────────── contracts ───────────────┘
```
