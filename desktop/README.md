# desktop (Wails shell)

Go-native desktop shell for XRayView, built on [Wails v2](https://wails.io). Wails
owns the native webview window and binds the Go `App` methods directly as the IPC
surface, so the backend orchestrator (`xrayview/backend`) runs in-process. There is
no Rust shell and no stdio sidecar — the GUI calls `backend/internal/app` directly.

## Layout

- `main.go` — builds the backend, mounts embedded assets, registers the preview
  asset handler, runs Wails. Applies the Linux Wayland/WebKit DMABUF workaround.
- `app.go` — the bound `App` struct: lifecycle (`startup`/`shutdown`), the
  `job-update` event pump, and `bindErr` (preserves structured `BackendError`
  across the Wails boundary).
- `bindings.go` — one method per contract command, reached from the webview as
  `window.go.main.App.<Method>`. Plus `PickBmpFile` (native file dialog).
- `assets.go` — `/previews?path=…` HTTP handler that streams rendered BMP previews
  from the backend cache dir (replaces Tauri's `asset://` / `convertFileSrc`).
- `dist/` — the built frontend, synced from `frontend/dist` and embedded via
  `//go:embed all:dist` (generated; only `.gitkeep` is tracked).

## Build & run

The Wails CLI must be on `PATH` (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`
installs it to `$(go env GOPATH)/bin`). From the repo root:

- `npm run desktop:dev` — hot-reload dev (`wails dev`); starts the Vite dev server.
- `npm run desktop:build` — production binary at `desktop/build/bin/xrayview`.

On Linux the webkit2gtk-4.1 build tag is required and is passed automatically
(`-tags webkit2_41`).

## Why a separate module

`desktop/` is its own Go module so the Wails cgo/webkit dependency stays out of
`xrayview/backend`. The backend library, its tests, and the headless CLI build as
pure Go with no native toolchain.
