export type Palette = "none" | "hot" | "bone";
export type ViewerMode = "original" | "processed" | "compare";
export type RuntimeMode = "tauri" | "mock";
export type ActiveTab = "view" | "processing";

export interface ProcessingControls {
  brightness: number;
  contrast: number;
  invert: boolean;
  equalize: boolean;
  palette: Palette;
}

export interface StudySession {
  inputPath: string | null;
  inputName: string;
  originalPreviewUrl: string | null;
  processedPreviewUrl: string | null;
  originalMeasurementScale: MeasurementScale | null;
  processedMeasurementScale: MeasurementScale | null;
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
