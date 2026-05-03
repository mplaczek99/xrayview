import type {
  LineAnnotation,
  OpenStudyCommandResult,
} from "./generated/contracts";
import { normalizeBackendError } from "./backendErrors";
import { buildProcessStudyCommand } from "./commandBuilders";
import type { BackendAPI } from "./runtimeTypes";
import { getWailsBindings } from "./wails";

async function invokeTypedDesktopBinding<T>(
  invoke: () => Promise<T>,
): Promise<T> {
  try {
    return await invoke();
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

export function createDesktopBackendAPI(): BackendAPI {
  const bindings = getWailsBindings();

  return {
    mode: "desktop",
    loadProcessingManifest: () =>
      invokeTypedDesktopBinding(() => bindings.GetProcessingManifest()),
    openStudy: async (inputPath): Promise<OpenStudyCommandResult> =>
      invokeTypedDesktopBinding(() => bindings.OpenStudy({ inputPath })),
    startRenderStudyJob: async (studyId) =>
      invokeTypedDesktopBinding(() => bindings.StartRenderJob({ studyId })),
    startAnalyzeStudyJob: async (studyId) =>
      invokeTypedDesktopBinding(() => bindings.StartAnalyzeJob({ studyId })),
    startProcessStudyJob: async (studyId, request) =>
      invokeTypedDesktopBinding(() =>
        bindings.StartProcessJob(buildProcessStudyCommand(studyId, request)),
      ),
    getJob: async (jobId) =>
      invokeTypedDesktopBinding(() => bindings.GetJobSnapshot({ jobId })),
    getJobs: async (jobIds) =>
      invokeTypedDesktopBinding(() => bindings.GetJobsSnapshot({ jobIds: [...new Set(jobIds)] })),
    cancelJob: async (jobId) =>
      invokeTypedDesktopBinding(() => bindings.CancelJobByID({ jobId })),
    measureLineAnnotation: async (studyId, annotation): Promise<LineAnnotation> => {
      const payload = await invokeTypedDesktopBinding(() =>
        bindings.MeasureLineAnnotation({
          studyId,
          annotation,
        }),
      );
      return payload.annotation;
    },
  };
}
