#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

use rfd::FileDialog;
use std::env;
use std::path::PathBuf;
use std::sync::Arc;
use tauri::{AppHandle, Emitter, Manager};
use xrayview_backend::api::{
    AnalyzeStudyCommand, JobCommand, JobSnapshot, JobState, MeasureLineAnnotationCommand,
    MeasureLineAnnotationCommandResult, OpenStudyCommand, OpenStudyCommandResult,
    ProcessStudyCommand, ProcessingManifest, RenderStudyCommand, StartedJob,
};
use xrayview_backend::app::processing_manifest;
use xrayview_backend::app::state::AppState as BackendAppState;
use xrayview_backend::error::{BackendError, BackendResult};

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
) -> Result<OpenStudyCommandResult, BackendError> {
    let backend_state = backend_state.inner().clone();

    run_blocking(move || backend_state.open_study_command(request.input_path)).await
}

#[tauri::command]
fn start_render_job(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: RenderStudyCommand,
) -> Result<StartedJob, BackendError> {
    let publish = job_publisher(app);
    backend_state.inner().clone().start_render_job(request, publish)
}

#[tauri::command]
fn start_process_job(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: ProcessStudyCommand,
) -> Result<StartedJob, BackendError> {
    let publish = job_publisher(app);
    backend_state.inner().clone().start_process_job(request, publish)
}

#[tauri::command]
fn start_analyze_job(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: AnalyzeStudyCommand,
) -> Result<StartedJob, BackendError> {
    let publish = job_publisher(app);
    backend_state.inner().clone().start_analyze_job(request, publish)
}

#[tauri::command]
fn get_job(
    backend_state: tauri::State<'_, BackendAppState>,
    request: JobCommand,
) -> Result<JobSnapshot, BackendError> {
    backend_state.inner().clone().get_job(request)
}

#[tauri::command]
fn cancel_job(
    app: tauri::AppHandle,
    backend_state: tauri::State<'_, BackendAppState>,
    request: JobCommand,
) -> Result<JobSnapshot, BackendError> {
    let snapshot = backend_state.inner().clone().cancel_job(request)?;
    emit_job_snapshot(&app, &snapshot);
    Ok(snapshot)
}

#[tauri::command]
fn measure_line_annotation(
    backend_state: tauri::State<'_, BackendAppState>,
    request: MeasureLineAnnotationCommand,
) -> Result<MeasureLineAnnotationCommandResult, BackendError> {
    backend_state.inner().clone().measure_line_annotation(request)
}

async fn run_blocking<T, F>(work: F) -> Result<T, BackendError>
where
    T: Send + 'static,
    F: FnOnce() -> BackendResult<T> + Send + 'static,
{
    tauri::async_runtime::spawn_blocking(work)
        .await
        .map_err(|error| BackendError::internal(format!("desktop worker failed: {error}")))?
}

fn job_publisher(app: AppHandle) -> Arc<dyn Fn(JobSnapshot) + Send + Sync + 'static> {
    Arc::new(move |snapshot| emit_job_snapshot(&app, &snapshot))
}

fn emit_job_snapshot(app: &AppHandle, snapshot: &JobSnapshot) {
    let event = match snapshot.state {
        JobState::Completed => "job:completed",
        JobState::Failed => "job:failed",
        JobState::Cancelled => "job:cancelled",
        JobState::Queued | JobState::Running | JobState::Cancelling => "job:progress",
    };
    let _ = app.emit(event, snapshot);
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

    tauri::Builder::default()
        .manage(BackendAppState::default())
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
            start_render_job,
            start_process_job,
            start_analyze_job,
            get_job,
            cancel_job,
            measure_line_annotation,
        ])
        .build(tauri::generate_context!())
        .expect("error while building tauri application")
        .run(|_, _| {});
}
