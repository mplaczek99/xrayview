# Phase 40 Replace Tauri Shell with Wails

This document completes phase 40 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The repository no longer treats the Tauri shell as the supported desktop host. The live desktop app is now the Wails shell in `desktop/`, which launches the existing React workstation frontend, owns native shell responsibilities, manages the Go sidecar lifecycle, and forwards the frontend command surface to the backend over the existing local HTTP contract.

Primary implementation references:

- [desktop/main.go](desktop/main.go)
- [desktop/app.go](desktop/app.go)
- [desktop/sidecar.go](desktop/sidecar.go)
- [desktop/scripts/build.mjs](desktop/scripts/build.mjs)
- [desktop/scripts/run.mjs](desktop/scripts/run.mjs)
- [frontend/src/lib/wails.ts](frontend/src/lib/wails.ts)
- [frontend/src/lib/shell.ts](frontend/src/lib/shell.ts)
- [frontend/src/lib/backend.ts](frontend/src/lib/backend.ts)
- [frontend/src/lib/runtime.ts](frontend/src/lib/runtime.ts)
- [frontend/scripts/wails-build.mjs](frontend/scripts/wails-build.mjs)
- [frontend/vite.wails.config.ts](frontend/vite.wails.config.ts)

## 1. The Desktop Shell Is Now Wails-First

Phase 39 proved Wails was viable. Phase 40 promotes that result into the real desktop path:

- the supported desktop entrypoints are now `npm run wails:build` and `npm run wails:run`
- the Wails shell binary is built at `desktop/build/bin/xrayview`
- the bundled Go sidecar binary is built beside it at `desktop/build/bin/xrayview-go-backend`
- the React frontend assets are built into `desktop/build/frontend/dist/`

That keeps the existing UI intact while moving shell ownership to Go.

## 2. Native Shell Responsibilities Moved Behind Wails Bindings

The desktop shell now exposes three frontend-facing capabilities through Wails bindings:

- `PickDicomFile`
- `PickSaveDicomPath`
- `InvokeBackendCommand`

The React app no longer imports Tauri APIs directly. Instead:

- `frontend/src/lib/wails.ts` owns the Wails binding surface
- `frontend/src/lib/shell.ts` resolves preview URLs through the Wails `/preview` route
- `frontend/src/lib/backend.ts` forwards every live backend command through the Wails shell binding instead of calling Tauri or talking to the sidecar from the browser context

This keeps the local HTTP boundary between shell and backend, but removes Tauri-specific frontend coupling from the live desktop flow.

## 3. Preview Artifact Handling Is Reimplemented For Wails

Tauri's asset protocol is gone from the supported path. The Wails shell now serves preview files through `/preview?path=...`:

- the frontend resolves preview URLs to that route
- the Wails handler validates that the path is absolute
- the file is streamed back with the detected content type

That preserves preview rendering behavior without changing the underlying Go backend artifact paths.

## 4. The Go Sidecar Lifecycle Is Owned By The Wails Shell

The Wails shell now owns sidecar startup and shutdown directly:

- runtime mode is resolved from `XRAYVIEW_BACKEND_RUNTIME`
- `desktop` starts or attaches to the loopback backend target
- `mock` skips sidecar startup
- shell-side command forwarding uses the same `/api/v1/commands/{command}` backend surface defined earlier in the migration

The earlier `legacy-rust` desktop fallback is no longer part of the supported live shell path, because that path depended on Tauri's in-process Rust bridge. The Rust backend remains in the repository only for the still-active helper responsibilities handled in later phases.

## 5. Build And Smoke Validation Now Target Wails

The repository build and smoke flow now points at Wails:

- root scripts export `wails:build` and `wails:run`
- frontend release smoke builds the Wails shell and validates the launched desktop binary
- the release smoke helper still accepts `--bundle`, but currently documents that the Wails path validates the built binary only

Validation completed in this phase:

- `npm --prefix frontend run build`
- `npm --prefix frontend run wails:build`
- `GOCACHE=/tmp/xrayview-go-build-cache GOTMPDIR=/tmp/xrayview-go-tmp go -C desktop test ./...`
- `npm run wails:build`

## 6. Exit Criteria

Phase 40 exit criteria are now met:

- file dialogs are owned by Wails
- save dialogs are owned by Wails
- app startup is owned by Wails
- preview path handling is owned by Wails
- frontend build integration targets the Wails desktop shell
- the supported desktop workflow now runs through Wails instead of Tauri
