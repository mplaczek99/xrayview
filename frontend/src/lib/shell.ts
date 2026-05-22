import { MOCK_BMP_PATH } from "./mockRuntime";
import { pickDesktopBmpFile } from "./desktop";
import type { ShellAPI } from "./runtimeTypes";

export function createMockShellAPI(): ShellAPI {
  return {
    pickBmpFile: async () => MOCK_BMP_PATH,
  };
}

export function createDesktopShellAPI(): ShellAPI {
  return {
    pickBmpFile: () => pickDesktopBmpFile(),
  };
}
