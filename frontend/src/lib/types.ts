import type {
  AnnotationBundle,
  MeasurementScale,
  PaletteName,
  ProcessingControls,
  ToothAnalysis,
} from "./generated/contracts";

export type Palette = PaletteName;
export type ViewerMode = "original" | "processed" | "compare";
export type RuntimeMode = "mock" | "legacy-rust" | "go-sidecar";
export type ShellMode = "mock" | "tauri";
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
  dicomPath: string;
  mode: string;
}

export interface ToothAnalysisResult {
  studyId: string;
  previewUrl: string;
  analysis: ToothAnalysis;
  suggestedAnnotations: AnnotationBundle;
  runtime: RuntimeMode;
}
