import { convertFileSrc, invoke } from "@tauri-apps/api/core";
import { MOCK_PROCESSING_MANIFEST } from "./mockProcessingManifest";
import { createMockPreview, createMockToothAnalysis } from "./mockStudy";
import type {
  AnalyzeStudyCommand,
  AnalyzeStudyCommandResult,
  MeasurementScale,
  OpenStudyCommand,
  OpenStudyCommandResult,
  PaletteName,
  ProcessStudyCommand,
  ProcessStudyCommandResult,
  ProcessingManifest,
  ProcessingPipelineStep,
  RenderStudyCommand,
  RenderStudyCommandResult,
} from "./generated/contracts";
import type {
  OpenedStudy,
  Palette,
  PreviewResult,
  ProcessResult,
  ProcessingRequest,
  RuntimeMode,
  ToothAnalysisResult,
} from "./types";

const MOCK_STUDY_DIRECTORY = "mock-data";
const MOCK_EXPORT_DIRECTORY = "mock-exports";
const MOCK_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study.dcm`;
const MOCK_PROCESSED_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study_processed.dcm`;
const DEFAULT_PIPELINE: ProcessingPipelineStep[] = [
  "grayscale",
  "invert",
  "brightness",
  "contrast",
  "equalize",
];
const PALETTE_LABELS: Record<Palette, string> = {
  none: "Neutral",
  hot: "Hot",
  bone: "Bone",
};

let mockStudySequence = 0;

export const FALLBACK_PROCESSING_MANIFEST = MOCK_PROCESSING_MANIFEST;

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function getRuntimeMode(): RuntimeMode {
  return isTauriRuntime() ? "tauri" : "mock";
}

function buildMockPath(directory: string, fileName: string): string {
  return `${directory}/${fileName}`;
}

function fileNameFromPath(inputPath: string): string {
  return inputPath.split(/[\\/]/).pop() ?? inputPath;
}

function nextMockStudyId(): string {
  mockStudySequence += 1;
  return `mock-study-${mockStudySequence}`;
}

function pipelinesEqual(
  left: readonly ProcessingPipelineStep[],
  right: readonly ProcessingPipelineStep[],
): boolean {
  return left.length === right.length && left.every((step, index) => step === right[index]);
}

async function runInRuntime<T>(options: {
  mock: () => T | Promise<T>;
  tauri: () => Promise<T>;
}): Promise<T> {
  return isTauriRuntime() ? options.tauri() : options.mock();
}

function toPreviewUrl(previewPath: string, runtime: RuntimeMode): string {
  return runtime === "tauri" ? convertFileSrc(previewPath) : previewPath;
}

function asOpenedStudy(
  studyId: string,
  inputPath: string,
  inputName: string,
  runtime: RuntimeMode,
  measurementScale: MeasurementScale | null | undefined,
): OpenedStudy {
  return {
    studyId,
    inputPath,
    inputName,
    measurementScale: measurementScale ?? null,
    runtime,
  };
}

function asPreviewResult(
  studyId: string,
  previewPath: string,
  runtime: RuntimeMode,
  measurementScale: MeasurementScale | null | undefined,
): PreviewResult {
  return {
    studyId,
    previewUrl: toPreviewUrl(previewPath, runtime),
    measurementScale: measurementScale ?? null,
    runtime,
  };
}

function asProcessResult(
  studyId: string,
  previewPath: string,
  dicomPath: string,
  runtime: RuntimeMode,
  measurementScale: MeasurementScale | null | undefined,
  mode: string,
): ProcessResult {
  return {
    ...asPreviewResult(studyId, previewPath, runtime, measurementScale),
    dicomPath,
    mode,
  };
}

function buildProcessStudyCommand(
  studyId: string,
  request: ProcessingRequest,
): ProcessStudyCommand {
  return {
    studyId,
    outputPath: request.outputPath,
    presetId: request.preset.id,
    invert: request.controls.invert && !request.preset.controls.invert,
    brightness:
      request.controls.brightness !== request.preset.controls.brightness
        ? request.controls.brightness
        : null,
    contrast:
      request.controls.contrast !== request.preset.controls.contrast
        ? request.controls.contrast
        : null,
    equalize: request.controls.equalize && !request.preset.controls.equalize,
    compare: request.compare,
    pipeline: pipelinesEqual(request.pipeline, DEFAULT_PIPELINE) ? null : request.pipeline,
    palette:
      request.controls.palette !== request.preset.controls.palette
        ? request.controls.palette
        : null,
  };
}

export async function loadProcessingManifest(): Promise<ProcessingManifest> {
  return runInRuntime({
    mock: () => MOCK_PROCESSING_MANIFEST,
    tauri: () => invoke<ProcessingManifest>("get_processing_manifest"),
  });
}

export async function pickDicomFile(): Promise<string | null> {
  return runInRuntime({
    mock: () => MOCK_DICOM_PATH,
    tauri: () => invoke<string | null>("pick_dicom_file"),
  });
}

export async function pickSaveDicomPath(defaultName: string): Promise<string | null> {
  return runInRuntime({
    mock: () => buildMockPath(MOCK_EXPORT_DIRECTORY, defaultName),
    tauri: () => invoke<string | null>("pick_save_dicom_path", { defaultName }),
  });
}

export async function openStudy(inputPath: string): Promise<OpenedStudy> {
  return runInRuntime({
    mock: () =>
      asOpenedStudy(
        nextMockStudyId(),
        inputPath,
        fileNameFromPath(inputPath),
        getRuntimeMode(),
        null,
      ),
    tauri: async () => {
      const request: OpenStudyCommand = { inputPath };
      const payload = await invoke<OpenStudyCommandResult>("open_study", { request });
      return asOpenedStudy(
        payload.study.studyId,
        payload.study.inputPath,
        payload.study.inputName,
        getRuntimeMode(),
        payload.study.measurementScale,
      );
    },
  });
}

export async function renderStudy(studyId: string): Promise<PreviewResult> {
  return runInRuntime({
    mock: () =>
      asPreviewResult(studyId, createMockPreview(false, "none"), getRuntimeMode(), null),
    tauri: async () => {
      const request: RenderStudyCommand = { studyId };
      const payload = await invoke<RenderStudyCommandResult>("render_study", {
        request,
      });
      return asPreviewResult(
        payload.studyId,
        payload.previewPath,
        getRuntimeMode(),
        payload.measurementScale,
      );
    },
  });
}

export function buildProcessingArgs(
  inputPath: string,
  request: ProcessingRequest,
): string[] {
  const args = ["--input", inputPath, "--preset", request.preset.id];

  if (request.outputPath) {
    args.push("--output", request.outputPath);
  }

  if (request.controls.invert && !request.preset.controls.invert) {
    args.push("--invert");
  }
  if (request.controls.brightness !== request.preset.controls.brightness) {
    args.push("--brightness", String(request.controls.brightness));
  }
  if (request.controls.contrast !== request.preset.controls.contrast) {
    args.push("--contrast", String(request.controls.contrast));
  }
  if (request.controls.equalize && !request.preset.controls.equalize) {
    args.push("--equalize");
  }
  if (request.controls.palette !== request.preset.controls.palette) {
    args.push("--palette", request.controls.palette);
  }
  if (request.compare) {
    args.push("--compare");
  }
  if (!pipelinesEqual(request.pipeline, DEFAULT_PIPELINE)) {
    args.push("--pipeline", request.pipeline.join(","));
  }

  return args;
}

export async function processStudy(
  studyId: string,
  request: ProcessingRequest,
): Promise<ProcessResult> {
  const command = buildProcessStudyCommand(studyId, request);

  return runInRuntime({
    mock: () =>
      asProcessResult(
        studyId,
        createMockPreview(true, request.controls.palette),
        request.outputPath ?? MOCK_PROCESSED_DICOM_PATH,
        getRuntimeMode(),
        null,
        request.compare ? "comparison output" : "processed preview",
      ),
    tauri: async () => {
      const payload = await invoke<ProcessStudyCommandResult>("process_study", {
        request: command,
      });

      return asProcessResult(
        payload.studyId,
        payload.previewPath,
        payload.dicomPath,
        getRuntimeMode(),
        payload.measurementScale,
        payload.mode,
      );
    },
  });
}

export async function analyzeStudy(studyId: string): Promise<ToothAnalysisResult> {
  return runInRuntime({
    mock: () => ({
      studyId,
      previewUrl: createMockPreview(false, "none"),
      analysis: createMockToothAnalysis(),
      runtime: getRuntimeMode(),
    }),
    tauri: async () => {
      const request: AnalyzeStudyCommand = { studyId };
      const payload = await invoke<AnalyzeStudyCommandResult>("analyze_study", {
        request,
      });

      return {
        studyId: payload.studyId,
        previewUrl: toPreviewUrl(payload.previewPath, getRuntimeMode()),
        analysis: payload.analysis,
        runtime: getRuntimeMode(),
      };
    },
  });
}

export function ensureDicomExtension(path: string): string {
  return /\.(dcm|dicom)$/i.test(path) ? path : `${path}.dcm`;
}

export function buildOutputName(inputPath: string): string {
  const fileName = fileNameFromPath(inputPath) || "study.dcm";
  const baseName = fileName.replace(/\.(dcm|dicom)$/i, "");
  return `${baseName}_processed.dcm`;
}

export function paletteLabel(palette: PaletteName): string {
  return PALETTE_LABELS[palette];
}
