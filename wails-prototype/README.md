# XRayView Wails Shell

This directory now owns the supported desktop shell for `xrayview`.

The Wails shell:

- launches the existing React workstation frontend
- exposes native open/save dialogs to the frontend through Wails bindings
- serves local preview artifacts back into the webview at `/preview`
- manages the Go sidecar lifecycle for the live desktop workflow
- forwards the frontend command surface to the Go backend over the existing local HTTP contract

## Commands

Build the frontend assets plus both Go binaries:

```bash
npm run wails:build
```

Build and immediately launch the desktop shell:

```bash
npm run wails:run
```

The frontend assets are written to `wails-prototype/build/frontend/dist/`, and the desktop app plus Go sidecar binaries are written to `wails-prototype/build/bin/`. The supported desktop build no longer depends on a Rust backend or Tauri shell crate.
