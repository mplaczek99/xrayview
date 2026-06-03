import { pickDesktopBmpFile } from "./desktop";
import { MOCK_BMP_PATH } from "./mockRuntime";
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
