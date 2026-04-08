# XRayView Wails Prototype

This directory holds the focused shell prototype for phase 39 of the Go backend migration plan.

The prototype is intentionally narrow. It proves:

- Wails application launch against the existing React/Vite toolchain
- native open-file and save-file dialogs without the Tauri bridge
- local preview artifact loading through a Wails asset handler
- one live backend call path via `openStudy` against the Go sidecar

## Commands

Build the prototype frontend and both Go binaries:

```bash
npm run wails:prototype:build
```

Build and immediately launch the prototype:

```bash
npm run wails:prototype:run
```

The prototype expects its frontend assets under `wails-prototype/frontend/dist/`, which are produced by `npm --prefix frontend run wails:prototype:build`.

## Scope

This is not the phase 40 shell replacement. It is a decision aid for:

- shell runtime fit
- dialog API ergonomics
- local artifact serving behavior
- Wails build friction on the current Linux desktop stack

The production Tauri shell remains unchanged in this phase.
