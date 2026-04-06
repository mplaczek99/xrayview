# Phase 8 Add Tauri-Go Process Management

This document completes phase 8 from [GO_BACKEND_MIGRATION_PLAN.md](GO_BACKEND_MIGRATION_PLAN.md). The Tauri shell now owns the Go sidecar lifecycle when the frontend is built for the `go-sidecar` runtime.

Primary implementation references:

- [frontend/src-tauri/src/go_sidecar.rs](frontend/src-tauri/src/go_sidecar.rs)
- [frontend/src-tauri/src/main.rs](frontend/src-tauri/src/main.rs)
- [frontend/src-tauri/build.rs](frontend/src-tauri/build.rs)
- [frontend/src-tauri/tauri.conf.json](frontend/src-tauri/tauri.conf.json)
- [frontend/scripts/prepare-go-sidecar.mjs](frontend/scripts/prepare-go-sidecar.mjs)
- [frontend/scripts/tauri-dev.mjs](frontend/scripts/tauri-dev.mjs)
- [frontend/scripts/tauri-build.mjs](frontend/scripts/tauri-build.mjs)
- [frontend/scripts/release-smoke-test.mjs](frontend/scripts/release-smoke-test.mjs)
- [README.md](README.md)

## 1. Shell-Owned Process Lifecycle

Phase 8 moves sidecar ownership into the existing Tauri shell:

- the Tauri app starts the Go backend during shell setup when the built frontend runtime is `go-sidecar`
- the shell waits for `GET /healthz` to report the expected local HTTP transport before the app continues
- the shell stops the sidecar when the desktop app exits

The frontend still does not spawn backend processes directly. Runtime selection remains centralized in the same build-time environment that already feeds the React app.

## 2. Build-Time Runtime Alignment

Phase 8 also closes a packaging gap that phase 7 left open.

The frontend runtime mode and Go sidecar URL are now exported from the Tauri build script into compile-time environment variables that the Rust shell reads at runtime:

- `XRAYVIEW_FRONTEND_BACKEND_RUNTIME`
- `XRAYVIEW_FRONTEND_GO_BACKEND_URL`

That keeps the shell and the compiled frontend aligned even in packaged builds, where `import.meta.env` values are baked into the frontend bundle and cannot be changed later by ordinary runtime environment variables.

## 3. Startup Safety And Stale-Process Detection

The shell now treats the sidecar port intentionally instead of opportunistically:

- if the configured loopback address already serves the expected Go backend, startup fails with a stale-process error instead of attaching to an orphan
- if the port is occupied by some other service, startup fails immediately
- if the sidecar exits before the health check succeeds, startup fails clearly

This keeps phase 8 consistent with the migration plan’s requirement that Tauri manage one backend process reliably instead of silently talking to a leftover process from some previous run.

## 4. Cache And Persistence Paths

The shell now injects sidecar paths explicitly:

- cache/artifact root stays under the existing temp-space layout used by the Tauri asset protocol: `$TEMP/xrayview/cache`
- sidecar persistence moves under the app-local data directory in a `go-backend/` subdirectory

Keeping preview artifacts under `$TEMP/xrayview/cache/artifacts` preserves the current `convertFileSrc(...)` and asset-protocol assumptions while the backend implementation is still migrating.

## 5. Sidecar Binary Preparation And Bundling

Phase 8 adds a dedicated Go sidecar preparation step to the frontend Tauri scripts:

- `frontend/scripts/prepare-go-sidecar.mjs` builds `go-backend/cmd/xrayviewd`
- the script emits the target-triple-suffixed binary name that Tauri expects for `bundle.externalBin`
- `tauri:dev` and `tauri:build` both prepare the Go sidecar before invoking the Tauri CLI
- packaged builds now declare `binaries/xrayview-go-backend` under `bundle.externalBin`

On Windows the sidecar build uses `-H=windowsgui` so the backend process does not open a second console window when the desktop app launches it.

## 6. Validation

Validate phase 8 with:

```bash
node frontend/scripts/prepare-go-sidecar.mjs
cargo test --manifest-path frontend/src-tauri/Cargo.toml
go -C go-backend test ./...
npm --prefix frontend run build
npm --prefix frontend run tauri:build -- --ci --no-bundle
```

Manual smoke check for the shell-managed path:

```bash
XRAYVIEW_BACKEND_RUNTIME=go-sidecar npm run tauri:dev
```

Expected phase 8 behavior:

- the shell launches the Go backend automatically
- startup fails fast if the configured sidecar address is already occupied
- quitting the app stops the sidecar

The Go command routes themselves are still phase 7 placeholders until phase 9 and later phases move real backend behavior behind them.

## 7. Exit Criteria Check

Phase 8 exit criteria are now met:

- Tauri starts the Go backend on app startup for the `go-sidecar` runtime
- Tauri stops the backend on app exit
- the shell passes cache and persistence configuration to the sidecar
- startup includes readiness checks and stale-process detection
- dev and packaged Tauri builds now prepare and bundle the Go sidecar binary intentionally
