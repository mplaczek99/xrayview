export interface BackendHealth {
  status: string;
  service: string;
  transport: string;
  listenAddress: string;
  cacheDir: string;
  persistenceDir: string;
  studyCount: number;
  startedAt: string;
}

export interface PrototypeInfo {
  backendBaseUrl: string;
  previewEndpointPath: string;
  assetDir: string;
  sidecarBinaryPath: string;
  sidecarManaged: boolean;
  startupError: string | null;
  sampleDicomPath: string;
  samplePreviewPath: string;
  backendHealth: BackendHealth | null;
}

export interface MeasurementScale {
  rowSpacingMm: number;
  columnSpacingMm: number;
  source: string;
}

export interface OpenStudyResult {
  studyId: string;
  inputPath: string;
  inputName: string;
  measurementScale: MeasurementScale | null;
  roundTripMs: number;
}

interface PrototypeBindings {
  PrototypeInfo(): Promise<PrototypeInfo>;
  PickDicomFile(): Promise<string>;
  PickPreviewArtifact(): Promise<string>;
  PickSaveDicomPath(defaultName?: string): Promise<string>;
  OpenStudy(inputPath: string): Promise<OpenStudyResult>;
}

declare global {
  interface Window {
    go?: {
      main?: {
        PrototypeApp?: PrototypeBindings;
      };
    };
  }
}

function requireBindings(): PrototypeBindings {
  const bindings = window.go?.main?.PrototypeApp;
  if (!bindings) {
    throw new Error("Wails bindings are not available. Launch this entry inside the Wails prototype.");
  }

  return bindings;
}

export async function loadPrototypeInfo(): Promise<PrototypeInfo> {
  return requireBindings().PrototypeInfo();
}

export async function pickDicomFile(): Promise<string | null> {
  const selected = await requireBindings().PickDicomFile();
  return selected || null;
}

export async function pickPreviewArtifact(): Promise<string | null> {
  const selected = await requireBindings().PickPreviewArtifact();
  return selected || null;
}

export async function pickSaveDicomPath(
  defaultName?: string,
): Promise<string | null> {
  const selected = await requireBindings().PickSaveDicomPath(defaultName);
  return selected || null;
}

export async function openStudy(inputPath: string): Promise<OpenStudyResult> {
  return requireBindings().OpenStudy(inputPath);
}
