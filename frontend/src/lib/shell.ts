import { buildMockPath, MOCK_DICOM_PATH, MOCK_EXPORT_DIRECTORY } from "./mockRuntime";
import type { ShellAPI } from "./runtimeTypes";
import { pickWailsDicomFile, pickWailsSaveDicomPath } from "./wails";

export function createMockShellAPI(): ShellAPI {
  return {
    mode: "mock",
    pickDicomFile: async () => MOCK_DICOM_PATH,
    pickSaveDicomPath: async (defaultName) =>
      buildMockPath(MOCK_EXPORT_DIRECTORY, defaultName),
    resolvePreviewUrl: (previewPath) => previewPath,
  };
}

function buildPreviewUrl(previewPath: string): string {
  return `/preview?path=${encodeURIComponent(previewPath)}`;
}

export function createWailsShellAPI(): ShellAPI {
  return {
    mode: "wails",
    pickDicomFile: () => pickWailsDicomFile(),
    pickSaveDicomPath: (defaultName) => pickWailsSaveDicomPath(defaultName),
    resolvePreviewUrl: (previewPath) => buildPreviewUrl(previewPath),
  };
}
