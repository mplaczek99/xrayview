import { normalizeBackendError } from "./backendErrors";

// The Wails-bound backend (desktop/bindings.go), reached on the webview as
// window.go.main.App.<Method>. Command payloads are the contract command objects;
// they are kept loosely typed here and cast to contract types in desktopBackend.
export interface WailsBackend {
  GetProcessingManifest(): Promise<unknown>;
  OpenStudy(command: unknown): Promise<unknown>;
  StartRenderJob(command: unknown): Promise<unknown>;
  StartAnalyzeJob(command: unknown): Promise<unknown>;
  StartProcessJob(command: unknown): Promise<unknown>;
  GetJob(command: unknown): Promise<unknown>;
  GetJobs(command: unknown): Promise<unknown>;
  CancelJob(command: unknown): Promise<unknown>;
  MeasureLineAnnotation(command: unknown): Promise<unknown>;
  SetStudyCalibration(command: unknown): Promise<unknown>;
  PickBmpFile(): Promise<string>;
}

// Wails injects this runtime helper into the webview.
interface WailsRuntime {
  EventsOn(eventName: string, callback: (...data: unknown[]) => void): () => void;
}

declare global {
  interface Window {
    go?: { main?: { App?: WailsBackend } };
    runtime?: WailsRuntime;
  }
}

export function isWailsRuntime(): boolean {
  return typeof window !== "undefined" && Boolean(window.runtime && window.go?.main?.App);
}

export function getWailsBackend(): WailsBackend {
  const backend = window.go?.main?.App;
  if (!backend) {
    throw normalizeBackendError(new Error("Wails backend bindings are unavailable"));
  }
  return backend;
}

// Subscribes to a Wails event, returning the unsubscribe function. Wails passes
// the emitted payload as the first callback argument.
export function onWailsEvent(eventName: string, callback: (payload: unknown) => void): () => void {
  const runtime = window.runtime;
  if (!runtime) {
    throw normalizeBackendError(new Error("Wails runtime is unavailable"));
  }
  return runtime.EventsOn(eventName, (...data: unknown[]) => callback(data[0]));
}

// Native single-BMP file picker. Returns null when the user cancels (the Go side
// returns an empty string). Replaces the former tauri-plugin-dialog open().
export async function pickBmpFile(): Promise<string | null> {
  try {
    const selected = await getWailsBackend().PickBmpFile();
    if (typeof selected === "string" && selected.trim()) {
      return selected;
    }
    return null;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}
