// On Windows release builds, suppress the console window. Debug builds keep
// the console so println!/log output is visible while iterating.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;

use std::sync::Arc;

use tauri::{Emitter, Manager};
use xrayview_backend_rs::{app::App, config::Config};

// Tauri's managed state. Arc because the backend is shared across every
// IPC command handler (each request gets a clone of the Arc, not the App).
pub struct AppState {
    pub backend: Arc<App>,
}

fn main() {
    // Has to run before Tauri spins up GTK/WebKit — env vars set after threads
    // exist are racy on glibc. See apply_linux_webkit_workarounds below.
    apply_linux_webkit_workarounds();

    tauri::Builder::default()
        // The dialog plugin gives us the native file-open picker for BMPs.
        .plugin(tauri_plugin_dialog::init())
        .setup(|tauri_app| {
            // Linux compositors give us ugly server-side decorations by default;
            // we draw our own chrome in HTML so kill the native frame.
            #[cfg(target_os = "linux")]
            if let Some(window) = tauri_app.get_webview_window("main") {
                window.set_decorations(false)?;
            }

            // Load env-driven config. Fail loudly here — there's no sensible
            // way to start the app with a broken config.
            let config = Config::load().map_err(|message| {
                Box::<dyn std::error::Error>::from(format!("backend config failed: {message}"))
            })?;
            // App::new wires up caches and persistence stores but doesn't yet
            // create directories. That happens in prepare() below.
            let backend = Arc::new(App::new(config).map_err(|error| {
                Box::<dyn std::error::Error>::from(format!("backend init failed: {error}"))
            })?);
            // prepare() is split out so tests can construct an App without
            // creating real dirs on disk.
            backend.prepare().map_err(|error| {
                Box::<dyn std::error::Error>::from(format!("backend prepare failed: {error}"))
            })?;

            // Bridge backend job snapshots → Tauri events. Each subscriber
            // gets its own crossbeam channel; we spawn one thread per Tauri
            // app instance (so, one).
            let subscription = backend.subscribe_job_updates();
            let handle = tauri_app.handle().clone();
            std::thread::spawn(move || {
                // Blocking recv loop — exits naturally when the backend drops
                // the sender (i.e. app shutdown) or when emit fails (window
                // closed). No need for explicit cancellation.
                while let Ok(snapshot) = subscription.receiver.recv() {
                    if handle.emit("job-update", &snapshot).is_err() {
                        // Frontend gone, no point continuing to emit.
                        break;
                    }
                }
            });

            tauri_app.manage(AppState { backend });
            Ok(())
        })
        // Whitelist of IPC commands the frontend may call. Anything not in this
        // list is rejected at the Tauri layer before reaching commands.rs.
        .invoke_handler(tauri::generate_handler![
            commands::get_processing_manifest,
            commands::open_study,
            commands::start_render_job,
            commands::start_analyze_job,
            commands::start_process_job,
            commands::get_job,
            commands::get_jobs,
            commands::cancel_job,
            commands::measure_line_annotation,
        ])
        .run(tauri::generate_context!())
        // Panic here is intentional — if Tauri itself can't run, the app is
        // unusable and there's nothing graceful to do.
        .expect("error while running xrayview tauri shell");
}

// Linux/Wayland + WebKitGTK has a long-running bug where the DMA-BUF renderer
// crashes or produces black frames on some GPUs (notably NVIDIA, in case that
// shocks anyone). Disabling it forces the safer fallback path. We only do this
// if the user hasn't already set the var themselves.
#[cfg(target_os = "linux")]
fn apply_linux_webkit_workarounds() {
    // XDG_SESSION_TYPE is the more reliable signal, but WAYLAND_DISPLAY shows
    // up in nested compositor setups (sway-inside-X, etc.), so check both.
    let is_wayland = std::env::var_os("WAYLAND_DISPLAY").is_some()
        || std::env::var("XDG_SESSION_TYPE")
            .is_ok_and(|session_type| session_type.eq_ignore_ascii_case("wayland"));

    if is_wayland && std::env::var_os("WEBKIT_DISABLE_DMABUF_RENDERER").is_none() {
        // SAFETY: called before Tauri initializes GTK/WebKit or spawns threads.
        unsafe {
            std::env::set_var("WEBKIT_DISABLE_DMABUF_RENDERER", "1");
        }
    }
}

// No-op stub for non-Linux targets — keeps main() free of cfg() noise.
#[cfg(not(target_os = "linux"))]
fn apply_linux_webkit_workarounds() {}
