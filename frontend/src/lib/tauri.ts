import { open } from "@tauri-apps/plugin-dialog";
import { normalizeBackendError } from "./backendErrors";

const BMP_FILTER = [
  {
    name: "Bitewing X-ray (BMP)",
    extensions: ["bmp"],
  },
];

export function isTauriRuntime(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return Boolean((window as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__);
}

export async function pickTauriBmpFile(): Promise<string | null> {
  try {
    const selected = await open({
      title: "Open Bitewing X-ray",
      multiple: false,
      directory: false,
      filters: BMP_FILTER,
    });
    if (typeof selected === "string" && selected.trim()) {
      return selected;
    }
    return null;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}
