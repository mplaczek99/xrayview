import { convertFileSrc } from "@tauri-apps/api/core";
import { isTauriRuntime, pickTauriBmpFile } from "./tauri";

export function isDesktopRuntime(): boolean {
  return isTauriRuntime();
}

export async function pickDesktopBmpFile(): Promise<string | null> {
  return pickTauriBmpFile();
}

export function buildDesktopPreviewUrl(previewPath: string): string {
  return convertFileSrc(previewPath);
}
