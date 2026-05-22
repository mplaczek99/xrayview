import { invoke } from "@tauri-apps/api/core";
import type {
  AnalyzeStudyCommand,
  JobCommand,
  JobSnapshot,
  LineAnnotation,
  MeasureLineAnnotationCommand,
  MeasureLineAnnotationCommandResult,
  OpenStudyCommand,
  OpenStudyCommandResult,
  ProcessStudyCommand,
  ProcessingManifest,
  RenderStudyCommand,
  StartedJob,
} from "./generated/contracts";
import { normalizeBackendError } from "./backendErrors";
import { buildProcessStudyCommand } from "./commandBuilders";
import { uniqueJobIds } from "./jobIds";
import type { BackendAPI } from "./runtimeTypes";

async function invokeCommand<TResult>(
  command: string,
  args: Record<string, unknown> = {},
): Promise<TResult> {
  try {
    return await invoke<TResult>(command, args);
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export function createDesktopBackendAPI(): BackendAPI {
  return {
    mode: "desktop",
    loadProcessingManifest: () =>
      invokeCommand<ProcessingManifest>("get_processing_manifest"),
    openStudy: (inputPath) =>
      invokeCommand<OpenStudyCommandResult>("open_study", {
        command: { inputPath } satisfies OpenStudyCommand,
      }),
    startRenderStudyJob: (studyId) =>
      invokeCommand<StartedJob>("start_render_job", {
        command: { studyId } satisfies RenderStudyCommand,
      }),
    startAnalyzeStudyJob: (studyId) =>
      invokeCommand<StartedJob>("start_analyze_job", {
        command: { studyId } satisfies AnalyzeStudyCommand,
      }),
    startProcessStudyJob: (studyId, request) =>
      invokeCommand<StartedJob>("start_process_job", {
        command: buildProcessStudyCommand(studyId, request) satisfies ProcessStudyCommand,
      }),
    getJob: (jobId) =>
      invokeCommand<JobSnapshot>("get_job", {
        command: { jobId } satisfies JobCommand,
      }),
    getJobs: (jobIds) =>
      invokeCommand<JobSnapshot[]>("get_jobs", {
        command: { jobIds: uniqueJobIds(jobIds) },
      }),
    cancelJob: (jobId) =>
      invokeCommand<JobSnapshot>("cancel_job", {
        command: { jobId } satisfies JobCommand,
      }),
    measureLineAnnotation: async (studyId, annotation): Promise<LineAnnotation> => {
      const payload = await invokeCommand<MeasureLineAnnotationCommandResult>(
        "measure_line_annotation",
        {
          command: { studyId, annotation } satisfies MeasureLineAnnotationCommand,
        },
      );
      return payload.annotation;
    },
  };
}
