import { buildMockPath, MOCK_DICOM_PATH, MOCK_EXPORT_DIRECTORY } from "./mockRuntime";
import { pickDesktopDicomFile, pickDesktopSaveDicomPath } from "./desktop";
import type { ShellAPI } from "./runtimeTypes";

export function createMockShellAPI(): ShellAPI {
  return {
    pickDicomFile: async () => MOCK_DICOM_PATH,
    pickSaveDicomPath: async (defaultName) =>
      buildMockPath(MOCK_EXPORT_DIRECTORY, defaultName),
  };
}

export function createDesktopShellAPI(): ShellAPI {
  return {
    pickDicomFile: () => pickDesktopDicomFile(),
    pickSaveDicomPath: (defaultName) => pickDesktopSaveDicomPath(defaultName),
  };
}
