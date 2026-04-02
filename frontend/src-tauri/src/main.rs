#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use rfd::FileDialog;
use std::env;
use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;
use tauri::Manager;
use tempfile::Builder;
use xrayview_backend::api::{
    AnalyzeStudyCommand, AnalyzeStudyCommandResult, OpenStudyCommand, OpenStudyCommandResult,
    ProcessStudyCommand, ProcessStudyCommandResult, ProcessingManifest, RenderStudyCommand,
    RenderStudyCommandResult,
};
use xrayview_backend::app::processing_manifest;
use xrayview_backend::app::state::AppState as BackendAppState;
use xrayview_backend::error::BackendResult;

/// Temp preview and export files stay available for the lifetime of the app so
/// the frontend can keep rendering them through Tauri's asset protocol.
#[derive(Default)]
struct TempFileState {
    paths: Mutex<Vec<PathBuf>>,
}

impl TempFileState {
    fn track(&self, path: PathBuf) {
        if let Ok(mut paths) = self.paths.lock() {
            paths.push(path);
        }
    }

    fn cleanup_all(&self) {
        if let Ok(mut paths) = self.paths.lock() {
            for path in paths.drain(..) {
                let _ = fs::remove_file(&path);
            }
        }
    }
}

#[tauri::command]
fn pick_dicom_file() -> Option<String> {
    FileDialog::new()
        .set_title("Open DICOM Study")
        .pick_file()
        .map(path_to_string)
}

#[tauri::command]
fn pick_save_dicom_path(default_name: Option<String>) -> Option<String> {
    let mut dialog = FileDialog::new().add_filter("DICOM", &["dcm", "dicom"]);
    if let Some(name) = default_name {
        dialog = dialog.set_file_name(&name);
    }

    dialog.save_file().map(path_to_string)
}

#[tauri::command]
fn get_processing_manifest() -> ProcessingManifest {
    processing_manifest()
}

#[tauri::command]
async fn open_study(
    backend_state: tauri::State<'_, BackendAppState>,
    request: OpenStudyCommand,
) -> Result<OpenStudyCommandResult, String> {
    let backend_state = backend_state.inner().clone();

    run_blocking(move || {
        backend_state
            .open_study(request.input_path)
            .map(|study| OpenStudyCommandResult { study })
    })
    .await
}

#[tauri::command]
async fn render_study(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: RenderStudyCommand,
) -> Result<RenderStudyCommandResult, String> {
    let preview_path = create_temp_file(".png")?;
    track_temp_file(&app, &preview_path);
    let backend_state = backend_state.inner().clone();

    run_blocking(move || backend_state.render_study(request, preview_path)).await
}

#[tauri::command]
async fn process_study(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: ProcessStudyCommand,
) -> Result<ProcessStudyCommandResult, String> {
    let preview_path = create_temp_file(".png")?;
    track_temp_file(&app, &preview_path);

    let dicom_path = match request.output_path.clone() {
        Some(path) => path,
        None => {
            let temp_output = create_temp_file(".dcm")?;
            track_temp_file(&app, &temp_output);
            temp_output
        }
    };
    let backend_state = backend_state.inner().clone();

    run_blocking(move || backend_state.process_study(request, preview_path, dicom_path)).await
}

#[tauri::command]
async fn analyze_study(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: AnalyzeStudyCommand,
) -> Result<AnalyzeStudyCommandResult, String> {
    let preview_path = create_temp_file(".png")?;
    track_temp_file(&app, &preview_path);
    let backend_state = backend_state.inner().clone();

    run_blocking(move || backend_state.analyze_study(request, preview_path)).await
}

async fn run_blocking<T, F>(work: F) -> Result<T, String>
where
    T: Send + 'static,
    F: FnOnce() -> BackendResult<T> + Send + 'static,
{
    tauri::async_runtime::spawn_blocking(work)
        .await
        .map_err(|error| format!("desktop worker failed: {error}"))?
        .map_err(|error| error.to_string())
}

fn create_temp_file(suffix: &str) -> Result<PathBuf, String> {
    let temp_file = Builder::new()
        .prefix("xrayview-frontend-")
        .suffix(suffix)
        .tempfile()
        .map_err(|error| format!("failed to create temporary file: {error}"))?;

    let (_file, path) = temp_file
        .keep()
        .map_err(|error| format!("failed to persist temporary file: {error}"))?;

    Ok(path)
}

fn track_temp_file(app: &tauri::AppHandle, path: &PathBuf) {
    app.state::<TempFileState>().track(path.clone());
}

fn path_to_string(path: PathBuf) -> String {
    path.to_string_lossy().to_string()
}

fn configure_linux_webkit_environment() {
    #[cfg(target_os = "linux")]
    {
        if env::var_os("WEBKIT_DISABLE_DMABUF_RENDERER").is_none() {
            env::set_var("WEBKIT_DISABLE_DMABUF_RENDERER", "1");
        }
    }
}

#[cfg(target_os = "linux")]
fn configure_linux_application_identity() {
    glib::set_prgname(Some("xrayview"));
    glib::set_application_name("XRayView");
}

#[cfg(not(target_os = "linux"))]
fn configure_linux_application_identity() {}

fn main() {
    configure_linux_webkit_environment();
    configure_linux_application_identity();

    let app = tauri::Builder::default()
        .manage(BackendAppState::default())
        .manage(TempFileState::default())
        .setup(|app| {
            #[cfg(target_os = "linux")]
            {
                if let Some(window) = app.get_webview_window("main") {
                    window.set_decorations(false)?;
                }
            }

            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            pick_dicom_file,
            pick_save_dicom_path,
            get_processing_manifest,
            open_study,
            render_study,
            process_study,
            analyze_study,
        ])
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|app_handle, event| {
        if matches!(
            event,
            tauri::RunEvent::Exit | tauri::RunEvent::ExitRequested { .. }
        ) {
            app_handle.state::<TempFileState>().cleanup_all();
        }
    });
}
