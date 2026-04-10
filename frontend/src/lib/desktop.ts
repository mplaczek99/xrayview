import {
  isWailsRuntime,
  pickWailsDicomFile,
  pickWailsSaveDicomPath,
} from "./wails";

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

export function buildDesktopPreviewUrl(previewPath: string): string {
  return `/preview?path=${encodeURIComponent(previewPath)}`;
}
