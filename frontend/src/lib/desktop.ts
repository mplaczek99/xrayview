import { convertFileSrc } from "@tauri-apps/api/core";
import {
  isTauriRuntime,
  pickTauriDicomFile,
  pickTauriSaveDicomPath,
} from "./tauri";

export function isDesktopRuntime(): boolean {
  return isTauriRuntime();
}

export async function pickDesktopDicomFile(): Promise<string | null> {
  return pickTauriDicomFile();
}

export async function pickDesktopSaveDicomPath(
  defaultName?: string,
): Promise<string | null> {
  return pickTauriSaveDicomPath(defaultName);
}

export function buildDesktopPreviewUrl(previewPath: string): string {
  return convertFileSrc(previewPath);
}
