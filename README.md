<h1 align="center">xrayview</h1>

<p align="center">
  A DICOM X-ray visualization and analysis workstation<br>
  built with a <strong>Wails</strong> desktop shell, a <strong>React/TypeScript</strong> frontend, and a <strong>Go</strong> backend.
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
- Run background render, process, and analyze jobs with cancellation
- Measure line annotations with calibration-aware distances when pixel spacing metadata is available
- Suggest tooth annotations from the Go analysis pipeline
- Persist a recent-studies catalog

> The user-facing workflow is **DICOM in, DICOM out**. PNG previews are an
> internal display artifact for the desktop UI.

---

## Repository Layout

```
xrayview/
├── frontend/    React/TypeScript workstation UI (Vite, strict mode)
├── desktop/     Wails desktop shell (Go module)
├── backend/     Go backend service & headless CLI (Go module)
│   ├── cmd/xrayviewd       HTTP server entrypoint
│   ├── cmd/xrayview-cli    headless CLI
│   └── internal/           domain packages
├── contracts/   shared JSON schema + generated TS & Go bindings (Go module)
└── images/      sample DICOM assets for dev & smoke testing
```

---

## Getting Started

### Prerequisites

- [Go](https://go.dev/) 1.22+
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

Build and launch the Wails shell with the live Go backend:

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
| `desktop` | Wails shell | Live Go backend over loopback HTTP |

Override with environment variables:

```bash
XRAYVIEW_BACKEND_RUNTIME=mock npm run dev
XRAYVIEW_BACKEND_RUNTIME=desktop XRAYVIEW_BACKEND_URL=http://127.0.0.1:38181 npm run wails:run
```

---

## Go Backend

The backend sidecar binds to `127.0.0.1:38181` by default. The transport is
**intentionally local-only** — it only binds to loopback and is never exposed
in mock mode.

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
| `start_analyze_job` | Run analysis pipeline |
| `get_job` | Poll job state |
| `cancel_job` | Cancel a running job |
| `measure_line_annotation` | Calibration-aware line measurement |

---

## CLI

The headless CLI lives at `backend/cmd/xrayview-cli` and supports **utility
subcommands** and **legacy workflow flags**.

### Utility subcommands

```bash
# Info
go -C backend run ./cmd/xrayview-cli print-config      # resolved config as JSON
go -C backend run ./cmd/xrayview-cli version           # service + contract version
go -C backend run ./cmd/xrayview-cli list-commands     # supported backend commands

# DICOM inspection
go -C backend run ./cmd/xrayview-cli inspect-decode ../images/sample-dental-radiograph.dcm
go -C backend run ./cmd/xrayview-cli decode-source  ../images/sample-dental-radiograph.dcm

# Render & process
go -C backend run ./cmd/xrayview-cli render-preview ../images/sample-dental-radiograph.dcm /tmp/preview.png
go -C backend run ./cmd/xrayview-cli render-preview --full-range ../images/sample-dental-radiograph.dcm /tmp/preview.png
go -C backend run ./cmd/xrayview-cli process-preview --invert --equalize ../images/sample-dental-radiograph.dcm /tmp/processed.png

# Export
go -C backend run ./cmd/xrayview-cli export-secondary-capture --palette hot ../images/sample-dental-radiograph.dcm /tmp/export.dcm
```

<details>
<summary>Legacy workflow flags</summary>

```bash
go -C backend run ./cmd/xrayview-cli -- --describe-presets
go -C backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --describe-study
go -C backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --analyze-tooth
go -C backend run ./cmd/xrayview-cli -- --input ../images/sample-dental-radiograph.dcm --preview-output /tmp/preview.png
```

</details>

> A public dental radiograph sample is included at
> `images/sample-dental-radiograph.dcm`. See `images/README.md` for provenance.

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

The project is a monorepo with **three independent Go modules** and a
React/TypeScript frontend. There is no Go workspace file; modules use `replace`
directives for local dependencies.

| Module | Responsibility |
|---|---|
| `frontend/` | Workstation UI and mock-mode behavior |
| `desktop/` | Native shell: window lifecycle, dialogs, preview serving, sidecar management |
| `backend/` | DICOM decode, render, processing, analysis, annotations, export, jobs, cache, persistence |
| `contracts/` | Shared command payload shapes via JSON schema |

```
┌─────────────┐     Wails binding     ┌─────────────┐    loopback HTTP    ┌─────────────┐
│  React UI   │ ◄──────────────────► │   Desktop   │ ◄──────────────────► │ Go Backend  │
│  (frontend) │                       │   (desktop) │                      │  (backend)  │
└─────────────┘                       └─────────────┘                      └─────────────┘
                                              ▲                                    ▲
                                              └────────── contracts ───────────────┘
```
