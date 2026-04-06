use std::path::PathBuf;

use serde::{Deserialize, Serialize};

use crate::ToothAnalysis;
use crate::annotations::{
    AnnotationBundle, AnnotationPoint, AnnotationSource, LineAnnotation, LineMeasurement,
    RectangleAnnotation,
};
use crate::error::BackendError;

pub const BACKEND_CONTRACT_VERSION: u32 = 1;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum PaletteName {
    None,
    Hot,
    Bone,
}

impl PaletteName {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::None => "none",
            Self::Hot => "hot",
            Self::Bone => "bone",
        }
    }
}

#[derive(Debug, Clone, Copy, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessingControls {
    pub brightness: i32,
    pub contrast: f64,
    pub invert: bool,
    pub equalize: bool,
    pub palette: PaletteName,
}

#[derive(Debug, Clone, Copy, Serialize)]
pub struct ProcessingPreset {
    pub id: &'static str,
    pub controls: ProcessingControls,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessingManifest {
    pub default_preset_id: &'static str,
    pub presets: Vec<ProcessingPreset>,
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct MeasurementScale {
    pub row_spacing_mm: f64,
    pub column_spacing_mm: f64,
    pub source: &'static str,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum AnnotationSourceDto {
    Manual,
    AutoTooth,
}

impl From<AnnotationSource> for AnnotationSourceDto {
    fn from(value: AnnotationSource) -> Self {
        match value {
            AnnotationSource::Manual => Self::Manual,
            AnnotationSource::AutoTooth => Self::AutoTooth,
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnotationPointDto {
    pub x: f64,
    pub y: f64,
}

impl From<AnnotationPoint> for AnnotationPointDto {
    fn from(value: AnnotationPoint) -> Self {
        Self {
            x: value.x,
            y: value.y,
        }
    }
}

impl From<AnnotationPointDto> for AnnotationPoint {
    fn from(value: AnnotationPointDto) -> Self {
        Self {
            x: value.x,
            y: value.y,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LineMeasurementDto {
    pub pixel_length: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub calibrated_length_mm: Option<f64>,
}

impl From<LineMeasurement> for LineMeasurementDto {
    fn from(value: LineMeasurement) -> Self {
        Self {
            pixel_length: value.pixel_length,
            calibrated_length_mm: value.calibrated_length_mm,
        }
    }
}

impl From<LineMeasurementDto> for LineMeasurement {
    fn from(value: LineMeasurementDto) -> Self {
        Self {
            pixel_length: value.pixel_length,
            calibrated_length_mm: value.calibrated_length_mm,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct LineAnnotationDto {
    pub id: String,
    pub label: String,
    pub source: AnnotationSourceDto,
    pub start: AnnotationPointDto,
    pub end: AnnotationPointDto,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement: Option<LineMeasurementDto>,
}

impl From<LineAnnotation> for LineAnnotationDto {
    fn from(value: LineAnnotation) -> Self {
        Self {
            id: value.id,
            label: value.label,
            source: value.source.into(),
            start: value.start.into(),
            end: value.end.into(),
            editable: value.editable,
            confidence: value.confidence,
            measurement: value.measurement.map(Into::into),
        }
    }
}

impl From<LineAnnotationDto> for LineAnnotation {
    fn from(value: LineAnnotationDto) -> Self {
        Self {
            id: value.id,
            label: value.label,
            source: match value.source {
                AnnotationSourceDto::Manual => AnnotationSource::Manual,
                AnnotationSourceDto::AutoTooth => AnnotationSource::AutoTooth,
            },
            start: value.start.into(),
            end: value.end.into(),
            editable: value.editable,
            confidence: value.confidence,
            measurement: value.measurement.map(Into::into),
        }
    }
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RectangleAnnotationDto {
    pub id: String,
    pub label: String,
    pub source: AnnotationSourceDto,
    pub x: f64,
    pub y: f64,
    pub width: f64,
    pub height: f64,
    pub editable: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub confidence: Option<f64>,
}

impl From<RectangleAnnotation> for RectangleAnnotationDto {
    fn from(value: RectangleAnnotation) -> Self {
        Self {
            id: value.id,
            label: value.label,
            source: value.source.into(),
            x: value.x,
            y: value.y,
            width: value.width,
            height: value.height,
            editable: value.editable,
            confidence: value.confidence,
        }
    }
}

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnnotationBundleDto {
    pub lines: Vec<LineAnnotationDto>,
    pub rectangles: Vec<RectangleAnnotationDto>,
}

impl From<AnnotationBundle> for AnnotationBundleDto {
    fn from(value: AnnotationBundle) -> Self {
        Self {
            lines: value.lines.into_iter().map(Into::into).collect(),
            rectangles: value.rectangles.into_iter().map(Into::into).collect(),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StudyDescription {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StudyRecord {
    pub study_id: String,
    pub input_path: PathBuf,
    pub input_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum JobKind {
    RenderStudy,
    ProcessStudy,
    AnalyzeStudy,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum JobState {
    Queued,
    Running,
    Cancelling,
    Completed,
    Failed,
    Cancelled,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JobProgress {
    pub percent: u8,
    pub stage: String,
    pub message: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StartedJob {
    pub job_id: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct JobCommand {
    pub job_id: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DescribeStudyCommand {
    pub input_path: PathBuf,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct OpenStudyCommand {
    pub input_path: PathBuf,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct OpenStudyCommandResult {
    pub study: StudyRecord,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RenderStudyCommand {
    pub study_id: String,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct RenderStudyCommandResult {
    pub study_id: String,
    pub preview_path: PathBuf,
    pub loaded_width: u32,
    pub loaded_height: u32,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessStudyCommand {
    pub study_id: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_path: Option<PathBuf>,
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

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessStudyCommandResult {
    pub study_id: String,
    pub preview_path: PathBuf,
    pub dicom_path: PathBuf,
    pub loaded_width: u32,
    pub loaded_height: u32,
    pub mode: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AnalyzeStudyCommand {
    pub study_id: String,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct MeasureLineAnnotationCommand {
    pub study_id: String,
    pub annotation: LineAnnotationDto,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AnalyzeStudyCommandResult {
    pub study_id: String,
    pub preview_path: PathBuf,
    pub analysis: ToothAnalysis,
    pub suggested_annotations: AnnotationBundleDto,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct MeasureLineAnnotationCommandResult {
    pub study_id: String,
    pub annotation: LineAnnotationDto,
}

#[derive(Debug, Clone, Serialize)]
#[serde(tag = "kind", content = "payload", rename_all = "camelCase")]
pub enum JobResult {
    RenderStudy(RenderStudyCommandResult),
    ProcessStudy(ProcessStudyCommandResult),
    AnalyzeStudy(AnalyzeStudyCommandResult),
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct JobSnapshot {
    pub job_id: String,
    pub job_kind: JobKind,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub study_id: Option<String>,
    pub state: JobState,
    pub progress: JobProgress,
    pub from_cache: bool,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub result: Option<JobResult>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<BackendError>,
}
