import type { JobResultPayload, JobSnapshot } from "../features/jobs/model";
import { buildDesktopPreviewUrl, isDesktopRuntime } from "./desktop";
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
import type { BackendAPI, RuntimeAdapter, ShellAPI } from "./runtimeTypes";
import { createDesktopShellAPI, createMockShellAPI } from "./shell";
import type {
  AnalysisResult,
  OpenedStudy,
  PreviewResult,
  ProcessResult,
  RuntimeMode,
} from "./types";

export const FALLBACK_PROCESSING_MANIFEST = MOCK_PROCESSING_MANIFEST;

function resolvePreviewUrl(previewPath: string, runtime: RuntimeMode): string {
  if (runtime === "desktop") {
    return buildDesktopPreviewUrl(previewPath);
  }

  return previewPath;
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

  let shell: ShellAPI;
  let backend: BackendAPI;
  switch (mode) {
    case "mock":
      shell = createMockShellAPI();
      backend = createMockBackendAPI();
      break;
    default:
      shell = createDesktopShellAPI();
      backend = createDesktopBackendAPI();
      break;
  }

  return {
    mode,
    shell,
    backend,
    loadProcessingManifest: () => backend.loadProcessingManifest(),
    pickBmpFile: () => shell.pickBmpFile(),
    openStudy: async (inputPath) => asOpenedStudy(await backend.openStudy(inputPath), mode),
    startRenderStudyJob: (studyId) => backend.startRenderStudyJob(studyId),
    startAnalyzeStudyJob: (studyId) => backend.startAnalyzeStudyJob(studyId),
    startProcessStudyJob: (studyId, request) => backend.startProcessStudyJob(studyId, request),
    getJob: async (jobId) => normalizeJobSnapshot(await backend.getJob(jobId), mode),
    getJobs: async (jobIds) => {
      const snapshots = await backend.getJobs(jobIds);
      const jobs = new Array<JobSnapshot>(snapshots.length);
      for (let index = 0; index < snapshots.length; index += 1) {
        jobs[index] = normalizeJobSnapshot(snapshots[index], mode);
      }
      return jobs;
    },
    forEachJob: async (jobIds, visitor) => {
      const snapshots = await backend.getJobs(jobIds);
      for (const snapshot of snapshots) {
        visitor(normalizeJobSnapshot(snapshot, mode));
      }
    },
    cancelJob: async (jobId) => normalizeJobSnapshot(await backend.cancelJob(jobId), mode),
    measureLineAnnotation: (studyId, annotation) =>
      backend.measureLineAnnotation(studyId, annotation),
  };
}

let activeRuntime: RuntimeAdapter | null = null;
let loggedRuntimeConfiguration = false;

export function getRuntimeAdapter(): RuntimeAdapter {
  if (!activeRuntime) {
    const configuration = resolveRuntimeConfiguration(isDesktopRuntime());
    activeRuntime = createRuntimeAdapter(configuration);

    if (!loggedRuntimeConfiguration) {
      for (const warning of configuration.warnings) {
        console.warn("[xrayview] runtime configuration:", warning);
      }

      console.info(
        `[xrayview] backend runtime: ${configuration.mode} (${configuration.selectionSource})`,
      );
      loggedRuntimeConfiguration = true;
    }
  }

  return activeRuntime;
}
