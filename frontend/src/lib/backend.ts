import type {
  JobResult,
  JobSnapshot as ContractJobSnapshot,
  JobState,
  LineAnnotation,
  MeasureLineAnnotationCommandResult,
  OpenStudyCommandResult,
  PaletteName,
  ProcessingManifest,
  StartedJob,
} from "./generated/contracts";
import { normalizeBackendError } from "./backendErrors";
import { buildProcessStudyCommand } from "./commandBuilders";
import { MOCK_PROCESSED_DICOM_PATH } from "./mockRuntime";
import { MOCK_PROCESSING_MANIFEST } from "./mockProcessingManifest";
import {
  createMockPreview,
  createMockSuggestedAnnotations,
  createMockToothAnalysis,
  measureMockLineAnnotation,
} from "./mockStudy";
import type { BackendAPI } from "./runtimeTypes";
import type { ProcessingRequest } from "./types";
import { getWailsBindings } from "./wails";

const PALETTE_LABELS: Record<PaletteName, string> = {
  none: "Neutral",
  hot: "Hot",
  bone: "Bone",
};

const mockJobs = new Map<string, ContractJobSnapshot>();
const mockJobControllers = new Map<string, { cancelled: boolean }>();

let mockStudySequence = 0;
let mockJobSequence = 0;

export const FALLBACK_PROCESSING_MANIFEST = MOCK_PROCESSING_MANIFEST;

function fileNameFromPath(inputPath: string): string {
  return inputPath.split(/[\\/]/).pop() ?? inputPath;
}

function nextMockStudyId(): string {
  mockStudySequence += 1;
  return `mock-study-${mockStudySequence}`;
}

function nextMockJobId(): string {
  mockJobSequence += 1;
  return `mock-job-${mockJobSequence}`;
}

function buildMockJobSnapshot(
  jobId: string,
  jobKind: ContractJobSnapshot["jobKind"],
  studyId: string,
): ContractJobSnapshot {
  return {
    jobId,
    jobKind,
    studyId,
    state: "queued",
    progress: {
      percent: 0,
      stage: "queued",
      message: "Queued",
    },
    fromCache: false,
    result: null,
    error: null,
  };
}

function updateMockJob(
  jobId: string,
  updater: (job: ContractJobSnapshot) => ContractJobSnapshot,
): ContractJobSnapshot {
  const current = mockJobs.get(jobId);
  if (!current) {
    throw normalizeBackendError({
      code: "notFound",
      message: `job not found: ${jobId}`,
      details: [],
      recoverable: true,
    });
  }

  const next = updater(current);
  mockJobs.set(jobId, next);
  return next;
}

function scheduleMockCompletion(jobId: string, resultFactory: () => JobResult) {
  const controller = { cancelled: false };
  mockJobControllers.set(jobId, controller);
  const steps: Array<{
    delay: number;
    state: JobState;
    percent: number;
    stage: string;
    message: string;
  }> = [
    { delay: 50, state: "running", percent: 18, stage: "preparing", message: "Preparing job" },
    { delay: 180, state: "running", percent: 62, stage: "working", message: "Working" },
    { delay: 360, state: "running", percent: 90, stage: "finishing", message: "Finishing" },
  ];

  for (const step of steps) {
    setTimeout(() => {
      if (controller.cancelled) {
        return;
      }

      updateMockJob(jobId, (job) => ({
        ...job,
        state: step.state,
        progress: {
          percent: step.percent,
          stage: step.stage,
          message: step.message,
        },
      }));
    }, step.delay);
  }

  setTimeout(() => {
    if (controller.cancelled) {
      return;
    }

    updateMockJob(jobId, (job) => ({
      ...job,
      state: "completed",
      progress: {
        percent: 100,
        stage: "completed",
        message: "Completed",
      },
      result: resultFactory(),
      error: null,
    }));
    mockJobControllers.delete(jobId);
  }, 520);
}

function startMockJob(
  jobKind: ContractJobSnapshot["jobKind"],
  studyId: string,
  resultFactory: () => JobResult,
): StartedJob {
  const jobId = nextMockJobId();
  mockJobs.set(jobId, buildMockJobSnapshot(jobId, jobKind, studyId));
  scheduleMockCompletion(jobId, resultFactory);
  return { jobId };
}

async function invokeTypedDesktopBinding<T>(
  invoke: () => Promise<T>,
): Promise<T> {
  try {
    return await invoke();
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export function createMockBackendAPI(): BackendAPI {
  return {
    mode: "mock",
    loadProcessingManifest: async (): Promise<ProcessingManifest> => MOCK_PROCESSING_MANIFEST,
    openStudy: async (inputPath): Promise<OpenStudyCommandResult> => ({
      study: {
        studyId: nextMockStudyId(),
        inputPath,
        inputName: fileNameFromPath(inputPath),
        measurementScale: null,
      },
    }),
    startRenderStudyJob: async (studyId) =>
      startMockJob("renderStudy", studyId, () => ({
        kind: "renderStudy",
        payload: {
          studyId,
          previewPath: createMockPreview(false, "none"),
          loadedWidth: 1200,
          loadedHeight: 820,
          measurementScale: null,
        },
      })),
    startProcessStudyJob: async (studyId, request) =>
      startMockJob("processStudy", studyId, () => ({
        kind: "processStudy",
        payload: {
          studyId,
          previewPath: createMockPreview(true, request.controls.palette),
          dicomPath: request.outputPath ?? MOCK_PROCESSED_DICOM_PATH,
          loadedWidth: 1200,
          loadedHeight: 820,
          mode: request.compare ? "comparison output" : "processed preview",
          measurementScale: null,
        },
      })),
    startAnalyzeStudyJob: async (studyId) =>
      startMockJob("analyzeStudy", studyId, () => ({
        kind: "analyzeStudy",
        payload: {
          studyId,
          previewPath: createMockPreview(false, "none"),
          analysis: createMockToothAnalysis(),
          suggestedAnnotations: createMockSuggestedAnnotations(),
        },
      })),
    getJob: async (jobId): Promise<ContractJobSnapshot> => {
      const snapshot = mockJobs.get(jobId);
      if (!snapshot) {
        throw normalizeBackendError({
          code: "notFound",
          message: `job not found: ${jobId}`,
          details: [],
          recoverable: true,
        });
      }

      return snapshot;
    },
    getJobs: async (jobIds): Promise<ContractJobSnapshot[]> => {
      const unique = [...new Set(jobIds)];
      return unique.flatMap((jobId) => {
        const snapshot = mockJobs.get(jobId);
        return snapshot ? [snapshot] : [];
      });
    },
    cancelJob: async (jobId): Promise<ContractJobSnapshot> => {
      const snapshot = mockJobs.get(jobId);
      if (!snapshot) {
        throw normalizeBackendError({
          code: "notFound",
          message: `job not found: ${jobId}`,
          details: [],
          recoverable: true,
        });
      }

      if (
        snapshot.state === "completed" ||
        snapshot.state === "failed" ||
        snapshot.state === "cancelled"
      ) {
        return snapshot;
      }

      const controller = mockJobControllers.get(jobId);
      if (controller) {
        controller.cancelled = true;
      }

      const cancelling = updateMockJob(jobId, (job) => ({
        ...job,
        state: job.state === "queued" ? "cancelled" : "cancelling",
        progress: {
          ...job.progress,
          message:
            job.state === "queued" ? "Cancelled before start" : "Cancellation requested",
        },
      }));

      if (cancelling.state === "cancelling") {
        setTimeout(() => {
          updateMockJob(jobId, (job) => ({
            ...job,
            state: "cancelled",
            progress: {
              percent: job.progress.percent,
              stage: "cancelled",
              message: "Cancelled by user",
            },
          }));
          mockJobControllers.delete(jobId);
        }, 30);
      } else {
        mockJobControllers.delete(jobId);
      }

      return cancelling;
    },
    measureLineAnnotation: async (_studyId, annotation): Promise<LineAnnotation> =>
      measureMockLineAnnotation(annotation),
  };
}

export function createDesktopBackendAPI(): BackendAPI {
  const bindings = getWailsBindings();

  return {
    mode: "desktop",
    loadProcessingManifest: () =>
      invokeTypedDesktopBinding(() => bindings.GetProcessingManifest()),
    openStudy: async (inputPath): Promise<OpenStudyCommandResult> =>
      invokeTypedDesktopBinding(() => bindings.OpenStudy({ inputPath })),
    startRenderStudyJob: async (studyId) =>
      invokeTypedDesktopBinding(() => bindings.StartRenderJob({ studyId })),
    startProcessStudyJob: async (studyId, request) =>
      invokeTypedDesktopBinding(() =>
        bindings.StartProcessJob(buildProcessStudyCommand(studyId, request)),
      ),
    startAnalyzeStudyJob: async (studyId) =>
      invokeTypedDesktopBinding(() => bindings.StartAnalyzeJob({ studyId })),
    getJob: async (jobId) =>
      invokeTypedDesktopBinding(() => bindings.GetJobSnapshot({ jobId })),
    getJobs: async (jobIds) =>
      invokeTypedDesktopBinding(() => bindings.GetJobsSnapshot({ jobIds: [...new Set(jobIds)] })),
    cancelJob: async (jobId) =>
      invokeTypedDesktopBinding(() => bindings.CancelJobByID({ jobId })),
    measureLineAnnotation: async (studyId, annotation): Promise<LineAnnotation> => {
      const payload = await invokeTypedDesktopBinding(() =>
        bindings.MeasureLineAnnotation({
          studyId,
          annotation,
        }),
      );
      return payload.annotation;
    },
  };
}

export function ensureDicomExtension(path: string): string {
  return /\.(dcm|dicom)$/i.test(path) ? path : `${path}.dcm`;
}

export function buildOutputName(inputPath: string): string {
  const fileName = fileNameFromPath(inputPath) || "study.dcm";
  const baseName = fileName.replace(/\.(dcm|dicom)$/i, "");
  return `${baseName}_processed.dcm`;
}

export function paletteLabel(palette: PaletteName): string {
  return PALETTE_LABELS[palette];
}
