// Tauri IPC surface. Each function here is a `#[tauri::command]` so the
// frontend can call it via `invoke()`. Every one is a thin pass-through into
// the backend `App` — keep it that way. Business logic lives in the library,
// not here.
//
// Errors are surfaced as `BackendError` so the frontend gets a structured
// payload (code + message), not a raw string. The TS side keys off the
// `code` field.

use tauri::State;
use xrayview_backend_rs::contracts::{
    AnalyzeStudyCommand, BackendError, GetJobsCommand, JobCommand, JobSnapshot,
    MeasureLineAnnotationCommand, MeasureLineAnnotationCommandResult, OpenStudyCommand,
    OpenStudyCommandResult, ProcessStudyCommand, ProcessingManifest, RenderStudyCommand,
    SetStudyCalibrationCommand, SetStudyCalibrationCommandResult, StartedJob,
};

use crate::AppState;

// Static manifest describing what knobs the frontend can flip. Cheap, sync —
// no point making this async.
#[tauri::command]
pub fn get_processing_manifest(state: State<'_, AppState>) -> ProcessingManifest {
    state.backend.get_processing_manifest()
}

// Synchronously opens a study by path: validates it, loads metadata, and
// produces the IDs the frontend then uses for follow-up render/process/analyze.
// Sync because the response is needed before the UI can show anything useful.
#[tauri::command]
pub fn open_study(
    state: State<'_, AppState>,
    command: OpenStudyCommand,
) -> Result<OpenStudyCommandResult, BackendError> {
    state.backend.open_study(command)
}

// The three start_*_job_async commands kick off background work and return
// immediately with a job handle. Progress comes through the "job-update"
// event channel set up in main.rs — never poll these from the frontend.
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

// Reads a single job snapshot. The frontend uses this for the initial fetch
// after start_*_job before the event stream has caught up.
#[tauri::command]
pub fn get_job(
    state: State<'_, AppState>,
    command: JobCommand,
) -> Result<JobSnapshot, BackendError> {
    state.backend.get_job(command)
}

// Lists all known jobs matching the filter. Used by the jobs panel.
#[tauri::command]
pub fn get_jobs(
    state: State<'_, AppState>,
    command: GetJobsCommand,
) -> Result<Vec<JobSnapshot>, BackendError> {
    state.backend.get_jobs(command)
}

// Cooperative cancel — the worker thread checks a flag between stages, so
// cancel doesn't kill mid-allocation. Idempotent: cancelling a completed job
// just returns its current state.
#[tauri::command]
pub fn cancel_job(
    state: State<'_, AppState>,
    command: JobCommand,
) -> Result<JobSnapshot, BackendError> {
    state.backend.cancel_job(command)
}

// Pure math (hypot + optional mm scale). Sync because it's microseconds and
// doesn't touch I/O.
#[tauri::command]
pub fn measure_line_annotation(
    state: State<'_, AppState>,
    command: MeasureLineAnnotationCommand,
) -> Result<MeasureLineAnnotationCommandResult, BackendError> {
    state.backend.measure_line_annotation(command)
}

// Sets or clears a study's manual measurement calibration. Sync — derives the
// scale and updates the in-memory study record without touching job machinery.
#[tauri::command]
pub fn set_study_calibration(
    state: State<'_, AppState>,
    command: SetStudyCalibrationCommand,
) -> Result<SetStudyCalibrationCommandResult, BackendError> {
    state.backend.set_study_calibration(command)
}
