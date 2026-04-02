import type {
  MeasurementScale,
  ProcessingControls,
  ProcessingManifest,
  ProcessingPipelineStep,
  ToothAnalysis,
} from "../../lib/generated/contracts";
import type {
  OpenedStudy,
  PreviewResult,
  ProcessResult,
  RuntimeMode,
} from "../../lib/types";
import type { JobSnapshot, ProcessingRunState } from "../jobs/model";

export const DEFAULT_PIPELINE: ProcessingPipelineStep[] = [
  "grayscale",
  "invert",
  "brightness",
  "contrast",
  "equalize",
];

export interface ProcessingForm {
  controls: ProcessingControls;
  outputPath: string | null;
  compare: boolean;
  pipeline: ProcessingPipelineStep[];
}

export interface ProcessingSession {
  form: ProcessingForm;
  output: ProcessResult | null;
  runStatus: ProcessingRunState;
}

export interface WorkbenchStudy {
  studyId: string;
  inputPath: string;
  inputName: string;
  measurementScale: MeasurementScale | null;
  originalPreview: PreviewResult | null;
  analysis: ToothAnalysis | null;
  processing: ProcessingSession;
  runtime: RuntimeMode;
  status: string;
  renderJobId: string | null;
  analysisJobId: string | null;
}

export interface WorkbenchState {
  manifest: ProcessingManifest;
  manifestStatus: "idle" | "loading" | "ready" | "error";
  activeStudyId: string | null;
  studies: Record<string, WorkbenchStudy>;
  studyOrder: string[];
  jobs: Record<string, JobSnapshot>;
  jobOrder: string[];
  isOpeningStudy: boolean;
  workbenchStatus: string;
}

export function createProcessingForm(
  defaultControls: ProcessingControls,
): ProcessingForm {
  return {
    controls: { ...defaultControls },
    outputPath: null,
    compare: false,
    pipeline: [...DEFAULT_PIPELINE],
  };
}

export function defaultControlsForManifest(
  manifest: ProcessingManifest,
): ProcessingControls {
  const defaultPreset =
    manifest.presets.find((preset) => preset.id === manifest.defaultPresetId) ??
    manifest.presets[0];

  return defaultPreset
    ? { ...defaultPreset.controls }
    : {
        brightness: 0,
        contrast: 1,
        invert: false,
        equalize: false,
        palette: "none",
      };
}

export function createWorkbenchStudy(
  study: OpenedStudy,
  defaultControls: ProcessingControls,
): WorkbenchStudy {
  return {
    studyId: study.studyId,
    inputPath: study.inputPath,
    inputName: study.inputName,
    measurementScale: study.measurementScale,
    originalPreview: null,
    analysis: null,
    processing: {
      form: createProcessingForm(defaultControls),
      output: null,
      runStatus: { state: "idle" },
    },
    runtime: study.runtime,
    status: "Study opened. Rendering source preview...",
    renderJobId: null,
    analysisJobId: null,
  };
}
