import { convertFileSrc, invoke } from "@tauri-apps/api/core";
import { normalizeBackendError } from "./backendErrors";
import { buildMockPath, MOCK_DICOM_PATH, MOCK_EXPORT_DIRECTORY } from "./mockRuntime";
import type { ShellAPI } from "./runtimeTypes";

async function invokeWithShellError<T>(
  command: string,
  args?: Record<string, unknown>,
): Promise<T> {
  try {
    return await invoke<T>(command, args);
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export function createMockShellAPI(): ShellAPI {
  return {
    mode: "mock",
    pickDicomFile: async () => MOCK_DICOM_PATH,
    pickSaveDicomPath: async (defaultName) =>
      buildMockPath(MOCK_EXPORT_DIRECTORY, defaultName),
    resolvePreviewUrl: (previewPath) => previewPath,
  };
}

export function createTauriShellAPI(): ShellAPI {
  return {
    mode: "tauri",
    pickDicomFile: () => invokeWithShellError<string | null>("pick_dicom_file"),
    pickSaveDicomPath: (defaultName) =>
      invokeWithShellError<string | null>("pick_save_dicom_path", { defaultName }),
    resolvePreviewUrl: (previewPath) => convertFileSrc(previewPath),
  };
}
