import { normalizeBackendError } from "./backendErrors";
import type {
  AnalyzeStudyCommand,
  CalibrationReference,
  GetJobsCommand,
  JobCommand,
  JobSnapshot,
  LineAnnotation,
  MeasureLineAnnotationCommand,
  MeasureLineAnnotationCommandResult,
  MeasurementScale,
  OpenStudyCommand,
  OpenStudyCommandResult,
  ProcessingManifest,
  ProcessStudyCommand,
  RenderStudyCommand,
  SetStudyCalibrationCommand,
  SetStudyCalibrationCommandResult,
  StartedJob,
} from "./generated/contracts";
import type { BackendAPI } from "./runtimeTypes";
import type { ProcessingRequest } from "./types";
import { getWailsBackend, type WailsBackend } from "./wails";

async function call<TResult>(
  invoke: (backend: WailsBackend) => Promise<unknown>,
): Promise<TResult> {
  try {
    return (await invoke(getWailsBackend())) as TResult;
  } catch (error) {
    throw normalizeBackendError(error);
  }
}

function buildProcessStudyCommand(
  studyId: string,
  request: ProcessingRequest,
): ProcessStudyCommand {
  const { controls, presetControls } = request;
  return {
    studyId,
    presetId: request.presetId,
    invert: controls.invert,
    brightness: controls.brightness !== presetControls.brightness ? controls.brightness : null,
    contrast: controls.contrast !== presetControls.contrast ? controls.contrast : null,
    equalize: controls.equalize,
    compare: request.compare,
    palette: controls.palette !== presetControls.palette ? controls.palette : null,
  };
}

export function createDesktopBackendAPI(): BackendAPI {
  return {
    mode: "desktop",
    loadProcessingManifest: () =>
      call<ProcessingManifest>((backend) => backend.GetProcessingManifest()),
    openStudy: (inputPath) =>
      call<OpenStudyCommandResult>((backend) =>
        backend.OpenStudy({ inputPath } satisfies OpenStudyCommand),
      ),
    startRenderStudyJob: (studyId) =>
      call<StartedJob>((backend) =>
        backend.StartRenderJob({ studyId } satisfies RenderStudyCommand),
      ),
    startAnalyzeStudyJob: (studyId) =>
      call<StartedJob>((backend) =>
        backend.StartAnalyzeJob({ studyId } satisfies AnalyzeStudyCommand),
      ),
    startProcessStudyJob: (studyId, request) =>
      call<StartedJob>((backend) =>
        backend.StartProcessJob(
          buildProcessStudyCommand(studyId, request) satisfies ProcessStudyCommand,
        ),
      ),
    getJob: (jobId) =>
      call<JobSnapshot>((backend) => backend.GetJob({ jobId } satisfies JobCommand)),
    getJobs: (jobIds) =>
      call<JobSnapshot[]>((backend) =>
        backend.GetJobs({ jobIds: [...new Set(jobIds)] } satisfies GetJobsCommand),
      ),
    cancelJob: (jobId) =>
      call<JobSnapshot>((backend) => backend.CancelJob({ jobId } satisfies JobCommand)),
    measureLineAnnotation: async (studyId, annotation): Promise<LineAnnotation> => {
      const payload = await call<MeasureLineAnnotationCommandResult>((backend) =>
        backend.MeasureLineAnnotation({
          studyId,
          annotation,
        } satisfies MeasureLineAnnotationCommand),
      );
      return payload.annotation;
    },
    setStudyCalibration: async (
      studyId,
      reference: CalibrationReference | null,
    ): Promise<MeasurementScale | null> => {
      const payload = await call<SetStudyCalibrationCommandResult>((backend) =>
        backend.SetStudyCalibration({ studyId, reference } satisfies SetStudyCalibrationCommand),
      );
      return payload.study.measurementScale ?? null;
    },
  };
}
