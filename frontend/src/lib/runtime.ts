import type { JobResultPayload, JobSnapshot } from "../features/jobs/model";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  createMockBackendAPI,
  createTauriBackendAPI,
  ensureDicomExtension,
  paletteLabel,
} from "./backend";
import type {
  AnalyzeStudyCommandResult,
  JobResult,
  JobSnapshot as ContractJobSnapshot,
  OpenStudyCommandResult,
  ProcessStudyCommandResult,
  RenderStudyCommandResult,
} from "./generated/contracts";
import {
  buildMockPath,
  MOCK_DICOM_PATH,
  MOCK_EXPORT_DIRECTORY,
} from "./mockRuntime";
import { createMockShellAPI, createTauriShellAPI } from "./shell";
import type { RuntimeAdapter, ShellAPI } from "./runtimeTypes";
import type {
  OpenedStudy,
  PreviewResult,
  ProcessResult,
  ProcessingRequest,
  RuntimeMode,
  ToothAnalysisResult,
} from "./types";

function isTauriRuntime(): boolean {
  return typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;
}

function detectRuntimeMode(): RuntimeMode {
  return isTauriRuntime() ? "tauri" : "mock";
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
  shell: ShellAPI,
): PreviewResult {
  return {
    studyId: payload.studyId,
    previewUrl: shell.resolvePreviewUrl(payload.previewPath),
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
  shell: ShellAPI,
): ProcessResult {
  return {
    ...asPreviewResult(payload, runtime, shell),
    dicomPath: payload.dicomPath,
    mode: payload.mode,
  };
}

function asToothAnalysisResult(
  payload: AnalyzeStudyCommandResult,
  runtime: RuntimeMode,
  shell: ShellAPI,
): ToothAnalysisResult {
  return {
    studyId: payload.studyId,
    previewUrl: shell.resolvePreviewUrl(payload.previewPath),
    analysis: payload.analysis,
    suggestedAnnotations: payload.suggestedAnnotations,
    runtime,
  };
}

function normalizeJobResultPayload(
  result: JobResult,
  runtime: RuntimeMode,
  shell: ShellAPI,
): JobResultPayload {
  switch (result.kind) {
    case "renderStudy":
      return {
        kind: "renderStudy",
        payload: asPreviewResult(result.payload, runtime, shell),
      };
    case "processStudy":
      return {
        kind: "processStudy",
        payload: asProcessResult(result.payload, runtime, shell),
      };
    case "analyzeStudy":
      return {
        kind: "analyzeStudy",
        payload: asToothAnalysisResult(result.payload, runtime, shell),
      };
  }
}

function normalizeJobSnapshot(
  snapshot: ContractJobSnapshot,
  runtime: RuntimeMode,
  shell: ShellAPI,
): JobSnapshot {
  return {
    jobId: snapshot.jobId,
    jobKind: snapshot.jobKind,
    studyId: snapshot.studyId ?? null,
    state: snapshot.state,
    progress: snapshot.progress,
    fromCache: snapshot.fromCache,
    result: snapshot.result
      ? normalizeJobResultPayload(snapshot.result, runtime, shell)
      : null,
    error: snapshot.error ?? null,
    timing: null,
  };
}

function createRuntimeAdapter(mode: RuntimeMode): RuntimeAdapter {
  const shell = mode === "tauri" ? createTauriShellAPI() : createMockShellAPI();
  const backend = mode === "tauri" ? createTauriBackendAPI() : createMockBackendAPI();

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
      normalizeJobSnapshot(await backend.getJob(jobId), mode, shell),
    cancelJob: async (jobId) =>
      normalizeJobSnapshot(await backend.cancelJob(jobId), mode, shell),
    measureLineAnnotation: (studyId, annotation) =>
      backend.measureLineAnnotation(studyId, annotation),
  };
}

let activeRuntime: RuntimeAdapter | null = null;

export function getRuntimeAdapter(): RuntimeAdapter {
  if (!activeRuntime) {
    activeRuntime = createRuntimeAdapter(detectRuntimeMode());
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
