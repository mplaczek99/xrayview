import type {
  MeasurementScale,
  PaletteName,
  ProcessingControls,
} from "./generated/contracts";

export type Palette = PaletteName;
export type RuntimeMode = "mock" | "desktop";
export type ActiveTab = "view" | "processing";

export interface ProcessingPresetOption {
  id: string;
  label: string;
  description: string;
  controls: ProcessingControls;
}

export interface OpenedStudy {
  studyId: string;
  inputPath: string;
  inputName: string;
  measurementScale: MeasurementScale | null;
  runtime: RuntimeMode;
}

export interface ProcessingRequest {
  controls: ProcessingControls;
  compare: boolean;
  outputPath: string | null;
  presetId: string;
  presetControls: ProcessingControls;
}

export interface PreviewResult {
  studyId: string;
  previewUrl: string;
  imageSize: { width: number; height: number };
  measurementScale: MeasurementScale | null;
  runtime: RuntimeMode;
}

export interface ProcessResult extends PreviewResult {
  mode: string;
}

export interface AnalysisResult extends PreviewResult {
  filledPreviewUrl: string;
  mode: string;
}
