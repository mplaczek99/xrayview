import type { JobResultPayload, JobSnapshot } from "../features/jobs/model";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  createGoSidecarBackendAPI,
  createLegacyRustBackendAPI,
  createMockBackendAPI,
  ensureDicomExtension,
  paletteLabel,
} from "./backend";
import type {
  AnalyzeStudyCommandResult,
  BackendError,
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
import {
  resolveRuntimeConfiguration,
  type RuntimeConfiguration,
} from "./runtimeConfig";
import { createMockShellAPI, createTauriShellAPI } from "./shell";
import type { BackendAPI, RuntimeAdapter, ShellAPI } from "./runtimeTypes";
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

type BackendOwner = Exclude<RuntimeMode, "mock">;

interface JobRoute {
  owner: BackendOwner;
  frontendStudyId: string | null;
}

interface StudyRoute {
  inputPath: string;
  rustStudyId: string | null;
}

function createNotFoundError(message: string): BackendError {
  return {
    code: "notFound",
    message,
    details: [],
    recoverable: true,
  };
}

function remapJobResult(
  result: JobResult,
  frontendStudyId: string | null,
): JobResult {
  switch (result.kind) {
    case "renderStudy":
      return {
        kind: "renderStudy",
        payload: {
          ...result.payload,
          studyId: frontendStudyId ?? result.payload.studyId,
        },
      };
    case "processStudy":
      return {
        kind: "processStudy",
        payload: {
          ...result.payload,
          studyId: frontendStudyId ?? result.payload.studyId,
        },
      };
    case "analyzeStudy":
      return {
        kind: "analyzeStudy",
        payload: {
          ...result.payload,
          studyId: frontendStudyId ?? result.payload.studyId,
        },
      };
  }
}

function createLegacyDesktopRuntimeAdapter(
  configuration: RuntimeConfiguration,
): RuntimeAdapter {
  const mode: RuntimeMode = "legacy-rust";
  const shell = createTauriShellAPI();
  const rustBackend = createLegacyRustBackendAPI();
  const goBackend = createGoSidecarBackendAPI(configuration.goSidecarBaseUrl);
  const studyRoutes = new Map<string, StudyRoute>();
  const pendingRustStudyRegistrations = new Map<string, Promise<string>>();
  const jobRoutes = new Map<string, JobRoute>();

  function trackJob(
    jobId: string,
    owner: BackendOwner,
    frontendStudyId: string | null,
  ) {
    jobRoutes.set(jobId, { owner, frontendStudyId });
  }

  async function ensureRustStudyId(frontendStudyId: string): Promise<string> {
    const route = studyRoutes.get(frontendStudyId);
    if (!route) {
      throw createNotFoundError(`study not found: ${frontendStudyId}`);
    }

    if (route.rustStudyId) {
      return route.rustStudyId;
    }

    const pending = pendingRustStudyRegistrations.get(frontendStudyId);
    if (pending) {
      return pending;
    }

    const registration = (async () => {
      const payload = await rustBackend.openStudy(route.inputPath);
      const rustStudyId = payload.study.studyId;
      studyRoutes.set(frontendStudyId, {
        ...route,
        rustStudyId,
      });
      return rustStudyId;
    })();

    pendingRustStudyRegistrations.set(frontendStudyId, registration);

    try {
      return await registration;
    } finally {
      pendingRustStudyRegistrations.delete(frontendStudyId);
    }
  }

  function remapJobSnapshot(
    snapshot: ContractJobSnapshot,
    frontendStudyId: string | null,
  ): ContractJobSnapshot {
    const mappedStudyId = frontendStudyId ?? snapshot.studyId ?? null;

    return {
      ...snapshot,
      studyId: mappedStudyId,
      result: snapshot.result
        ? remapJobResult(snapshot.result, mappedStudyId)
        : null,
    };
  }

  function runtimeForJob(jobId: string): BackendOwner {
    return jobRoutes.get(jobId)?.owner ?? "legacy-rust";
  }

  const backend: BackendAPI = {
    mode,
    loadProcessingManifest: () => rustBackend.loadProcessingManifest(),
    openStudy: async (inputPath) => {
      const payload = await goBackend.openStudy(inputPath);
      studyRoutes.set(payload.study.studyId, {
        inputPath: payload.study.inputPath,
        rustStudyId: null,
      });
      return payload;
    },
    startRenderStudyJob: async (studyId) => {
      const rustStudyId = await ensureRustStudyId(studyId);
      const started = await rustBackend.startRenderStudyJob(rustStudyId);
      trackJob(started.jobId, "legacy-rust", studyId);
      return started;
    },
    startProcessStudyJob: async (studyId, request) => {
      const started = await goBackend.startProcessStudyJob(studyId, request);
      trackJob(started.jobId, "go-sidecar", studyId);
      return started;
    },
    startAnalyzeStudyJob: async (studyId) => {
      const started = await goBackend.startAnalyzeStudyJob(studyId);
      trackJob(started.jobId, "go-sidecar", studyId);
      return started;
    },
    getJob: async (jobId) => {
      const route = jobRoutes.get(jobId);
      if (route?.owner === "go-sidecar") {
        return remapJobSnapshot(await goBackend.getJob(jobId), route.frontendStudyId);
      }

      return remapJobSnapshot(await rustBackend.getJob(jobId), route?.frontendStudyId ?? null);
    },
    cancelJob: async (jobId) => {
      const route = jobRoutes.get(jobId);
      if (route?.owner === "go-sidecar") {
        return remapJobSnapshot(await goBackend.cancelJob(jobId), route.frontendStudyId);
      }

      return remapJobSnapshot(
        await rustBackend.cancelJob(jobId),
        route?.frontendStudyId ?? null,
      );
    },
    measureLineAnnotation: async (studyId, annotation) =>
      goBackend.measureLineAnnotation(studyId, annotation),
  };

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
      normalizeJobSnapshot(await backend.getJob(jobId), runtimeForJob(jobId), shell),
    cancelJob: async (jobId) =>
      normalizeJobSnapshot(await backend.cancelJob(jobId), runtimeForJob(jobId), shell),
    measureLineAnnotation: (studyId, annotation) =>
      backend.measureLineAnnotation(studyId, annotation),
  };
}

function createRuntimeAdapter(configuration: RuntimeConfiguration): RuntimeAdapter {
  const { mode } = configuration;
  if (mode === "legacy-rust") {
    return createLegacyDesktopRuntimeAdapter(configuration);
  }

  const shell = mode === "mock" ? createMockShellAPI() : createTauriShellAPI();
  const backend =
    mode === "mock"
      ? createMockBackendAPI()
      : createGoSidecarBackendAPI(configuration.goSidecarBaseUrl);

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
let loggedRuntimeConfiguration = false;

export function getRuntimeAdapter(): RuntimeAdapter {
  if (!activeRuntime) {
    const configuration = resolveRuntimeConfiguration(isTauriRuntime());
    activeRuntime = createRuntimeAdapter(configuration);

    if (!loggedRuntimeConfiguration) {
      for (const warning of configuration.warnings) {
        console.warn("[xrayview] runtime configuration:", warning);
      }

      const description =
        configuration.mode === "go-sidecar"
          ? `${configuration.mode} (${configuration.goSidecarBaseUrl})`
          : configuration.mode === "legacy-rust"
            ? `${configuration.mode} fallback + go-sidecar(openStudy+processStudy+analyzeStudy+measureLineAnnotation @ ${configuration.goSidecarBaseUrl})`
            : configuration.mode;
      console.info(
        `[xrayview] backend runtime: ${description} (${configuration.selectionSource})`,
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
