import type { JobResultPayload, JobSnapshot } from "../features/jobs/model";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  createDesktopBackendAPI,
  createMockBackendAPI,
  ensureDicomExtension,
  paletteLabel,
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
import { resolveRuntimeConfiguration } from "./runtimeConfig";
import { createDesktopShellAPI, createMockShellAPI } from "./shell";
import type { BackendAPI, RuntimeAdapter } from "./runtimeTypes";
import {
  buildMockPath,
  MOCK_DICOM_PATH,
  MOCK_EXPORT_DIRECTORY,
} from "./mockRuntime";
import type {
  OpenedStudy,
  PreviewResult,
  ProcessResult,
  ProcessingRequest,
  RuntimeMode,
  ToothAnalysisResult,
} from "./types";

function resolvePreviewUrl(
  previewPath: string,
  runtime: RuntimeMode,
): string {
  return runtime === "desktop"
    ? buildDesktopPreviewUrl(previewPath)
    : previewPath;
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

function normalizeJobSnapshot(
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
  const shell = mode === "mock" ? createMockShellAPI() : createDesktopShellAPI();
  const backend: BackendAPI =
    mode === "mock"
      ? createMockBackendAPI()
      : createDesktopBackendAPI();

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

export {
  FALLBACK_PROCESSING_MANIFEST,
  buildMockPath,
  buildOutputName,
  ensureDicomExtension,
  MOCK_DICOM_PATH,
  MOCK_EXPORT_DIRECTORY,
  paletteLabel,
};
export type { BackendAPI, RuntimeAdapter, ShellAPI } from "./runtimeTypes";
