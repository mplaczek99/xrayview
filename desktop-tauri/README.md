# XRayView Desktop Shell (Tauri)

This directory owns the supported desktop shell for `xrayview`. It is a Tauri 2
crate that links `backend-rs` as a library and hosts the application
in-process — there is no separate backend sidecar.

Responsibilities:

- launch the HTMX workstation frontend inside a Tauri WebView
- expose native open/save dialogs through `@tauri-apps/plugin-dialog`
- own the `xrayview_backend_rs::app::App` and bridge its commands to the
  frontend via `#[tauri::command]` IPC handlers
- forward `App::subscribe_job_updates` snapshots to Tauri's event bus as
  `"job-update"` so the frontend `listen("job-update", …)` hook receives them

## Commands

Build and launch the desktop app in dev mode:

```bash
npm run tauri:dev
```

Build the release binary:

```bash
npm run tauri:build           # binary + installer bundles
npm run tauri:build -- --no-bundle   # binary only
```

Build outputs:

- desktop binary: `desktop-tauri/target/release/xrayview`
- installer bundles: `desktop-tauri/target/release/bundle/<format>/...`
- frontend assets bundled into the binary at compile time (built from
  `frontend/dist`)

## Architecture

```
HTMX UI (frontend) ─── Tauri IPC ───▶ desktop-tauri (Rust shell)
                                          │
                                          ▼
                                 xrayview_backend_rs::App (in-process)
```

The shell links `backend-rs` directly as a library — there is no local HTTP
server. The standalone `xrayview-backend-rs` binary is the headless CLI used
for scripted inspection of BMP studies.

## Platform support

- **Linux:** webkit2gtk-4.1 (preferred) or 4.0.
- **Windows:** WebView2 (auto-installed by the Tauri bundler).
- **macOS:** the Tauri stack supports it but the icons in `icons/` are
  placeholders and there is no signed-bundle CI yet.
