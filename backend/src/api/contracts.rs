use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::ToothAnalysis;
use crate::annotations::{
    AnnotationBundle, AnnotationPoint, AnnotationSource, LineAnnotation, LineMeasurement,
    RectangleAnnotation,
};
use crate::error::BackendError;

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

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum ProcessingPipelineStep {
    Grayscale,
    Invert,
    Brightness,
    Contrast,
    Equalize,
}

impl ProcessingPipelineStep {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Grayscale => "grayscale",
            Self::Invert => "invert",
            Self::Brightness => "brightness",
            Self::Contrast => "contrast",
            Self::Equalize => "equalize",
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
    pub pipeline: Option<Vec<ProcessingPipelineStep>>,
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

pub fn generated_typescript_contracts() -> String {
    String::from(
        r#"// This file is generated from `backend/src/api/contracts.rs`.
// Run `npm --prefix frontend run generate:contracts` after changing Rust contracts.

export type PaletteName = "none" | "hot" | "bone";

export type ProcessingPipelineStep =
  | "grayscale"
  | "invert"
  | "brightness"
  | "contrast"
  | "equalize";

export interface ProcessingControls {
  brightness: number;
  contrast: number;
  invert: boolean;
  equalize: boolean;
  palette: PaletteName;
}

export interface ProcessingPreset {
  id: string;
  controls: ProcessingControls;
}

export interface ProcessingManifest {
  defaultPresetId: string;
  presets: ProcessingPreset[];
}

export interface MeasurementScale {
  rowSpacingMm: number;
  columnSpacingMm: number;
  source: string;
}

export type AnnotationSource = "manual" | "autoTooth";

export interface AnnotationPoint {
  x: number;
  y: number;
}

export interface LineMeasurement {
  pixelLength: number;
  calibratedLengthMm?: number | null;
}

export interface LineAnnotation {
  id: string;
  label: string;
  source: AnnotationSource;
  start: AnnotationPoint;
  end: AnnotationPoint;
  editable: boolean;
  confidence?: number | null;
  measurement?: LineMeasurement | null;
}

export interface RectangleAnnotation {
  id: string;
  label: string;
  source: AnnotationSource;
  x: number;
  y: number;
  width: number;
  height: number;
  editable: boolean;
  confidence?: number | null;
}

export interface AnnotationBundle {
  lines: LineAnnotation[];
  rectangles: RectangleAnnotation[];
}

export type BackendErrorCode =
  | "invalidInput"
  | "notFound"
  | "cancelled"
  | "conflict"
  | "cacheCorrupted"
  | "internal";

export interface BackendError {
  code: BackendErrorCode;
  message: string;
  details: string[];
  recoverable: boolean;
}

export type JobKind = "renderStudy" | "processStudy" | "analyzeStudy";

export type JobState =
  | "queued"
  | "running"
  | "cancelling"
  | "completed"
  | "failed"
  | "cancelled";

export interface JobProgress {
  percent: number;
  stage: string;
  message: string;
}

export interface StartedJob {
  jobId: string;
}

export interface JobCommand {
  jobId: string;
}

export interface OpenStudyCommand {
  inputPath: string;
}

export interface StudyRecord {
  studyId: string;
  inputPath: string;
  inputName: string;
  measurementScale?: MeasurementScale | null;
}

export interface OpenStudyCommandResult {
  study: StudyRecord;
}

export interface RenderStudyCommand {
  studyId: string;
}

export interface RenderStudyCommandResult {
  studyId: string;
  previewPath: string;
  loadedWidth: number;
  loadedHeight: number;
  measurementScale?: MeasurementScale | null;
}

export interface ProcessStudyCommand {
  studyId: string;
  outputPath?: string | null;
  presetId: string;
  invert: boolean;
  brightness?: number | null;
  contrast?: number | null;
  equalize: boolean;
  compare: boolean;
  pipeline?: ProcessingPipelineStep[] | null;
  palette?: PaletteName | null;
}

export interface ProcessStudyCommandResult {
  studyId: string;
  previewPath: string;
  dicomPath: string;
  loadedWidth: number;
  loadedHeight: number;
  mode: string;
  measurementScale?: MeasurementScale | null;
}

export interface AnalyzeStudyCommand {
  studyId: string;
}

export interface MeasureLineAnnotationCommand {
  studyId: string;
  annotation: LineAnnotation;
}

export interface ToothAnalysis {
  image: ToothImageMetadata;
  calibration: ToothCalibration;
  tooth?: ToothCandidate | null;
  warnings: string[];
}

export interface ToothImageMetadata {
  width: number;
  height: number;
}

export interface ToothCalibration {
  pixelUnits: string;
  measurementScale?: MeasurementScale | null;
  realWorldMeasurementsAvailable: boolean;
}

export interface ToothCandidate {
  confidence: number;
  maskAreaPixels: number;
  measurements: ToothMeasurementBundle;
  geometry: ToothGeometry;
}

export interface ToothMeasurementBundle {
  pixel: ToothMeasurementValues;
  calibrated?: ToothMeasurementValues | null;
}

export interface ToothMeasurementValues {
  toothWidth: number;
  toothHeight: number;
  boundingBoxWidth: number;
  boundingBoxHeight: number;
  units: string;
}

export interface ToothGeometry {
  boundingBox: BoundingBox;
  widthLine: LineSegment;
  heightLine: LineSegment;
}

export interface BoundingBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface LineSegment {
  start: Point;
  end: Point;
}

export interface Point {
  x: number;
  y: number;
}

export interface AnalyzeStudyCommandResult {
  studyId: string;
  previewPath: string;
  analysis: ToothAnalysis;
  suggestedAnnotations: AnnotationBundle;
}

export interface MeasureLineAnnotationCommandResult {
  studyId: string;
  annotation: LineAnnotation;
}

export type JobResult =
  | { kind: "renderStudy"; payload: RenderStudyCommandResult }
  | { kind: "processStudy"; payload: ProcessStudyCommandResult }
  | { kind: "analyzeStudy"; payload: AnalyzeStudyCommandResult };

export interface JobSnapshot {
  jobId: string;
  jobKind: JobKind;
  studyId?: string | null;
  state: JobState;
  progress: JobProgress;
  fromCache: boolean;
  result?: JobResult | null;
  error?: BackendError | null;
}
"#,
    )
}

pub fn write_typescript_contracts(path: &Path) -> io::Result<()> {
    let contents = generated_typescript_contracts();
    if fs::read_to_string(path).ok().as_deref() == Some(contents.as_str()) {
        return Ok(());
    }

    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }

    fs::write(path, contents)
}

#[cfg(test)]
mod tests {
    use super::generated_typescript_contracts;

    #[test]
    fn generated_contracts_include_job_types() {
        let contracts = generated_typescript_contracts();

        assert!(contracts.contains("export interface ProcessStudyCommand {"));
        assert!(contracts.contains("export interface AnalyzeStudyCommandResult {"));
        assert!(contracts.contains("export interface MeasureLineAnnotationCommand {"));
        assert!(contracts.contains("export interface AnnotationBundle {"));
        assert!(contracts.contains("export interface JobSnapshot {"));
        assert!(contracts.contains("export type JobState ="));
        assert!(contracts.contains("export interface BackendError {"));
    }
}
