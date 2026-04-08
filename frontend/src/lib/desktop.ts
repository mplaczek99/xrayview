import {
  invokeWailsBackendCommand,
  isWailsRuntime,
  pickWailsDicomFile,
  pickWailsSaveDicomPath,
} from "./wails";

export type DesktopBackendCommandResponse = Awaited<
  ReturnType<typeof invokeWailsBackendCommand>
>;

export function isDesktopRuntime(): boolean {
  return isWailsRuntime();
}

export async function pickDesktopDicomFile(): Promise<string | null> {
  return pickWailsDicomFile();
}

export async function pickDesktopSaveDicomPath(
  defaultName?: string,
): Promise<string | null> {
  return pickWailsSaveDicomPath(defaultName);
}

export async function invokeDesktopBackendCommand(
  command: string,
  payload?: unknown,
): Promise<DesktopBackendCommandResponse> {
  return invokeWailsBackendCommand(command, payload);
}

export function buildDesktopPreviewUrl(previewPath: string): string {
  return `/preview?path=${encodeURIComponent(previewPath)}`;
}
