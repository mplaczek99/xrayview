export type Palette = "none" | "hot" | "bone";
export type ViewerMode = "original" | "processed" | "compare";
export type RuntimeMode = "tauri" | "mock";
export type ActiveTab = "view" | "processing";
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
  palette: Palette;
}

// Session state keeps both source and derived assets so the viewer and
// processing tabs can switch without re-fetching files.
export interface StudySession {
  inputPath: string | null;
  inputName: string;
  originalPreviewUrl: string | null;
  processedPreviewUrl: string | null;
  originalMeasurementScale: MeasurementScale | null;
  processedMeasurementScale: MeasurementScale | null;
  toothAnalysis: ToothAnalysis | null;
  processedDicomPath: string | null;
  savedDestination: string | null;
  status: string;
  dirty: boolean;
  runtime: RuntimeMode;
}

export interface ProcessingPreset {
  id: string;
  label: string;
  description: string;
  controls: ProcessingControls;
}

export interface ProcessingPresetDefinition {
  id: string;
  controls: ProcessingControls;
}

export interface ProcessingManifest {
  defaultPresetId: string;
  presets: ProcessingPresetDefinition[];
}

export interface ProcessingRequest {
  controls: ProcessingControls;
  compare: boolean;
  outputPath: string | null;
  pipeline: ProcessingPipelineStep[];
  // This is the preset baseline passed to the backend. When the current
  // controls no longer match any named preset, callers should fall back to the
  // backend default preset and send the remaining values as explicit flags.
  preset: ProcessingPresetDefinition;
}

export interface MeasurementScale {
  rowSpacingMm: number;
  columnSpacingMm: number;
  source: string;
}

export interface PreviewResult {
  previewUrl: string;
  measurementScale: MeasurementScale | null;
  runtime: RuntimeMode;
}

export interface ProcessResult extends PreviewResult {
  dicomPath: string;
}

export interface Point {
  x: number;
  y: number;
}

export interface LineSegment {
  start: Point;
  end: Point;
}

export interface BoundingBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ToothGeometry {
  boundingBox: BoundingBox;
  widthLine: LineSegment;
  heightLine: LineSegment;
}

export interface ToothMeasurementValues {
  toothWidth: number;
  toothHeight: number;
  boundingBoxWidth: number;
  boundingBoxHeight: number;
  units: string;
}

export interface ToothMeasurementBundle {
  pixel: ToothMeasurementValues;
  calibrated: ToothMeasurementValues | null;
}

export interface ToothCandidate {
  confidence: number;
  maskAreaPixels: number;
  measurements: ToothMeasurementBundle;
  geometry: ToothGeometry;
}

export interface ToothCalibration {
  pixelUnits: string;
  measurementScale: MeasurementScale | null;
  realWorldMeasurementsAvailable: boolean;
}

export interface ToothAnalysis {
  image: {
    width: number;
    height: number;
  };
  calibration: ToothCalibration;
  tooth: ToothCandidate | null;
  warnings: string[];
}

export interface ToothAnalysisResult {
  previewUrl: string;
  analysis: ToothAnalysis;
  runtime: RuntimeMode;
}
