import { normalizeBackendError } from "./backendErrors";

export interface WailsBackendCommandResponse {
  status: number;
  body: string;
}

interface DesktopBindings {
  PickDicomFile(): Promise<string>;
  PickSaveDicomPath(defaultName?: string): Promise<string>;
  InvokeBackendCommand(
    command: string,
    payloadJson?: string,
  ): Promise<WailsBackendCommandResponse>;
}

declare global {
  interface Window {
    go?: {
      main?: {
        DesktopApp?: DesktopBindings;
      };
    };
  }
}

function requireBindings(): DesktopBindings {
  const bindings = window.go?.main?.DesktopApp;
  if (!bindings) {
    throw new Error("Wails bindings are not available. Launch this entry inside the Wails desktop shell.");
  }

  return bindings;
}

export function isWailsRuntime(): boolean {
  if (typeof window === "undefined") {
    return false;
  }

  return (
    window.location.protocol === "wails:" ||
    window.location.host === "wails.localhost"
  );
}

export async function pickWailsDicomFile(): Promise<string | null> {
  try {
    const selected = await requireBindings().PickDicomFile();
    return selected || null;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export async function pickWailsSaveDicomPath(
  defaultName?: string,
): Promise<string | null> {
  try {
    const selected = await requireBindings().PickSaveDicomPath(defaultName);
    return selected || null;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export async function invokeWailsBackendCommand(
  command: string,
  payload?: unknown,
): Promise<WailsBackendCommandResponse> {
  try {
    return await requireBindings().InvokeBackendCommand(
      command,
      payload === undefined ? "" : JSON.stringify(payload),
    );
  } catch (error) {
    throw normalizeBackendError(error);
  }
}
