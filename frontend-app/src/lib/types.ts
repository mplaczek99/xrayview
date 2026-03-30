export type Palette = "none" | "hot" | "bone";
export type ViewerMode = "original" | "processed" | "compare";
export type RuntimeMode = "tauri" | "mock";

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
  processedDicomPath: string | null;
  savedDestination: string | null;
  status: string;
  dirty: boolean;
  runtime: RuntimeMode;
}

export interface ProcessingPreset {
  label: string;
  description: string;
  controls: ProcessingControls;
}

export interface PreviewResult {
  previewUrl: string;
  runtime: RuntimeMode;
}

export interface ProcessResult extends PreviewResult {
  dicomPath: string;
}
