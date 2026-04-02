import { convertFileSrc, invoke } from "@tauri-apps/api/core";
import { MOCK_PROCESSING_MANIFEST } from "./mockProcessingManifest";
import { createMockPreview, createMockToothAnalysis } from "./mockStudy";
import type {
  MeasurementScale,
  Palette,
  PreviewResult,
  ProcessResult,
  ProcessingManifest,
  ProcessingPipelineStep,
  ProcessingRequest,
  RuntimeMode,
  ToothAnalysis,
  ToothAnalysisResult,
} from "./types";

interface PreviewPayload {
  previewPath: string;
  measurementScale: MeasurementScale | null;
}

interface ProcessPayload extends PreviewPayload {
  dicomPath: string;
}

interface ToothMeasurementPayload {
  previewPath: string;
  analysis: ToothAnalysis;
}

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
  // Keep the app code path identical in the browser mock and the packaged Tauri app.
  return isTauriRuntime() ? options.tauri() : options.mock();
}

function toPreviewUrl(previewPath: string, runtime: RuntimeMode): string {
  // Tauri returns a filesystem path; the browser mock already returns a web-safe URL.
  return runtime === "tauri" ? convertFileSrc(previewPath) : previewPath;
}

function asPreviewResult(
  previewPath: string,
  runtime: RuntimeMode,
  measurementScale: MeasurementScale | null,
): PreviewResult {
  return {
    previewUrl: toPreviewUrl(previewPath, runtime),
    measurementScale,
    runtime,
  };
}

function asProcessResult(
  previewPath: string,
  dicomPath: string,
  runtime: RuntimeMode,
  measurementScale: MeasurementScale | null,
): ProcessResult {
  return {
    ...asPreviewResult(previewPath, runtime, measurementScale),
    dicomPath,
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

export async function runBackendPreview(inputPath: string): Promise<PreviewResult> {
  return runInRuntime({
    mock: () => asPreviewResult(createMockPreview(false, "none"), getRuntimeMode(), null),
    tauri: async () => {
      const payload = await invoke<PreviewPayload>("run_backend_preview", { inputPath });
      return asPreviewResult(payload.previewPath, getRuntimeMode(), payload.measurementScale);
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

export async function runBackendProcess(
  inputPath: string,
  request: ProcessingRequest,
): Promise<ProcessResult> {
  const args = buildProcessingArgs(inputPath, request);

  return runInRuntime({
    mock: () =>
      asProcessResult(
        createMockPreview(true, request.controls.palette),
        request.outputPath ?? MOCK_PROCESSED_DICOM_PATH,
        getRuntimeMode(),
        null,
      ),
    tauri: async () => {
      const payload = await invoke<ProcessPayload>("run_backend_process", { args });

      return asProcessResult(
        payload.previewPath,
        payload.dicomPath,
        getRuntimeMode(),
        payload.measurementScale,
      );
    },
  });
}

export async function runBackendToothMeasurement(
  inputPath: string,
): Promise<ToothAnalysisResult> {
  return runInRuntime({
    mock: () => ({
      previewUrl: createMockPreview(false, "none"),
      analysis: createMockToothAnalysis(),
      runtime: getRuntimeMode(),
    }),
    tauri: async () => {
      const payload = await invoke<ToothMeasurementPayload>(
        "run_backend_tooth_measurement",
        { inputPath },
      );

      return {
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
  // Mirror the backend naming convention so suggested save paths line up with
  // the file the native process would auto-generate on its own.
  const fileName = inputPath.split(/[\\/]/).pop() ?? "study.dcm";
  const baseName = fileName.replace(/\.(dcm|dicom)$/i, "");
  return `${baseName}_processed.dcm`;
}

export function paletteLabel(palette: Palette): string {
  return PALETTE_LABELS[palette];
}
