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

const MOCK_DICOM_PATH = "/tmp/mock-dental-study.dcm";

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function asPreviewResult(previewPath: string, runtime: RuntimeMode): PreviewResult {
  return {
    previewUrl: runtime === "tauri" ? convertFileSrc(previewPath) : previewPath,
    runtime,
  };
}

export async function pickDicomFile(): Promise<string | null> {
  if (!isTauriRuntime()) {
    return MOCK_DICOM_PATH;
  }

  return invoke<string | null>("pick_dicom_file");
}

export async function pickSaveDicomPath(defaultName: string): Promise<string | null> {
  if (!isTauriRuntime()) {
    return `/tmp/${defaultName}`;
  }

  return invoke<string | null>("pick_save_dicom_path", { defaultName });
}

export async function runBackendPreview(inputPath: string): Promise<PreviewResult> {
  if (!isTauriRuntime()) {
    return asPreviewResult(createMockPreview(false, "none"), "mock");
  }

  const payload = await invoke<PreviewPayload>("run_backend_preview", { inputPath });
  return asPreviewResult(payload.previewPath, "tauri");
}

export async function runBackendProcess(
  inputPath: string,
  controls: ProcessingControls,
): Promise<ProcessResult> {
  if (!isTauriRuntime()) {
    return {
      previewUrl: createMockPreview(true, controls.palette),
      dicomPath: "/tmp/mock-dental-study_processed.dcm",
      runtime: "mock",
    };
  }

  const payload = await invoke<ProcessPayload>("run_backend_process", {
    inputPath,
    options: controls,
  });

  return {
    previewUrl: convertFileSrc(payload.previewPath),
    dicomPath: payload.dicomPath,
    runtime: "tauri",
  };
}

export async function copyProcessedOutput(
  sourcePath: string,
  destinationPath: string,
): Promise<string> {
  if (!isTauriRuntime()) {
    return destinationPath;
  }

  return invoke<string>("copy_processed_output", { sourcePath, destinationPath });
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
  return palette === "none" ? "Neutral" : palette === "hot" ? "Hot" : "Bone";
}
