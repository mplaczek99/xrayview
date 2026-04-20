import type { JobResultPayload, JobSnapshot } from "../features/jobs/model";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  createDesktopBackendAPI,
  createMockBackendAPI,
  ensureDicomExtension,
} from "./backend";
import { buildDesktopPreviewUrl, isDesktopRuntime } from "./desktop";
import type {
  AnalyzeStudyCommandResult,
  JobResult,
  JobSnapshot as ContractJobSnapshot,
  OpenStudyCommandResult,
  ProcessStudyCommandResult,
  RenderStudyCommandResult,
} from "./generated/contracts";
import { createHttpBackendAPI } from "./httpBackend";
import { createHttpShellAPI } from "./httpShell";
import { resolveRuntimeConfiguration } from "./runtimeConfig";
import { createDesktopShellAPI, createMockShellAPI } from "./shell";
import type { BackendAPI, RuntimeAdapter } from "./runtimeTypes";
import type {
  OpenedStudy,
  PreviewResult,
  ProcessResult,
  ProcessingRequest,
  RuntimeMode,
  ToothAnalysisResult,
} from "./types";

// activeHttpBaseUrl is set once when the http runtime adapter is created.
// resolvePreviewUrl reads it so a job snapshot produced outside of the
// adapter's closure (notably the useJobs poller) can still rebuild the
// browser-usable preview URL from a backend-supplied filesystem path.
let activeHttpBaseUrl: string | undefined;

function resolvePreviewUrl(
  previewPath: string,
  runtime: RuntimeMode,
): string {
  if (runtime === "desktop") {
    return buildDesktopPreviewUrl(previewPath);
  }

  if (runtime === "http") {
    if (!activeHttpBaseUrl) {
      // The adapter sets this during construction. If it is missing the
      // runtime wiring is broken, not the caller — surface loudly rather
      // than rendering a raw filesystem path the browser cannot load.
      throw new Error(
        "http runtime selected but base URL is unset. Is the agent harness configured?",
      );
    }

    return `${activeHttpBaseUrl}${buildDesktopPreviewUrl(previewPath)}`;
  }

  return previewPath;
}

function asOpenedStudy(
  payload: OpenStudyCommandResult,
  runtime: RuntimeMode,
): OpenedStudy {
  return {
    studyId: payload.study.studyId,
    inputPath: payload.study.inputPath,
    inputName: payload.study.inputName,
    measurementScale: payload.study.measurementScale ?? null,
    runtime,
  };
}

function asPreviewResult(
  payload: RenderStudyCommandResult,
  runtime: RuntimeMode,
): PreviewResult {
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

function asProcessResult(
  payload: ProcessStudyCommandResult,
  runtime: RuntimeMode,
): ProcessResult {
  return {
    ...asPreviewResult(payload, runtime),
    dicomPath: payload.dicomPath,
    mode: payload.mode,
  };
}

function asToothAnalysisResult(
  payload: AnalyzeStudyCommandResult,
  runtime: RuntimeMode,
): ToothAnalysisResult {
  return {
    studyId: payload.studyId,
    previewUrl: resolvePreviewUrl(payload.previewPath, runtime),
    analysis: payload.analysis,
    suggestedAnnotations: payload.suggestedAnnotations,
    runtime,
  };
}

function normalizeJobResultPayload(
  result: JobResult,
  runtime: RuntimeMode,
): JobResultPayload {
  switch (result.kind) {
    case "renderStudy":
      return {
        kind: "renderStudy",
        payload: asPreviewResult(result.payload, runtime),
      };
    case "processStudy":
      return {
        kind: "processStudy",
        payload: asProcessResult(result.payload, runtime),
      };
    case "analyzeStudy":
      return {
        kind: "analyzeStudy",
        payload: asToothAnalysisResult(result.payload, runtime),
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
    result: snapshot.result
      ? normalizeJobResultPayload(snapshot.result, runtime)
      : null,
    error: snapshot.error ?? null,
    timing: null,
  };
}

function createRuntimeAdapter(
  configuration: ReturnType<typeof resolveRuntimeConfiguration>,
): RuntimeAdapter {
  const { mode } = configuration;

  activeHttpBaseUrl =
    mode === "http" ? configuration.httpBaseUrl : undefined;

  let shell;
  let backend: BackendAPI;
  switch (mode) {
    case "mock":
      shell = createMockShellAPI();
      backend = createMockBackendAPI();
      break;
    case "http":
      if (!configuration.httpBaseUrl) {
        // Should never fire — resolveRuntimeConfiguration guarantees a base
        // URL whenever mode === "http". The branch exists so future changes
        // to the configuration shape cannot silently produce a dead adapter.
        throw new Error("http runtime selected but httpBaseUrl is unset");
      }
      shell = createHttpShellAPI();
      backend = createHttpBackendAPI(configuration.httpBaseUrl);
      break;
    case "desktop":
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
    pickDicomFile: () => shell.pickDicomFile(),
    pickSaveDicomPath: (defaultName) => shell.pickSaveDicomPath(defaultName),
    openStudy: async (inputPath) =>
      asOpenedStudy(await backend.openStudy(inputPath), mode),
    startRenderStudyJob: (studyId) => backend.startRenderStudyJob(studyId),
    startProcessStudyJob: (studyId, request) =>
      backend.startProcessStudyJob(studyId, request),
    startAnalyzeStudyJob: (studyId) => backend.startAnalyzeStudyJob(studyId),
    getJob: async (jobId) =>
      normalizeJobSnapshot(await backend.getJob(jobId), mode),
    getJobs: async (jobIds) =>
      (await backend.getJobs(jobIds)).map((s) => normalizeJobSnapshot(s, mode)),
    cancelJob: async (jobId) =>
      normalizeJobSnapshot(await backend.cancelJob(jobId), mode),
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

export { FALLBACK_PROCESSING_MANIFEST, buildOutputName, ensureDicomExtension };
