import { convertFileSrc, invoke } from "@tauri-apps/api/core";
import { createMockPreview } from "./mockStudy";
import type {
  Palette,
  PreviewResult,
  ProcessResult,
  ProcessingControls,
  RuntimeMode,
} from "./types";

interface PreviewPayload {
  previewPath: string;
}

interface ProcessPayload extends PreviewPayload {
  dicomPath: string;
}

const MOCK_STUDY_DIRECTORY = "mock-data";
const MOCK_EXPORT_DIRECTORY = "mock-exports";
const MOCK_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study.dcm`;
const MOCK_PROCESSED_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study_processed.dcm`;
const PALETTE_LABELS: Record<Palette, string> = {
  none: "Neutral",
  hot: "Hot",
  bone: "Bone",
};

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function getRuntimeMode(): RuntimeMode {
  return isTauriRuntime() ? "tauri" : "mock";
}

function buildMockPath(directory: string, fileName: string): string {
  return `${directory}/${fileName}`;
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

function asPreviewResult(previewPath: string, runtime: RuntimeMode): PreviewResult {
  return {
    previewUrl: toPreviewUrl(previewPath, runtime),
    runtime,
  };
}

function asProcessResult(previewPath: string, dicomPath: string, runtime: RuntimeMode): ProcessResult {
  return {
    ...asPreviewResult(previewPath, runtime),
    dicomPath,
  };
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
    mock: () => asPreviewResult(createMockPreview(false, "none"), getRuntimeMode()),
    tauri: async () => {
      const payload = await invoke<PreviewPayload>("run_backend_preview", { inputPath });
      return asPreviewResult(payload.previewPath, getRuntimeMode());
    },
  });
}

export async function runBackendProcess(
  inputPath: string,
  controls: ProcessingControls,
): Promise<ProcessResult> {
  return runInRuntime({
    mock: () =>
      asProcessResult(
        createMockPreview(true, controls.palette),
        MOCK_PROCESSED_DICOM_PATH,
        getRuntimeMode(),
      ),
    tauri: async () => {
      const payload = await invoke<ProcessPayload>("run_backend_process", {
        inputPath,
        options: controls,
      });

      return asProcessResult(payload.previewPath, payload.dicomPath, getRuntimeMode());
    },
  });
}

export async function copyProcessedOutput(
  sourcePath: string,
  destinationPath: string,
): Promise<string> {
  return runInRuntime({
    mock: () => destinationPath,
    tauri: () => invoke<string>("copy_processed_output", { sourcePath, destinationPath }),
  });
}

export function ensureDicomExtension(path: string): string {
  return /\.(dcm|dicom)$/i.test(path) ? path : `${path}.dcm`;
}

export function buildOutputName(inputPath: string): string {
  const fileName = inputPath.split(/[\\/]/).pop() ?? "study.dcm";
  const baseName = fileName.replace(/\.(dcm|dicom)$/i, "");
  return `${baseName}_processed.dcm`;
}

export function paletteLabel(palette: Palette): string {
  return PALETTE_LABELS[palette];
}
