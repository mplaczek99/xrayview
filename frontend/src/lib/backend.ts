import type {
  AnalyzeStudyCommand,
  BackendError,
  JobCommand,
  JobResult,
  JobSnapshot as ContractJobSnapshot,
  JobState,
  LineAnnotation,
  MeasureLineAnnotationCommand,
  MeasureLineAnnotationCommandResult,
  OpenStudyCommand,
  OpenStudyCommandResult,
  PaletteName,
  ProcessStudyCommand,
  ProcessingManifest,
  RenderStudyCommand,
  StartedJob,
} from "./generated/contracts";
import { normalizeBackendError } from "./backendErrors";
import { invokeDesktopBackendCommand } from "./desktop";
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

function buildProcessStudyCommand(
  studyId: string,
  request: ProcessingRequest,
): ProcessStudyCommand {
  return {
    studyId,
    outputPath: request.outputPath,
    presetId: request.presetId,
    invert: request.controls.invert && !request.presetControls.invert,
    brightness:
      request.controls.brightness !== request.presetControls.brightness
        ? request.controls.brightness
        : null,
    contrast:
      request.controls.contrast !== request.presetControls.contrast
        ? request.controls.contrast
        : null,
    equalize: request.controls.equalize && !request.presetControls.equalize,
    compare: request.compare,
    palette:
      request.controls.palette !== request.presetControls.palette
        ? request.controls.palette
        : null,
  };
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

function parseBackendResponseBody<T>(
  command: string,
  responseBody: string,
): T | null {
  if (responseBody.trim() === "") {
    return null;
  }

  try {
    return JSON.parse(responseBody) as T;
  } catch (error) {
    throw normalizeBackendError({
      code: "internal",
      message: `invalid JSON response for backend command ${command}`,
      details: error instanceof Error && error.message ? [error.message] : [],
      recoverable: false,
    });
  }
}

async function invokeDesktopBackend<T>(
  command: string,
  payload?: unknown,
): Promise<T> {
  const response = await invokeDesktopBackendCommand(command, payload);
  const parsed = parseBackendResponseBody<T | BackendError>(command, response.body);

  if (response.status >= 400) {
    throw normalizeBackendError(
      parsed ?? {
        code: "internal",
        message: `backend command ${command} failed with status ${response.status}`,
        details: [],
        recoverable: response.status >= 500,
      },
    );
  }

  return parsed as T;
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
  return {
    mode: "desktop",
    loadProcessingManifest: () =>
      invokeDesktopBackend<ProcessingManifest>("get_processing_manifest"),
    openStudy: async (inputPath): Promise<OpenStudyCommandResult> => {
      const request: OpenStudyCommand = { inputPath };
      return invokeDesktopBackend<OpenStudyCommandResult>("open_study", request);
    },
    startRenderStudyJob: async (studyId) => {
      const request: RenderStudyCommand = { studyId };
      return invokeDesktopBackend<StartedJob>("start_render_job", request);
    },
    startProcessStudyJob: async (studyId, request) =>
      invokeDesktopBackend<StartedJob>(
        "start_process_job",
        buildProcessStudyCommand(studyId, request),
      ),
    startAnalyzeStudyJob: async (studyId) => {
      const request: AnalyzeStudyCommand = { studyId };
      return invokeDesktopBackend<StartedJob>("start_analyze_job", request);
    },
    getJob: async (jobId) => {
      const request: JobCommand = { jobId };
      return invokeDesktopBackend<ContractJobSnapshot>("get_job", request);
    },
    cancelJob: async (jobId) => {
      const request: JobCommand = { jobId };
      return invokeDesktopBackend<ContractJobSnapshot>("cancel_job", request);
    },
    measureLineAnnotation: async (studyId, annotation): Promise<LineAnnotation> => {
      const request: MeasureLineAnnotationCommand = {
        studyId,
        annotation,
      };
      const payload = await invokeDesktopBackend<MeasureLineAnnotationCommandResult>(
        "measure_line_annotation",
        request,
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
