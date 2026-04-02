import type {
  MeasurementScale,
  PaletteName,
  ProcessingControls,
  ProcessingPipelineStep,
  ProcessingPreset,
  ToothAnalysis,
} from "./generated/contracts";

export type Palette = PaletteName;
export type ViewerMode = "original" | "processed" | "compare";
export type RuntimeMode = "tauri" | "mock";
export type ActiveTab = "view" | "processing";

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

export interface ProcessingPresetOption {
  id: string;
  label: string;
  description: string;
  controls: ProcessingControls;
}

export interface ProcessingRequest {
  controls: ProcessingControls;
  compare: boolean;
  outputPath: string | null;
  pipeline: ProcessingPipelineStep[];
  preset: ProcessingPreset;
}

export interface PreviewResult {
  previewUrl: string;
  measurementScale: MeasurementScale | null;
  runtime: RuntimeMode;
}

export interface ProcessResult extends PreviewResult {
  dicomPath: string;
}

export interface ToothAnalysisResult {
  previewUrl: string;
  analysis: ToothAnalysis;
  runtime: RuntimeMode;
}
