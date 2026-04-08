# XRayView Desktop Shell

This directory owns the supported desktop shell for `xrayview`.

Responsibilities:

- launch the React workstation frontend inside Wails
- expose native open/save dialogs through Wails bindings
- serve local preview artifacts back into the webview at `/preview`
- manage the Go backend sidecar lifecycle for live desktop mode
- forward frontend commands to the Go backend over the local HTTP contract

## Commands

Build and launch the desktop app:

```bash
npm run wails:run
```

Build the desktop shell plus bundled backend sidecar:

```bash
npm run wails:build
```

Run the shell tests directly:

```bash
go -C desktop test ./...
```

Build outputs:

- frontend assets: `desktop/build/frontend/dist/`
- desktop binary: `desktop/build/bin/xrayview`
- backend sidecar binary: `desktop/build/bin/xrayview-go-backend`

Supported runtime modes for shell launches:

- `desktop` - start or attach to the local Go backend sidecar
- `mock` - skip the live backend and keep the UI in browser-like mock mode
