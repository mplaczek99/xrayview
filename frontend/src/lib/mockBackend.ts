import { normalizeBackendError } from "./backendErrors";
import type {
  BackendError,
  BackendErrorCode,
  JobSnapshot as ContractJobSnapshot,
  JobResult,
  JobState,
  LineAnnotation,
  OpenStudyCommandResult,
  ProcessingManifest,
  StartedJob,
} from "./generated/contracts";
import { MOCK_PROCESSING_MANIFEST } from "./mockProcessingManifest";
import { createMockPreview, measureMockLineAnnotation } from "./mockStudy";
import type { BackendAPI } from "./runtimeTypes";

const mockJobs = new Map<string, ContractJobSnapshot>();
const mockJobControllers = new Map<string, { cancelled: boolean }>();

let mockStudySequence = 0;
let mockJobSequence = 0;

function mockError(code: BackendErrorCode, message: string, recoverable = true): BackendError {
  return normalizeBackendError({
    code,
    message,
    details: [],
    recoverable,
  });
}

function requireNonBlank(value: string, fieldName: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    throw mockError("invalidInput", `${fieldName} is required`);
  }
  return trimmed;
}

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
    throw mockError("notFound", `job not found: ${jobId}`);
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

export function createMockBackendAPI(): BackendAPI {
  return {
    mode: "mock",
    loadProcessingManifest: async (): Promise<ProcessingManifest> => MOCK_PROCESSING_MANIFEST,
    openStudy: async (inputPath): Promise<OpenStudyCommandResult> => {
      const resolvedInputPath = requireNonBlank(inputPath, "inputPath");
      return {
        study: {
          studyId: nextMockStudyId(),
          inputPath: resolvedInputPath,
          inputName: fileNameFromPath(resolvedInputPath),
          measurementScale: null,
        },
      };
    },
    startRenderStudyJob: async (studyId) => {
      const resolvedStudyId = requireNonBlank(studyId, "studyId");
      return startMockJob("renderStudy", resolvedStudyId, () => ({
        kind: "renderStudy",
        payload: {
          studyId: resolvedStudyId,
          previewPath: createMockPreview(false, "none"),
          loadedWidth: 1200,
          loadedHeight: 820,
          measurementScale: null,
        },
      }));
    },
    startAnalyzeStudyJob: async (studyId) => {
      const resolvedStudyId = requireNonBlank(studyId, "studyId");
      return startMockJob("analyzeStudy", resolvedStudyId, () => ({
        kind: "analyzeStudy",
        payload: {
          studyId: resolvedStudyId,
          previewPath: createMockPreview(true, "none", "outline"),
          filledPreviewPath: createMockPreview(true, "none", "filled"),
          loadedWidth: 1200,
          loadedHeight: 820,
          mode: "dynamic tooth and bone level overlay",
          measurementScale: null,
        },
      }));
    },
    startProcessStudyJob: async (studyId, request) => {
      const resolvedStudyId = requireNonBlank(studyId, "studyId");
      return startMockJob("processStudy", resolvedStudyId, () => ({
        kind: "processStudy",
        payload: {
          studyId: resolvedStudyId,
          previewPath: createMockPreview(true, request.controls.palette),
          loadedWidth: 1200,
          loadedHeight: 820,
          mode: request.compare ? "comparison output" : "processed preview",
          measurementScale: null,
        },
      }));
    },
    getJob: async (jobId): Promise<ContractJobSnapshot> => {
      const resolvedJobId = requireNonBlank(jobId, "jobId");
      const snapshot = mockJobs.get(resolvedJobId);
      if (!snapshot) {
        throw mockError("notFound", `job not found: ${resolvedJobId}`);
      }

      return snapshot;
    },
    getJobs: async (jobIds): Promise<ContractJobSnapshot[]> => {
      return [...new Set(jobIds.map((jobId) => jobId.trim()).filter(Boolean))].flatMap((jobId) => {
        const snapshot = mockJobs.get(jobId);
        return snapshot ? [snapshot] : [];
      });
    },
    cancelJob: async (jobId): Promise<ContractJobSnapshot> => {
      const resolvedJobId = requireNonBlank(jobId, "jobId");
      const snapshot = mockJobs.get(resolvedJobId);
      if (!snapshot) {
        throw mockError("notFound", `job not found: ${resolvedJobId}`);
      }

      if (
        snapshot.state === "completed" ||
        snapshot.state === "failed" ||
        snapshot.state === "cancelled"
      ) {
        return snapshot;
      }

      const controller = mockJobControllers.get(resolvedJobId);
      if (controller) {
        controller.cancelled = true;
      }

      const cancelling = updateMockJob(resolvedJobId, (job) => ({
        ...job,
        state: job.state === "queued" ? "cancelled" : "cancelling",
        progress: {
          ...job.progress,
          message: job.state === "queued" ? "Cancelled before start" : "Cancellation requested",
        },
      }));

      if (cancelling.state === "cancelling") {
        setTimeout(() => {
          updateMockJob(resolvedJobId, (job) => ({
            ...job,
            state: "cancelled",
            progress: {
              percent: job.progress.percent,
              stage: "cancelled",
              message: "Cancelled by user",
            },
          }));
          mockJobControllers.delete(resolvedJobId);
        }, 30);
      } else {
        mockJobControllers.delete(resolvedJobId);
      }

      return cancelling;
    },
    measureLineAnnotation: async (_studyId, annotation): Promise<LineAnnotation> =>
      measureMockLineAnnotation(annotation),
  };
}
