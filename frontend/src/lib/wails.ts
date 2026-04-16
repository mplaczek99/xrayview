import { normalizeBackendError } from "./backendErrors";
import type {
  AnalyzeStudyCommand,
  JobCommand,
  JobSnapshot,
  MeasureLineAnnotationCommand,
  MeasureLineAnnotationCommandResult,
  OpenStudyCommand,
  OpenStudyCommandResult,
  ProcessStudyCommand,
  ProcessingManifest,
  RenderStudyCommand,
  StartedJob,
} from "./generated/contracts";

export interface DesktopBindings {
  PickDicomFile(): Promise<string>;
  PickSaveDicomPath(defaultName?: string): Promise<string>;
  OpenStudy(command: OpenStudyCommand): Promise<OpenStudyCommandResult>;
  StartRenderJob(command: RenderStudyCommand): Promise<StartedJob>;
  StartProcessJob(command: ProcessStudyCommand): Promise<StartedJob>;
  StartAnalyzeJob(command: AnalyzeStudyCommand): Promise<StartedJob>;
  GetJobSnapshot(command: JobCommand): Promise<JobSnapshot>;
  GetJobsSnapshot(command: { jobIds: string[] }): Promise<JobSnapshot[]>;
  CancelJobByID(command: JobCommand): Promise<JobSnapshot>;
  GetProcessingManifest(): Promise<ProcessingManifest>;
  MeasureLineAnnotation(
    command: MeasureLineAnnotationCommand,
  ): Promise<MeasureLineAnnotationCommandResult>;
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
    throw new Error(
      "Desktop bindings are not available. Launch this entry inside the supported desktop shell.",
    );
  }

  return bindings;
}

export function getWailsBindings(): DesktopBindings {
  return requireBindings();
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

