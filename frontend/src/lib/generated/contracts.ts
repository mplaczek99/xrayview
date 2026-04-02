// This file is generated from `backend/src/api/contracts.rs`.
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
