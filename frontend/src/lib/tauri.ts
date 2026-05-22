import { open, save } from "@tauri-apps/plugin-dialog";
import { normalizeBackendError } from "./backendErrors";

const DICOM_FILTERS = [
  {
    name: "Supported Files",
    extensions: ["dcm", "dicom", "bmp", "tif", "tiff"],
  },
  {
    name: "All Files",
    extensions: ["*"],
  },
];

const DICOM_SAVE_FILTERS = [
  {
    name: "DICOM Files",
    extensions: ["dcm", "dicom"],
  },
];

export function isTauriRuntime(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return Boolean((window as { __TAURI_INTERNALS__?: unknown }).__TAURI_INTERNALS__);
}

export async function pickTauriDicomFile(): Promise<string | null> {
  try {
    const selected = await open({
      title: "Open Study or BMP/TIFF",
      multiple: false,
      directory: false,
      filters: DICOM_FILTERS,
    });
    if (typeof selected === "string" && selected.trim()) {
      return selected;
    }
    return null;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export async function pickTauriSaveDicomPath(
  defaultName?: string,
): Promise<string | null> {
  try {
    const selected = await save({
      title: "Save Processed DICOM",
      defaultPath: defaultName,
      filters: DICOM_SAVE_FILTERS,
    });
    if (typeof selected === "string" && selected.trim()) {
      return selected;
    }
    return null;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}
