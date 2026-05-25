use serde::{Deserialize, Serialize};
use std::sync::Arc;

pub const SERVICE_NAME: &str = "xrayview-backend";
pub const BACKEND_CONTRACT_VERSION: u32 = 2;
pub const BACKEND_CONTRACT_SCHEMA_ID: &str =
    "https://xrayview.local/contracts/backend-contract-v1.schema.json";

pub const COMMAND_GET_PROCESSING_MANIFEST: &str = "get_processing_manifest";
pub const COMMAND_OPEN_STUDY: &str = "open_study";
pub const COMMAND_START_RENDER_JOB: &str = "start_render_job";
pub const COMMAND_START_ANALYZE_JOB: &str = "start_analyze_job";
pub const COMMAND_START_PROCESS_JOB: &str = "start_process_job";
pub const COMMAND_GET_JOB: &str = "get_job";
pub const COMMAND_GET_JOBS: &str = "get_jobs";
pub const COMMAND_CANCEL_JOB: &str = "cancel_job";
pub const COMMAND_MEASURE_LINE_ANNOTATION: &str = "measure_line_annotation";

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

pub fn is_supported_command(command: &str) -> bool {
    SUPPORTED_COMMANDS.contains(&command)
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum PaletteName {
    None,
    Hot,
    Bone,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessingControls {
    pub brightness: i32,
    pub contrast: f64,
    pub invert: bool,
    pub equalize: bool,
    pub palette: PaletteName,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessingPreset {
    pub id: String,
    pub controls: ProcessingControls,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct ProcessingManifest {
    pub default_preset_id: String,
    pub presets: Vec<ProcessingPreset>,
}

pub fn default_processing_manifest() -> ProcessingManifest {
    ProcessingManifest {
        default_preset_id: "default".to_string(),
        presets: vec![
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

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct MeasurementScale {
    pub row_spacing_mm: f64,
    pub column_spacing_mm: f64,
    pub source: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum AnnotationSource {
    Manual,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnnotationPoint {
    pub x: f64,
    pub y: f64,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct LineMeasurement {
    pub pixel_length: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub calibrated_length_mm: Option<f64>,
}

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

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnnotationBundle {
    pub lines: Vec<LineAnnotation>,
    pub rectangles: Vec<RectangleAnnotation>,
    pub polylines: Vec<PolylineAnnotation>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum BackendErrorCode {
    InvalidInput,
    NotFound,
    Cancelled,
    Conflict,
    CacheCorrupted,
    Internal,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct BackendError {
    pub code: BackendErrorCode,
    pub message: String,
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub details: Vec<String>,
    pub recoverable: bool,
}

impl BackendError {
    pub fn new(code: BackendErrorCode, message: impl Into<String>) -> Self {
        let recoverable = code != BackendErrorCode::Internal;
        Self {
            code,
            message: message.into(),
            details: Vec::new(),
            recoverable,
        }
    }

    pub fn invalid_input(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::InvalidInput, message)
    }

    pub fn not_found(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::NotFound, message)
    }

    pub fn internal(message: impl Into<String>) -> Self {
        Self::new(BackendErrorCode::Internal, message)
    }

    pub fn with_details<I, S>(mut self, details: I) -> Self
    where
        I: IntoIterator<Item = S>,
        S: Into<String>,
    {
        self.details = details.into_iter().map(Into::into).collect();
        self
    }
}

impl std::fmt::Display for BackendError {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(&self.message)
    }
}

impl std::error::Error for BackendError {}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct OpenStudyCommand {
    pub input_path: String,
}

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

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct AnalyzeStudyCommandResult {
    pub study_id: String,
    pub preview_path: String,
    pub filled_preview_path: String,
    pub loaded_width: u32,
    pub loaded_height: u32,
    pub mode: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

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

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobCommand {
    pub job_id: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct GetJobsCommand {
    pub job_ids: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum JobKind {
    RenderStudy,
    AnalyzeStudy,
    ProcessStudy,
}

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

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobProgress {
    pub percent: i32,
    pub stage: String,
    pub message: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct StartedJob {
    pub job_id: String,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobResult {
    pub kind: JobKind,
    pub payload: serde_json::Value,
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase", deny_unknown_fields)]
pub struct JobSnapshot {
    pub job_id: String,
    pub job_kind: JobKind,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub study_id: Option<String>,
    pub state: JobState,
    pub progress: JobProgress,
    pub from_cache: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<Arc<JobResult>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<BackendError>,
}

#[cfg(test)]
mod tests {
    use super::*;

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
