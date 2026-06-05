// Data contracts between the backend and the HTMX/TS frontend. Every
// struct/enum here has a counterpart in contracts/backend-contract-v1.schema.json
// and frontend/src/lib/generated/contracts.ts — if you change one, you have to
// hand-sync the Rust side and re-run `npm run contracts:generate` for TS.
//
// All field shapes use camelCase on the wire (see #[serde(rename_all)]) and
// most reject unknown fields (deny_unknown_fields) so wire-format drift gets
// caught at deserialize time rather than silently ignored.

use serde::{Deserialize, Serialize};
use std::sync::Arc;

pub const SERVICE_NAME: &str = "xrayview-backend";
// Bump this whenever the schema breaks compatibility — the desktop shell
// refuses to talk to a backend with a different major version.
pub const BACKEND_CONTRACT_VERSION: u32 = 2;

// Command name constants. Frontend invoke() calls use these literal strings;
// keeping them as named constants here means a rename only happens in one place.
pub const COMMAND_GET_PROCESSING_MANIFEST: &str = "get_processing_manifest";
pub const COMMAND_OPEN_STUDY: &str = "open_study";
pub const COMMAND_START_RENDER_JOB: &str = "start_render_job";
pub const COMMAND_START_ANALYZE_JOB: &str = "start_analyze_job";
pub const COMMAND_START_PROCESS_JOB: &str = "start_process_job";
pub const COMMAND_GET_JOB: &str = "get_job";
pub const COMMAND_GET_JOBS: &str = "get_jobs";
pub const COMMAND_CANCEL_JOB: &str = "cancel_job";
pub const COMMAND_MEASURE_LINE_ANNOTATION: &str = "measure_line_annotation";

// Order here matches the contract schema's enumeration order — the test below
// pins this down. Don't reorder casually.
pub const SUPPORTED_COMMANDS: [&str; 9] = [
    COMMAND_GET_PROCESSING_MANIFEST,
    COMMAND_OPEN_STUDY,
    COMMAND_START_RENDER_JOB,
    COMMAND_START_ANALYZE_JOB,
    COMMAND_START_PROCESS_JOB,
    COMMAND_GET_JOB,
    COMMAND_GET_JOBS,
    COMMAND_CANCEL_JOB,
    COMMAND_MEASURE_LINE_ANNOTATION,
];

// Pseudocolor palette identifier on the wire. Note this is the *contract*
// enum — `processing::Palette` is the internal representation.
// Mirror in contracts.ts as a string union.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum PaletteName {
    None,
    Hot,
    Bone,
}

// Snapshot of every adjustable processing knob. Used inside a preset and as
// the resolved set after merging user input.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessingControls {
    pub brightness: i32,
    pub contrast: f64,
    pub invert: bool,
    pub equalize: bool,
    pub palette: PaletteName,
}

// A named preset (e.g. "default", "xray"). The `id` is the wire identifier
// the frontend sends in ProcessStudyCommand.preset_id.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessingPreset {
    pub id: String,
    pub controls: ProcessingControls,
}

// The full menu of presets the backend offers, plus which one is the default.
// Fetched by the frontend at startup and re-fetched only on backend version
// change.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessingManifest {
    pub default_preset_id: String,
    pub presets: Vec<ProcessingPreset>,
}

// Hardcoded preset list. If you change the numbers here, update the test
// `default_processing_manifest_matches_contract_payload` below to match.
// "xray" is the curated bitewing default — brightness +10 lifts low-mid
// values, contrast 1.4 sharpens the bone/tissue boundary, equalize on,
// bone palette tints toward the warm/cool spectrum clinicians prefer.
pub fn default_processing_manifest() -> ProcessingManifest {
    ProcessingManifest {
        default_preset_id: "default".to_string(),
        presets: vec![
            // Identity preset — applies nothing. Useful as a control / reset.
            ProcessingPreset {
                id: "default".to_string(),
                controls: ProcessingControls {
                    brightness: 0,
                    contrast: 1.0,
                    invert: false,
                    equalize: false,
                    palette: PaletteName::None,
                },
            },
            // The opinionated bitewing preset.
            ProcessingPreset {
                id: "xray".to_string(),
                controls: ProcessingControls {
                    brightness: 10,
                    contrast: 1.4,
                    invert: false,
                    equalize: true,
                    palette: PaletteName::Bone,
                },
            },
            // For dark/underexposed images — push contrast hard, no palette.
            ProcessingPreset {
                id: "high-contrast".to_string(),
                controls: ProcessingControls {
                    brightness: 0,
                    contrast: 1.8,
                    invert: false,
                    equalize: true,
                    palette: PaletteName::None,
                },
            },
        ],
    }
}

// Per-axis pixel spacing in millimeters. Bitewings can have non-square pixels
// (row vs column spacing differ), so we keep them separate. `source` records
// where calibration came from so the UI can show it traceably.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MeasurementScale {
    pub row_spacing_mm: f64,
    pub column_spacing_mm: f64,
    pub source: String,
}

// Currently only one source ("Manual" = user drew it). Adding ML-sourced
// annotations would extend this enum — and likely flip `editable` to false.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum AnnotationSource {
    Manual,
}

// f64 coords because the frontend draws fractional positions when zoomed.
#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnnotationPoint {
    pub x: f64,
    pub y: f64,
}

// Result of a line measurement. pixel_length always present; mm only if a
// scale was available. skip_serializing_if keeps the JSON tidy when there's
// no calibration.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct LineMeasurement {
    pub pixel_length: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub calibrated_length_mm: Option<f64>,
}

// User-drawn line. `confidence` is reserved for the day we add ML-suggested
// annotations; today it's always None for manual lines.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct LineAnnotation {
    pub id: String,
    pub label: String,
    pub source: AnnotationSource,
    pub start: AnnotationPoint,
    pub end: AnnotationPoint,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement: Option<LineMeasurement>,
}

// Axis-aligned rectangle annotation. Width/height are in image pixels.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct RectangleAnnotation {
    pub id: String,
    pub label: String,
    pub source: AnnotationSource,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
}

// Open or closed polyline. `closed=true` means the last point connects back
// to the first (a polygon outline).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct PolylineAnnotation {
    pub id: String,
    pub label: String,
    pub source: AnnotationSource,
    pub points: Vec<AnnotationPoint>,
    pub closed: bool,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
}

// All annotations for a single study, separated by shape. Bundled this way
// (rather than a single tagged enum) so the frontend can render each type
// with a dedicated component without a discriminant switch.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnnotationBundle {
    pub lines: Vec<LineAnnotation>,
    pub rectangles: Vec<RectangleAnnotation>,
    pub polylines: Vec<PolylineAnnotation>,
}

// Coarse error taxonomy. The frontend uses these to decide on UI treatment
// (toast vs modal vs silent retry). Keep them small and add a new variant
// rather than overloading an existing one.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum BackendErrorCode {
    // User input we won't process — e.g. bad preset id, out-of-range brightness.
    InvalidInput,
    // Specific resource missing — usually a study_id or job_id.
    NotFound,
    // Caller invoked cancel on a still-running job and we honored it.
    Cancelled,
    // State conflict — e.g. two start_*_job calls racing for the same fingerprint.
    Conflict,
    // Cache file on disk is unreadable; user can usually just retry.
    CacheCorrupted,
    // Programmer bug or unrecoverable I/O. `recoverable` is forced to false.
    Internal,
}

// The error payload the frontend sees. `recoverable` lets the UI offer a
// retry button only when it makes sense.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct BackendError {
    pub code: BackendErrorCode,
    pub message: String,
    // Optional extra context lines (file paths, header values, etc).
    // Empty vec serializes away to keep payloads small.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub details: Vec<String>,
    pub recoverable: bool,
}

impl BackendError {
    // Rule of thumb: Internal is always non-recoverable, everything else is.
    // If you ever invent a Code variant that breaks that, the rule needs to
    // become a match.
    pub fn new(code: BackendErrorCode, message: impl Into<String>) -> Self {
        let recoverable = code != BackendErrorCode::Internal;
        Self {
            code,
            message: message.into(),
            details: Vec::new(),
            recoverable,
        }
    }

    // The most common error type — almost every command can produce these.
    pub fn invalid_input(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::InvalidInput, message)
    }

    pub fn not_found(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::NotFound, message)
    }

    pub fn internal(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::Internal, message)
    }
}

// Display just emits the message — useful for log lines and `?` propagation
// when the caller doesn't care about the code.
impl std::fmt::Display for BackendError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(&self.message)
    }
}

impl std::error::Error for BackendError {}

// open_study payloads — input_path is filesystem path on disk (absolute).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct OpenStudyCommand {
    pub input_path: String,
}

// A study's identity in this session. study_id is the stable key the frontend
// uses for every subsequent command. input_name is the basename for display.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct StudyRecord {
    pub study_id: String,
    pub input_path: String,
    pub input_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct OpenStudyCommandResult {
    pub study: StudyRecord,
}

// Three command pairs follow — Render, Analyze, Process. All take study_id;
// results all include preview_path (the cache-resident BMP they produced) +
// loaded dimensions + the measurement scale (so the frontend can label
// annotations without re-fetching).

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct RenderStudyCommand {
    pub study_id: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct RenderStudyCommandResult {
    pub study_id: String,
    pub preview_path: String,
    pub loaded_width: u32,
    pub loaded_height: u32,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnalyzeStudyCommand {
    pub study_id: String,
}

// Analyze produces two outputs — the raw analyzed preview and a "filled"
// version with overlay shading applied. Frontend can toggle between them.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnalyzeStudyCommandResult {
    pub study_id: String,
    pub preview_path: String,
    pub filled_preview_path: String,
    pub loaded_width: u32,
    pub loaded_height: u32,
    // Human-readable description of what analysis was done — surfaces in UI.
    pub mode: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

// Process is the heavy one — preset id + per-knob overrides. None on a knob
// means "use the preset value", Some means "override it". invert/equalize/
// compare are always sent (bool, no override semantics needed).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessStudyCommand {
    pub study_id: String,
    pub preset_id: String,
    pub invert: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub brightness: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub contrast: Option<f64>,
    pub equalize: bool,
    pub compare: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub palette: Option<PaletteName>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessStudyCommandResult {
    pub study_id: String,
    pub preview_path: String,
    pub loaded_width: u32,
    pub loaded_height: u32,
    pub mode: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

// Synchronous (non-job) command + result for line measurement. The annotation
// goes in and comes back with its `measurement` field populated.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MeasureLineAnnotationCommand {
    pub study_id: String,
    pub annotation: LineAnnotation,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MeasureLineAnnotationCommandResult {
    pub study_id: String,
    pub annotation: LineAnnotation,
}

// A reference segment of known physical length, used to derive a study's pixel
// spacing. BMP carries no DICOM-style pixel-spacing metadata, so calibration is
// manual: the user draws a line across a feature of known size (a periodontal
// probe, a calibration marker) and supplies its real-world length in mm. The
// pixel length is recomputed server-side from start/end so the measurement math
// stays in one place (`annotations::measure_line`).
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct CalibrationReference {
    pub start: AnnotationPoint,
    pub end: AnnotationPoint,
    pub known_length_mm: f64,
}

// set_study_calibration payload. `reference: None` clears any existing
// calibration; `Some` derives an isotropic MeasurementScale from the segment.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct SetStudyCalibrationCommand {
    pub study_id: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub reference: Option<CalibrationReference>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct SetStudyCalibrationCommandResult {
    pub study: StudyRecord,
}

// Generic single-job lookup/cancel payload.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobCommand {
    pub job_id: String,
}

// Bulk lookup — the UI refreshes its jobs panel using this rather than N
// get_job calls.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct GetJobsCommand {
    pub job_ids: Vec<String>,
}

// Tagged-by-string job discriminator. Mirrors the three start_*_job commands.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum JobKind {
    RenderStudy,
    AnalyzeStudy,
    ProcessStudy,
}

// Job state machine. Legal transitions:
//   Queued → Running → Completed | Failed
//   Running → Cancelling → Cancelled
//   Queued → Cancelled (if cancelled before start)
// "Cancelling" exists as a distinct state because cancellation is cooperative —
// the worker may still be unwinding when the UI asks for status.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum JobState {
    Queued,
    Running,
    Cancelling,
    Completed,
    Failed,
    Cancelled,
}

// Progress payload published on every stage transition. `stage` is a short
// machine token ("decoding", "rendering", "writing"), `message` is the
// human-readable companion shown in the UI.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobProgress {
    pub percent: i32,
    pub stage: String,
    pub message: String,
}

// Returned synchronously from start_*_job — just the id so the frontend can
// start subscribing to updates immediately.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct StartedJob {
    pub job_id: String,
}

// Job result payload. The shape is union-of-three (RenderStudyCommandResult,
// AnalyzeStudyCommandResult, ProcessStudyCommandResult) so we keep it as
// untyped JSON here and let the frontend narrow on `kind`. Keeps this enum
// from ballooning with every new job type.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobResult {
    pub kind: JobKind,
    pub payload: serde_json::Value,
}

// What `get_job` / the job-update event sends. Arc on result because the same
// snapshot gets fanned out to every subscriber and we don't want to clone
// the (potentially large) payload N times.
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobSnapshot {
    pub job_id: String,
    pub job_kind: JobKind,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub study_id: Option<String>,
    pub state: JobState,
    pub progress: JobProgress,
    // True when the result was served from cache — frontend uses this to
    // suppress the "rendering..." spinner for already-known results.
    pub from_cache: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<Arc<JobResult>>,
    // Mutually exclusive with `result`. State machine enforces that.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<BackendError>,
}

#[cfg(test)]
mod tests {
    use super::*;

    // Locks down both the *order* and *names* of commands. Re-ordering would
    // break the schema enum's positional check; renaming would break the
    // frontend's literal invoke() calls.
    #[test]
    fn supported_commands_match_contract_order() {
        assert_eq!(
            SUPPORTED_COMMANDS,
            [
                "get_processing_manifest",
                "open_study",
                "start_render_job",
                "start_analyze_job",
                "start_process_job",
                "get_job",
                "get_jobs",
                "cancel_job",
                "measure_line_annotation",
            ]
        );
    }

    // Serialize the manifest and check it byte-for-byte against the expected
    // JSON. This is the integration boundary with the TS-generated types —
    // if this drifts, contracts:check will catch it on the schema side too,
    // but this test fires earlier (during cargo test, no node needed).
    #[test]
    fn default_processing_manifest_matches_contract_payload() {
        let payload = serde_json::to_value(default_processing_manifest()).unwrap();
        let expected = serde_json::json!({
            "defaultPresetId": "default",
            "presets": [
                {
                    "id": "default",
                    "controls": {
                        "brightness": 0,
                        "contrast": 1.0,
                        "invert": false,
                        "equalize": false,
                        "palette": "none"
                    }
                },
                {
                    "id": "xray",
                    "controls": {
                        "brightness": 10,
                        "contrast": 1.4,
                        "invert": false,
                        "equalize": true,
                        "palette": "bone"
                    }
                },
                {
                    "id": "high-contrast",
                    "controls": {
                        "brightness": 0,
                        "contrast": 1.8,
                        "invert": false,
                        "equalize": true,
                        "palette": "none"
                    }
                }
            ]
        });

        assert_eq!(payload, expected);
    }
}
