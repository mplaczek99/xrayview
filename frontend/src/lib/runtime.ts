import type { JobResultPayload, JobSnapshot } from "../features/jobs/model";
import { createDesktopBackendAPI } from "./desktopBackend";
import type {
  AnalyzeStudyCommandResult,
  JobSnapshot as ContractJobSnapshot,
  JobResult,
  OpenStudyCommandResult,
  ProcessStudyCommandResult,
  RenderStudyCommandResult,
} from "./generated/contracts";
import { createMockBackendAPI } from "./mockBackend";
import { MOCK_PROCESSING_MANIFEST } from "./mockProcessingManifest";
import { resolveRuntimeConfiguration } from "./runtimeConfig";
import type { RuntimeAdapter } from "./runtimeTypes";
import type {
  AnalysisResult,
  OpenedStudy,
  PreviewResult,
  ProcessResult,
  RuntimeMode,
} from "./types";
import { isWailsRuntime, pickBmpFile as pickDesktopBmpFile } from "./wails";

export const FALLBACK_PROCESSING_MANIFEST = MOCK_PROCESSING_MANIFEST;

const MOCK_BMP_PATH = "images/BMP/1.bmp";

function resolvePreviewUrl(previewPath: string, runtime: RuntimeMode): string {
  // Desktop previews are streamed by the Wails asset handler (desktop/assets.go);
  // the browser mock serves the path as-is.
  return runtime === "desktop" ? `/previews?path=${encodeURIComponent(previewPath)}` : previewPath;
}

function asOpenedStudy(payload: OpenStudyCommandResult, runtime: RuntimeMode): OpenedStudy {
  return {
    studyId: payload.study.studyId,
    inputPath: payload.study.inputPath,
    inputName: payload.study.inputName,
    measurementScale: payload.study.measurementScale ?? null,
    runtime,
  };
}

function asPreviewResult(payload: RenderStudyCommandResult, runtime: RuntimeMode): PreviewResult {
  return {
    studyId: payload.studyId,
    previewUrl: resolvePreviewUrl(payload.previewPath, runtime),
    imageSize: {
      width: payload.loadedWidth,
      height: payload.loadedHeight,
    },
    measurementScale: payload.measurementScale ?? null,
    runtime,
  };
}

function asProcessResult(payload: ProcessStudyCommandResult, runtime: RuntimeMode): ProcessResult {
  return {
    ...asPreviewResult(payload, runtime),
    mode: payload.mode,
  };
}

function asAnalysisResult(
  payload: AnalyzeStudyCommandResult,
  runtime: RuntimeMode,
): AnalysisResult {
  return {
    ...asPreviewResult(payload, runtime),
    filledPreviewUrl: resolvePreviewUrl(payload.filledPreviewPath, runtime),
    mode: payload.mode,
  };
}

function normalizeJobResultPayload(result: JobResult, runtime: RuntimeMode): JobResultPayload {
  switch (result.kind) {
    case "renderStudy":
      return {
        kind: "renderStudy",
        payload: asPreviewResult(result.payload, runtime),
      };
    case "analyzeStudy":
      return {
        kind: "analyzeStudy",
        payload: asAnalysisResult(result.payload, runtime),
      };
    case "processStudy":
      return {
        kind: "processStudy",
        payload: asProcessResult(result.payload, runtime),
      };
  }
}

export function normalizeJobSnapshot(
  snapshot: ContractJobSnapshot,
  runtime: RuntimeMode,
): JobSnapshot {
  return {
    jobId: snapshot.jobId,
    jobKind: snapshot.jobKind,
    studyId: snapshot.studyId ?? null,
    state: snapshot.state,
    progress: snapshot.progress,
    fromCache: snapshot.fromCache,
    result: snapshot.result ? normalizeJobResultPayload(snapshot.result, runtime) : null,
    error: snapshot.error ?? null,
    timing: null,
  };
}

function createRuntimeAdapter(
  configuration: ReturnType<typeof resolveRuntimeConfiguration>,
): RuntimeAdapter {
  const { mode } = configuration;
  const backend = mode === "mock" ? createMockBackendAPI() : createDesktopBackendAPI();
  const pickBmpFile = mode === "mock" ? async () => MOCK_BMP_PATH : () => pickDesktopBmpFile();

  return {
    mode,
    loadProcessingManifest: () => backend.loadProcessingManifest(),
    pickBmpFile,
    openStudy: async (inputPath) => asOpenedStudy(await backend.openStudy(inputPath), mode),
    startRenderStudyJob: (studyId) => backend.startRenderStudyJob(studyId),
    startAnalyzeStudyJob: (studyId) => backend.startAnalyzeStudyJob(studyId),
    startProcessStudyJob: (studyId, request) => backend.startProcessStudyJob(studyId, request),
    getJob: async (jobId) => normalizeJobSnapshot(await backend.getJob(jobId), mode),
    getJobs: async (jobIds) => {
      const snapshots = await backend.getJobs(jobIds);
      return snapshots.map((snapshot) => normalizeJobSnapshot(snapshot, mode));
    },
    cancelJob: async (jobId) => normalizeJobSnapshot(await backend.cancelJob(jobId), mode),
    measureLineAnnotation: (studyId, annotation) =>
      backend.measureLineAnnotation(studyId, annotation),
    setStudyCalibration: (studyId, reference) => backend.setStudyCalibration(studyId, reference),
  };
}

let activeRuntime: RuntimeAdapter | null = null;

export function getRuntimeAdapter(): RuntimeAdapter {
  if (!activeRuntime) {
    const configuration = resolveRuntimeConfiguration(isWailsRuntime());
    activeRuntime = createRuntimeAdapter(configuration);

    for (const warning of configuration.warnings) {
      console.warn("[xrayview] runtime configuration:", warning);
    }
    console.info(
      `[xrayview] backend runtime: ${configuration.mode} (${configuration.selectionSource})`,
    );
  }

  return activeRuntime;
}
