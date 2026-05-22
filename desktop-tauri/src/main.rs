#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod commands;

use std::sync::Arc;

use tauri::{Emitter, Manager};
use xrayview_backend_rs::{app::App, config::Config};

pub struct AppState {
    pub backend: Arc<App>,
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .setup(|tauri_app| {
            let config = Config::load().map_err(|message| {
                Box::<dyn std::error::Error>::from(format!("backend config failed: {message}"))
            })?;
            let backend = Arc::new(App::new(config).map_err(|error| {
                Box::<dyn std::error::Error>::from(format!("backend init failed: {error}"))
            })?);
            backend.prepare().map_err(|error| {
                Box::<dyn std::error::Error>::from(format!("backend prepare failed: {error}"))
            })?;

            let subscription = backend.subscribe_job_updates();
            let handle = tauri_app.handle().clone();
            std::thread::spawn(move || {
                while let Ok(snapshot) = subscription.receiver.recv() {
                    if handle.emit("job-update", &snapshot).is_err() {
                        break;
                    }
                }
            });

            tauri_app.manage(AppState { backend });
            Ok(())
        })
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
        .expect("error while running xrayview tauri shell");
}
