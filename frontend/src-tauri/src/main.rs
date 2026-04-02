#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use rfd::FileDialog;
use std::env;
use std::fs;
use std::path::PathBuf;
use std::sync::Mutex;
use tauri::Manager;
use tempfile::Builder;
use xrayview_backend::api::{
    AnalyzeStudyCommand, AnalyzeStudyCommandResult, DescribeStudyCommand, PreviewCommandResult,
    ProcessStudyCommand, ProcessStudyCommandResult, ProcessingManifest, RenderPreviewCommand,
    StudyDescription,
};
use xrayview_backend::api::{AnalyzeStudyRequest, ProcessStudyRequest, RenderPreviewRequest};
use xrayview_backend::app::{
    analyze_study as backend_analyze_study, describe_study as backend_describe_study,
    process_study as backend_process_study, processing_manifest,
    render_preview as backend_render_preview,
};
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
async fn describe_study(request: DescribeStudyCommand) -> Result<StudyDescription, String> {
    run_blocking(move || backend_describe_study(&request.input_path)).await
}

#[tauri::command]
async fn render_preview(
    app: tauri::AppHandle,
    request: RenderPreviewCommand,
) -> Result<PreviewCommandResult, String> {
    let preview_path = create_temp_file(".png")?;
    track_temp_file(&app, &preview_path);

    let result = run_blocking(move || {
        backend_render_preview(RenderPreviewRequest {
            input_path: request.input_path,
            preview_output: preview_path.clone(),
        })
    })
    .await?;

    Ok(PreviewCommandResult {
        preview_path: result.preview_output,
        measurement_scale: result.measurement_scale,
    })
}

#[tauri::command]
async fn process_study(
    app: tauri::AppHandle,
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

    let backend_request = ProcessStudyRequest {
        input_path: request.input_path,
        output_path: Some(dicom_path.clone()),
        preview_output: Some(preview_path.clone()),
        preset: request.preset_id,
        invert: request.invert,
        brightness: request.brightness,
        contrast: request.contrast,
        equalize: request.equalize,
        compare: request.compare,
        pipeline: request.pipeline.map(join_pipeline_steps),
        palette: request.palette.map(|palette| palette.as_str().to_string()),
    };

    let result = run_blocking(move || backend_process_study(backend_request)).await?;

    Ok(ProcessStudyCommandResult {
        preview_path,
        dicom_path,
        loaded_width: result.loaded_width,
        loaded_height: result.loaded_height,
        mode: result.mode,
        measurement_scale: result.measurement_scale,
    })
}

#[tauri::command]
async fn analyze_study(
    app: tauri::AppHandle,
    request: AnalyzeStudyCommand,
) -> Result<AnalyzeStudyCommandResult, String> {
    let preview_path = create_temp_file(".png")?;
    track_temp_file(&app, &preview_path);
    let preview_output = preview_path.clone();

    let result = run_blocking(move || {
        backend_analyze_study(AnalyzeStudyRequest {
            input_path: request.input_path,
            preview_output: Some(preview_output),
        })
    })
    .await?;

    Ok(AnalyzeStudyCommandResult {
        preview_path,
        analysis: result.analysis,
    })
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

fn join_pipeline_steps(steps: Vec<xrayview_backend::api::ProcessingPipelineStep>) -> String {
    steps
        .into_iter()
        .map(|step| step.as_str())
        .collect::<Vec<_>>()
        .join(",")
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
            describe_study,
            render_preview,
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
