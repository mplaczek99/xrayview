import type {
  AnalyzeStudyCommand,
  AnalyzeStudyCommandResult,
  JobSnapshot as ContractJobSnapshot,
  JobCommand,
  LineAnnotation,
  MeasureLineAnnotationCommand,
  MeasureLineAnnotationCommandResult,
  OpenStudyCommand,
  OpenStudyCommandResult,
  ProcessStudyCommand,
  ProcessStudyCommandResult,
  ProcessingManifest,
  RenderStudyCommand,
  RenderStudyCommandResult,
  StartedJob,
} from "./generated/contracts";
import { normalizeBackendError } from "./backendErrors";
import { buildProcessStudyCommand } from "./commandBuilders";
import type { BackendAPI } from "./runtimeTypes";
import type { ProcessingRequest } from "./types";

// httpBackend dispatches backend commands over the loopback HTTP API. It
// mirrors the desktop adapter shape so the runtime can swap between modes
// without touching the store. Only the browser-hosted agent harness uses
// this path today — the packaged desktop shell continues to go through
// the Wails JS bridge.
const COMMANDS_PATH = "/api/v1/commands";

type Fetch = typeof fetch;

async function invokeCommand<TRequest, TResponse>(
  baseUrl: string,
  command: string,
  payload: TRequest,
  fetchImpl: Fetch,
): Promise<TResponse> {
  const url = `${baseUrl}${COMMANDS_PATH}/${command}`;

  let response: Response;
  try {
    response = await fetchImpl(url, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(payload ?? {}),
    });
  } catch (error) {
    throw normalizeBackendError(error);
  }

  const rawBody = await response.text();
  let parsed: unknown = null;
  if (rawBody.length > 0) {
    try {
      parsed = JSON.parse(rawBody);
    } catch (error) {
      throw normalizeBackendError(
        new Error(`invalid JSON from backend for ${command}: ${(error as Error).message}`),
      );
    }
  }

  if (!response.ok) {
    // The backend always returns a BackendError-shaped body on failure;
    // pass it through normalizeBackendError so the frontend sees the same
    // shape the desktop adapter produces.
    throw normalizeBackendError(
      parsed ?? `backend responded ${response.status} for ${command}`,
    );
  }

  return parsed as TResponse;
}

function trimTrailingSlash(baseUrl: string): string {
  return baseUrl.replace(/\/+$/, "");
}

export function createHttpBackendAPI(
  baseUrl: string,
  fetchImpl: Fetch = fetch,
): BackendAPI {
  const root = trimTrailingSlash(baseUrl);
  const call = <TRequest, TResponse>(command: string, payload: TRequest) =>
    invokeCommand<TRequest, TResponse>(root, command, payload, fetchImpl);

  return {
    mode: "http",
    loadProcessingManifest: () =>
      call<Record<string, never>, ProcessingManifest>(
        "get_processing_manifest",
        {},
      ),
    openStudy: (inputPath) =>
      call<OpenStudyCommand, OpenStudyCommandResult>("open_study", { inputPath }),
    startRenderStudyJob: (studyId) =>
      call<RenderStudyCommand, StartedJob>("start_render_job", { studyId }),
    startProcessStudyJob: (studyId, request: ProcessingRequest) =>
      call<ProcessStudyCommand, StartedJob>(
        "start_process_job",
        buildProcessStudyCommand(studyId, request),
      ),
    startAnalyzeStudyJob: (studyId) =>
      call<AnalyzeStudyCommand, StartedJob>("start_analyze_job", { studyId }),
    getJob: (jobId) =>
      call<JobCommand, ContractJobSnapshot>("get_job", { jobId }),
    getJobs: (jobIds) =>
      call<{ jobIds: string[] }, ContractJobSnapshot[]>("get_jobs", {
        jobIds: [...new Set(jobIds)],
      }),
    cancelJob: (jobId) =>
      call<JobCommand, ContractJobSnapshot>("cancel_job", { jobId }),
    measureLineAnnotation: async (
      studyId,
      annotation,
    ): Promise<LineAnnotation> => {
      const payload = await call<
        MeasureLineAnnotationCommand,
        MeasureLineAnnotationCommandResult
      >("measure_line_annotation", { studyId, annotation });
      return payload.annotation;
    },
  };
}

// Re-exports kept explicit so TypeScript can verify the response types line
// up with the contract generator output. These suppressions are intentional:
// without a reference the imports would be trimmed and reintroduced on the
// next edit, causing drift.
export type {
  AnalyzeStudyCommandResult,
  ProcessStudyCommandResult,
  RenderStudyCommandResult,
};
