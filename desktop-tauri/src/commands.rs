use tauri::State;
use xrayview_backend_rs::contracts::{
    AnalyzeStudyCommand, BackendError, GetJobsCommand, JobCommand, JobSnapshot,
    MeasureLineAnnotationCommand, MeasureLineAnnotationCommandResult, OpenStudyCommand,
    OpenStudyCommandResult, ProcessStudyCommand, ProcessingManifest, RenderStudyCommand,
    StartedJob,
};

use crate::AppState;

#[tauri::command]
pub fn get_processing_manifest(state: State<'_, AppState>) -> ProcessingManifest {
    state.backend.get_processing_manifest()
}

#[tauri::command]
pub fn open_study(
    state: State<'_, AppState>,
    command: OpenStudyCommand,
) -> Result<OpenStudyCommandResult, BackendError> {
    state.backend.open_study(command)
}

#[tauri::command]
pub fn start_render_job(
    state: State<'_, AppState>,
    command: RenderStudyCommand,
) -> Result<StartedJob, BackendError> {
    state.backend.start_render_job_async(command)
}

#[tauri::command]
pub fn start_analyze_job(
    state: State<'_, AppState>,
    command: AnalyzeStudyCommand,
) -> Result<StartedJob, BackendError> {
    state.backend.start_analyze_job_async(command)
}

#[tauri::command]
pub fn start_process_job(
    state: State<'_, AppState>,
    command: ProcessStudyCommand,
) -> Result<StartedJob, BackendError> {
    state.backend.start_process_job_async(command)
}

#[tauri::command]
pub fn get_job(
    state: State<'_, AppState>,
    command: JobCommand,
) -> Result<JobSnapshot, BackendError> {
    state.backend.get_job(command)
}

#[tauri::command]
pub fn get_jobs(
    state: State<'_, AppState>,
    command: GetJobsCommand,
) -> Result<Vec<JobSnapshot>, BackendError> {
    state.backend.get_jobs(command)
}

#[tauri::command]
pub fn cancel_job(
    state: State<'_, AppState>,
    command: JobCommand,
) -> Result<JobSnapshot, BackendError> {
    state.backend.cancel_job(command)
}

#[tauri::command]
pub fn measure_line_annotation(
    state: State<'_, AppState>,
    command: MeasureLineAnnotationCommand,
) -> Result<MeasureLineAnnotationCommandResult, BackendError> {
    state.backend.measure_line_annotation(command)
}
