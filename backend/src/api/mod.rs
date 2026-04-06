pub mod contracts;

use std::path::PathBuf;

use serde::Serialize;

use crate::ToothAnalysis;

pub use contracts::{
    AnalyzeStudyCommand, AnalyzeStudyCommandResult, BACKEND_CONTRACT_VERSION, DescribeStudyCommand,
    JobCommand, JobKind, JobProgress, JobResult, JobSnapshot, JobState,
    MeasureLineAnnotationCommand, MeasureLineAnnotationCommandResult, MeasurementScale,
    OpenStudyCommand, OpenStudyCommandResult, PaletteName, ProcessStudyCommand,
    ProcessStudyCommandResult, ProcessingControls, ProcessingManifest, ProcessingPreset,
    RenderStudyCommand, RenderStudyCommandResult, StartedJob, StudyDescription, StudyRecord,
    generated_typescript_contracts, write_typescript_contracts,
};

#[derive(Debug, Clone)]
pub struct RenderPreviewRequest {
    pub input_path: PathBuf,
    pub preview_output: PathBuf,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RenderPreviewResult {
    pub loaded_width: u32,
    pub loaded_height: u32,
    pub preview_output: PathBuf,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone)]
pub struct ProcessStudyRequest {
    pub input_path: PathBuf,
    pub output_path: Option<PathBuf>,
    pub preview_output: Option<PathBuf>,
    pub preset: String,
    pub invert: bool,
    pub brightness: Option<i32>,
    pub contrast: Option<f64>,
    pub equalize: bool,
    pub compare: bool,
    pub palette: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessStudyResult {
    pub loaded_width: u32,
    pub loaded_height: u32,
    pub mode: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub preview_output: Option<PathBuf>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_path: Option<PathBuf>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone)]
pub struct AnalyzeStudyRequest {
    pub input_path: PathBuf,
    pub preview_output: Option<PathBuf>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AnalyzeStudyResult {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub preview_output: Option<PathBuf>,
    pub analysis: ToothAnalysis,
}
