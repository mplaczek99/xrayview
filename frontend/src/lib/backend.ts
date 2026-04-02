import { convertFileSrc, invoke } from "@tauri-apps/api/core";
import type {
  AnalyzeStudyCommand,
  AnalyzeStudyCommandResult,
  LineAnnotation,
  BackendError,
  JobCommand,
  JobSnapshot as ContractJobSnapshot,
  JobState,
  MeasureLineAnnotationCommand,
  MeasureLineAnnotationCommandResult,
  MeasurementScale,
  OpenStudyCommand,
  OpenStudyCommandResult,
  PaletteName,
  ProcessStudyCommand,
  ProcessStudyCommandResult,
  ProcessingManifest,
  ProcessingPipelineStep,
  RenderStudyCommand,
  RenderStudyCommandResult,
  StartedJob,
} from "./generated/contracts";
import { MOCK_PROCESSING_MANIFEST } from "./mockProcessingManifest";
import {
  createMockPreview,
  createMockSuggestedAnnotations,
  createMockToothAnalysis,
  measureMockLineAnnotation,
} from "./mockStudy";
import type {
  JobResultPayload,
  JobSnapshot,
} from "../features/jobs/model";
import type {
  OpenedStudy,
  Palette,
  PreviewResult,
  ProcessResult,
  ProcessingRequest,
  RuntimeMode,
  ToothAnalysisResult,
} from "./types";

const MOCK_STUDY_DIRECTORY = "mock-data";
const MOCK_EXPORT_DIRECTORY = "mock-exports";
const MOCK_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study.dcm`;
const MOCK_PROCESSED_DICOM_PATH = `${MOCK_STUDY_DIRECTORY}/mock-dental-study_processed.dcm`;
const DEFAULT_PIPELINE: ProcessingPipelineStep[] = [
  "grayscale",
  "invert",
  "brightness",
  "contrast",
  "equalize",
];
const PALETTE_LABELS: Record<Palette, string> = {
  none: "Neutral",
  hot: "Hot",
  bone: "Bone",
};

const mockJobListeners = new Set<(job: JobSnapshot) => void>();
const mockJobs = new Map<string, JobSnapshot>();
const mockJobControllers = new Map<string, { cancelled: boolean }>();

let mockStudySequence = 0;
let mockJobSequence = 0;

export const FALLBACK_PROCESSING_MANIFEST = MOCK_PROCESSING_MANIFEST;

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function getRuntimeMode(): RuntimeMode {
  return isTauriRuntime() ? "tauri" : "mock";
}

function buildMockPath(directory: string, fileName: string): string {
  return `${directory}/${fileName}`;
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

function pipelinesEqual(
  left: readonly ProcessingPipelineStep[],
  right: readonly ProcessingPipelineStep[],
): boolean {
  return left.length === right.length && left.every((step, index) => step === right[index]);
}

async function runInRuntime<T>(options: {
  mock: () => T | Promise<T>;
  tauri: () => Promise<T>;
}): Promise<T> {
  return isTauriRuntime() ? options.tauri() : options.mock();
}

function toPreviewUrl(previewPath: string, runtime: RuntimeMode): string {
  return runtime === "tauri" ? convertFileSrc(previewPath) : previewPath;
}

function asOpenedStudy(
  studyId: string,
  inputPath: string,
  inputName: string,
  runtime: RuntimeMode,
  measurementScale: MeasurementScale | null | undefined,
): OpenedStudy {
  return {
    studyId,
    inputPath,
    inputName,
    measurementScale: measurementScale ?? null,
    runtime,
  };
}

function asPreviewResult(
  studyId: string,
  previewPath: string,
  runtime: RuntimeMode,
  imageSize: { width: number; height: number },
  measurementScale: MeasurementScale | null | undefined,
): PreviewResult {
  return {
    studyId,
    previewUrl: toPreviewUrl(previewPath, runtime),
    imageSize,
    measurementScale: measurementScale ?? null,
    runtime,
  };
}

function asProcessResult(
  studyId: string,
  previewPath: string,
  dicomPath: string,
  runtime: RuntimeMode,
  imageSize: { width: number; height: number },
  measurementScale: MeasurementScale | null | undefined,
  mode: string,
): ProcessResult {
  return {
    ...asPreviewResult(studyId, previewPath, runtime, imageSize, measurementScale),
    dicomPath,
    mode,
  };
}

function asToothAnalysisResult(
  studyId: string,
  previewPath: string,
  runtime: RuntimeMode,
  analysis: AnalyzeStudyCommandResult["analysis"],
  suggestedAnnotations: AnalyzeStudyCommandResult["suggestedAnnotations"],
): ToothAnalysisResult {
  return {
    studyId,
    previewUrl: toPreviewUrl(previewPath, runtime),
    analysis,
    suggestedAnnotations,
    runtime,
  };
}

function buildProcessStudyCommand(
  studyId: string,
  request: ProcessingRequest,
): ProcessStudyCommand {
  return {
    studyId,
    outputPath: request.outputPath,
    presetId: request.preset.id,
    invert: request.controls.invert && !request.preset.controls.invert,
    brightness:
      request.controls.brightness !== request.preset.controls.brightness
        ? request.controls.brightness
        : null,
    contrast:
      request.controls.contrast !== request.preset.controls.contrast
        ? request.controls.contrast
        : null,
    equalize: request.controls.equalize && !request.preset.controls.equalize,
    compare: request.compare,
    pipeline: pipelinesEqual(request.pipeline, DEFAULT_PIPELINE) ? null : request.pipeline,
    palette:
      request.controls.palette !== request.preset.controls.palette
        ? request.controls.palette
        : null,
  };
}

function normalizeBackendError(error: unknown): BackendError {
  if (error && typeof error === "object") {
    const candidate = error as Partial<BackendError>;
    if (typeof candidate.message === "string" && typeof candidate.code === "string") {
      return {
        code: candidate.code,
        message: candidate.message,
        details: Array.isArray(candidate.details)
          ? candidate.details.filter((entry): entry is string => typeof entry === "string")
          : [],
        recoverable: Boolean(candidate.recoverable),
      };
    }
  }

  if (error instanceof Error && error.message.trim()) {
    return {
      code: "internal",
      message: error.message,
      details: [],
      recoverable: false,
    };
  }

  if (typeof error === "string" && error.trim()) {
    return {
      code: "internal",
      message: error,
      details: [],
      recoverable: false,
    };
  }

  return {
    code: "internal",
    message: "Unexpected backend error",
    details: [],
    recoverable: false,
  };
}

export function formatBackendError(
  error: BackendError | unknown,
  fallback = "Unexpected backend error.",
): string {
  const normalized = normalizeBackendError(error);
  return normalized.message.trim() || fallback;
}

function normalizeJobResultPayload(
  result: NonNullable<ContractJobSnapshot["result"]>,
): JobResultPayload {
  const runtime = getRuntimeMode();

  switch (result.kind) {
    case "renderStudy":
      return {
        kind: "renderStudy",
        payload: asPreviewResult(
          result.payload.studyId,
          result.payload.previewPath,
          runtime,
          {
            width: result.payload.loadedWidth,
            height: result.payload.loadedHeight,
          },
          result.payload.measurementScale,
        ),
      };
    case "processStudy":
      return {
        kind: "processStudy",
        payload: asProcessResult(
          result.payload.studyId,
          result.payload.previewPath,
          result.payload.dicomPath,
          runtime,
          {
            width: result.payload.loadedWidth,
            height: result.payload.loadedHeight,
          },
          result.payload.measurementScale,
          result.payload.mode,
        ),
      };
    case "analyzeStudy":
      return {
        kind: "analyzeStudy",
        payload: asToothAnalysisResult(
          result.payload.studyId,
          result.payload.previewPath,
          runtime,
          result.payload.analysis,
          result.payload.suggestedAnnotations,
        ),
      };
  }
}

function normalizeJobSnapshot(snapshot: ContractJobSnapshot): JobSnapshot {
  return {
    jobId: snapshot.jobId,
    jobKind: snapshot.jobKind,
    studyId: snapshot.studyId ?? null,
    state: snapshot.state,
    progress: snapshot.progress,
    fromCache: snapshot.fromCache,
    result: snapshot.result ? normalizeJobResultPayload(snapshot.result) : null,
    error: snapshot.error ? normalizeBackendError(snapshot.error) : null,
  };
}

function emitMockJob(snapshot: JobSnapshot) {
  mockJobs.set(snapshot.jobId, snapshot);
  for (const listener of mockJobListeners) {
    listener(snapshot);
  }
}

function buildMockJobSnapshot(
  jobId: string,
  jobKind: JobSnapshot["jobKind"],
  studyId: string,
): JobSnapshot {
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
  updater: (job: JobSnapshot) => JobSnapshot,
): JobSnapshot {
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
  emitMockJob(next);
  return next;
}

function scheduleMockCompletion(
  jobId: string,
  resultFactory: () => JobResultPayload,
) {
  const controller = { cancelled: false };
  mockJobControllers.set(jobId, controller);
  const steps: Array<{ delay: number; state: JobState; percent: number; stage: string; message: string }> = [
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
    const result = resultFactory();
    updateMockJob(jobId, (job) => ({
      ...job,
      state: "completed",
      progress: {
        percent: 100,
        stage: "completed",
        message: "Completed",
      },
      result,
      error: null,
    }));
  }, 520);
}

function startMockJob(
  jobKind: JobSnapshot["jobKind"],
  studyId: string,
  resultFactory: () => JobResultPayload,
): StartedJob {
  const jobId = nextMockJobId();
  emitMockJob(buildMockJobSnapshot(jobId, jobKind, studyId));
  scheduleMockCompletion(jobId, resultFactory);
  return { jobId };
}

async function invokeWithBackendError<T>(
  command: string,
  args?: Record<string, unknown>,
): Promise<T> {
  try {
    return await invoke<T>(command, args);
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export async function loadProcessingManifest(): Promise<ProcessingManifest> {
  return runInRuntime({
    mock: () => MOCK_PROCESSING_MANIFEST,
    tauri: () => invokeWithBackendError<ProcessingManifest>("get_processing_manifest"),
  });
}

export async function pickDicomFile(): Promise<string | null> {
  return runInRuntime({
    mock: () => MOCK_DICOM_PATH,
    tauri: () => invokeWithBackendError<string | null>("pick_dicom_file"),
  });
}

export async function pickSaveDicomPath(defaultName: string): Promise<string | null> {
  return runInRuntime({
    mock: () => buildMockPath(MOCK_EXPORT_DIRECTORY, defaultName),
    tauri: () =>
      invokeWithBackendError<string | null>("pick_save_dicom_path", { defaultName }),
  });
}

export async function openStudy(inputPath: string): Promise<OpenedStudy> {
  return runInRuntime({
    mock: () =>
      asOpenedStudy(
        nextMockStudyId(),
        inputPath,
        fileNameFromPath(inputPath),
        getRuntimeMode(),
        null,
      ),
    tauri: async () => {
      const request: OpenStudyCommand = { inputPath };
      const payload = await invokeWithBackendError<OpenStudyCommandResult>("open_study", {
        request,
      });
      return asOpenedStudy(
        payload.study.studyId,
        payload.study.inputPath,
        payload.study.inputName,
        getRuntimeMode(),
        payload.study.measurementScale,
      );
    },
  });
}

export async function startRenderStudyJob(studyId: string): Promise<StartedJob> {
  return runInRuntime({
    mock: () =>
      startMockJob("renderStudy", studyId, () => ({
        kind: "renderStudy",
        payload: asPreviewResult(
          studyId,
          createMockPreview(false, "none"),
          getRuntimeMode(),
          {
            width: 1200,
            height: 820,
          },
          null,
        ),
      })),
    tauri: async () => {
      const request: RenderStudyCommand = { studyId };
      return invokeWithBackendError<StartedJob>("start_render_job", { request });
    },
  });
}

export function buildProcessingArgs(
  inputPath: string,
  request: ProcessingRequest,
): string[] {
  const args = ["--input", inputPath, "--preset", request.preset.id];

  if (request.outputPath) {
    args.push("--output", request.outputPath);
  }

  if (request.controls.invert && !request.preset.controls.invert) {
    args.push("--invert");
  }
  if (request.controls.brightness !== request.preset.controls.brightness) {
    args.push("--brightness", String(request.controls.brightness));
  }
  if (request.controls.contrast !== request.preset.controls.contrast) {
    args.push("--contrast", String(request.controls.contrast));
  }
  if (request.controls.equalize && !request.preset.controls.equalize) {
    args.push("--equalize");
  }
  if (request.controls.palette !== request.preset.controls.palette) {
    args.push("--palette", request.controls.palette);
  }
  if (request.compare) {
    args.push("--compare");
  }
  if (!pipelinesEqual(request.pipeline, DEFAULT_PIPELINE)) {
    args.push("--pipeline", request.pipeline.join(","));
  }

  return args;
}

export async function startProcessStudyJob(
  studyId: string,
  request: ProcessingRequest,
): Promise<StartedJob> {
  const command = buildProcessStudyCommand(studyId, request);

  return runInRuntime({
    mock: () =>
      startMockJob("processStudy", studyId, () => ({
        kind: "processStudy",
        payload: asProcessResult(
          studyId,
          createMockPreview(true, request.controls.palette),
          request.outputPath ?? MOCK_PROCESSED_DICOM_PATH,
          getRuntimeMode(),
          {
            width: 1200,
            height: 820,
          },
          null,
          request.compare ? "comparison output" : "processed preview",
        ),
      })),
    tauri: async () =>
      invokeWithBackendError<StartedJob>("start_process_job", { request: command }),
  });
}

export async function startAnalyzeStudyJob(studyId: string): Promise<StartedJob> {
  return runInRuntime({
    mock: () =>
      startMockJob("analyzeStudy", studyId, () => ({
        kind: "analyzeStudy",
        payload: {
          studyId,
          previewUrl: createMockPreview(false, "none"),
          analysis: createMockToothAnalysis(),
          suggestedAnnotations: createMockSuggestedAnnotations(),
          runtime: getRuntimeMode(),
        },
      })),
    tauri: async () => {
      const request: AnalyzeStudyCommand = { studyId };
      return invokeWithBackendError<StartedJob>("start_analyze_job", { request });
    },
  });
}

export async function getJob(jobId: string): Promise<JobSnapshot> {
  return runInRuntime({
    mock: () => {
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
    tauri: async () => {
      const request: JobCommand = { jobId };
      const snapshot = await invokeWithBackendError<ContractJobSnapshot>("get_job", { request });
      return normalizeJobSnapshot(snapshot);
    },
  });
}

export async function cancelJob(jobId: string): Promise<JobSnapshot> {
  return runInRuntime({
    mock: () => {
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
        }, 30);
      }

      return cancelling;
    },
    tauri: async () => {
      const request: JobCommand = { jobId };
      const snapshot = await invokeWithBackendError<ContractJobSnapshot>("cancel_job", {
        request,
      });
      return normalizeJobSnapshot(snapshot);
    },
  });
}

export async function measureLineAnnotation(
  studyId: string,
  annotation: LineAnnotation,
): Promise<LineAnnotation> {
  return runInRuntime({
    mock: () => measureMockLineAnnotation(annotation),
    tauri: async () => {
      const request: MeasureLineAnnotationCommand = {
        studyId,
        annotation,
      };
      const payload = await invokeWithBackendError<MeasureLineAnnotationCommandResult>(
        "measure_line_annotation",
        { request },
      );
      return payload.annotation;
    },
  });
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
