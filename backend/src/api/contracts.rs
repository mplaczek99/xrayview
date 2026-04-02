use std::fs;
use std::io;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

use crate::ToothAnalysis;

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

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct StudyDescription {
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct DescribeStudyCommand {
    pub input_path: PathBuf,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RenderPreviewCommand {
    pub input_path: PathBuf,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PreviewCommandResult {
    pub preview_path: PathBuf,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub measurement_scale: Option<MeasurementScale>,
}

#[derive(Debug, Clone, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct ProcessStudyCommand {
    pub input_path: PathBuf,
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
    pub input_path: PathBuf,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct AnalyzeStudyCommandResult {
    pub preview_path: PathBuf,
    pub analysis: ToothAnalysis,
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

export interface StudyDescription {
  measurementScale?: MeasurementScale | null;
}

export interface DescribeStudyCommand {
  inputPath: string;
}

export interface RenderPreviewCommand {
  inputPath: string;
}

export interface PreviewCommandResult {
  previewPath: string;
  measurementScale?: MeasurementScale | null;
}

export interface ProcessStudyCommand {
  inputPath: string;
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
  previewPath: string;
  dicomPath: string;
  loadedWidth: number;
  loadedHeight: number;
  mode: string;
  measurementScale?: MeasurementScale | null;
}

export interface AnalyzeStudyCommand {
  inputPath: string;
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
  previewPath: string;
  analysis: ToothAnalysis;
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
    fn generated_contracts_include_process_command() {
        let contracts = generated_typescript_contracts();

        assert!(contracts.contains("export interface ProcessStudyCommand {"));
        assert!(contracts.contains("export interface AnalyzeStudyCommandResult {"));
        assert!(contracts.contains("export type ProcessingPipelineStep ="));
    }
}
