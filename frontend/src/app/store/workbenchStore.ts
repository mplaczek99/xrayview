import { useSyncExternalStore } from "react";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  cancelJob as cancelBackendJob,
  ensureDicomExtension,
  formatBackendError,
  getJob,
  loadProcessingManifest,
  measureLineAnnotation,
  openStudy as openBackendStudy,
  pickDicomFile,
  pickSaveDicomPath,
  startAnalyzeStudyJob,
  startProcessStudyJob,
  startRenderStudyJob,
} from "../../lib/backend";
import type {
  LineAnnotation,
  ProcessingControls,
} from "../../lib/generated/contracts";
import type { ProcessingRequest } from "../../lib/types";
import type { JobSnapshot, ProcessingRunState } from "../../features/jobs/model";
import { advanceJobProgressTiming } from "../../features/jobs/progressTiming";
import {
  removeAnnotation,
  replaceSuggestedAnnotations,
  upsertLineAnnotation,
  type ViewerTool,
} from "../../features/annotations/tools";
import {
  createWorkbenchStudy,
  defaultControlsForManifest,
  type WorkbenchState,
  type WorkbenchStudy,
} from "../../features/study/model";

const INITIAL_STATE: WorkbenchState = {
  manifest: FALLBACK_PROCESSING_MANIFEST,
  manifestStatus: "idle",
  activeStudyId: null,
  studies: {},
  studyOrder: [],
  jobs: {},
  jobOrder: [],
  isOpeningStudy: false,
  workbenchStatus: "Open a DICOM study to begin.",
};

type Listener = () => void;

function nextJobOrder(currentOrder: readonly string[], jobId: string): string[] {
  return [jobId, ...currentOrder.filter((entry) => entry !== jobId)];
}

function activeJob(jobId: string | null, jobs: WorkbenchState["jobs"]): JobSnapshot | null {
  if (!jobId) {
    return null;
  }

  return jobs[jobId] ?? null;
}

function isPendingJob(job: JobSnapshot | null): boolean {
  return job !== null && ["queued", "running", "cancelling"].includes(job.state);
}

function createPendingJobSnapshot(
  jobId: string,
  jobKind: JobSnapshot["jobKind"],
  studyId: string,
  message: string,
): JobSnapshot {
  const snapshot: JobSnapshot = {
    jobId,
    jobKind,
    studyId,
    state: "queued",
    progress: {
      percent: 0,
      stage: "queued",
      message,
    },
    fromCache: false,
    result: null,
    error: null,
    timing: null,
  };

  return {
    ...snapshot,
    timing: advanceJobProgressTiming(null, snapshot),
  };
}

function applyRenderJob(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
  switch (job.state) {
    case "queued":
    case "running":
    case "cancelling":
      return {
        ...study,
        renderJobId: job.jobId,
        status: job.progress.message,
      };
    case "completed":
      if (job.result?.kind !== "renderStudy") {
        return study;
      }

      return {
        ...study,
        renderJobId: job.jobId,
        originalPreview: job.result.payload,
        measurementScale: job.result.payload.measurementScale ?? study.measurementScale,
        status: job.fromCache
          ? "Preview ready from cache."
          : "Study loaded. Drag to pan, scroll to zoom, or draw a line measurement.",
      };
    case "failed":
      return {
        ...study,
        renderJobId: job.jobId,
        status: formatBackendError(job.error, "Preview loading failed."),
      };
    case "cancelled":
      return {
        ...study,
        renderJobId: job.jobId,
        status: "Preview rendering cancelled.",
      };
  }
}

function applyAnalyzeJob(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
  switch (job.state) {
    case "queued":
    case "running":
    case "cancelling":
      return {
        ...study,
        analysisJobId: job.jobId,
        status: job.progress.message,
      };
    case "completed": {
      if (job.result?.kind !== "analyzeStudy") {
        return study;
      }

      const toothFound = Boolean(job.result.payload.analysis.tooth);
      return {
        ...study,
        analysisJobId: job.jobId,
        originalPreview: {
          studyId: job.result.payload.studyId,
          previewUrl: job.result.payload.previewUrl,
          imageSize: {
            width: job.result.payload.analysis.image.width,
            height: job.result.payload.analysis.image.height,
          },
          measurementScale:
            job.result.payload.analysis.calibration.measurementScale ??
            study.originalPreview?.measurementScale ??
            study.measurementScale,
          runtime: job.result.payload.runtime,
        },
        measurementScale:
          job.result.payload.analysis.calibration.measurementScale ?? study.measurementScale,
        analysis: job.result.payload.analysis,
        annotations: replaceSuggestedAnnotations(
          study.annotations,
          job.result.payload.suggestedAnnotations,
        ),
        status: toothFound
          ? job.fromCache
            ? "Tooth suggestions loaded from cache."
            : "Tooth measurement complete. Suggestions are ready to edit."
          : "Measurement completed, but the backend could not isolate a tooth candidate.",
      };
    }
    case "failed":
      return {
        ...study,
        analysisJobId: job.jobId,
        status: formatBackendError(job.error, "Tooth measurement failed."),
      };
    case "cancelled":
      return {
        ...study,
        analysisJobId: job.jobId,
        status: "Tooth measurement cancelled.",
      };
  }
}

function applyProcessJob(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
  switch (job.state) {
    case "queued":
    case "running":
      return {
        ...study,
        status: job.progress.message,
        processing: {
          ...study.processing,
          runStatus: {
            state: "running",
            jobId: job.jobId,
            progress: job.progress,
            timing: job.timing,
          },
        },
      };
    case "cancelling":
      return {
        ...study,
        status: job.progress.message,
        processing: {
          ...study.processing,
          runStatus: {
            state: "cancelling",
            jobId: job.jobId,
            progress: job.progress,
            timing: job.timing,
          },
        },
      };
    case "completed":
      if (job.result?.kind !== "processStudy") {
        return study;
      }

      return {
        ...study,
        measurementScale: job.result.payload.measurementScale ?? study.measurementScale,
        status: job.fromCache ? "Processing loaded from cache." : "Processing complete.",
        processing: {
          ...study.processing,
          output: job.result.payload,
          runStatus: {
            state: "success",
            jobId: job.jobId,
            outputPath: job.result.payload.dicomPath,
            fromCache: job.fromCache,
          },
        },
      };
    case "failed":
      return {
        ...study,
        status: formatBackendError(job.error, "Processing failed."),
        processing: {
          ...study.processing,
          runStatus: {
            state: "error",
            jobId: job.jobId,
            error:
              job.error ?? {
                code: "internal",
                message: "Processing failed.",
                details: [],
                recoverable: false,
              },
          },
        },
      };
    case "cancelled":
      return {
        ...study,
        status: "Processing cancelled.",
        processing: {
          ...study.processing,
          runStatus: {
            state: "cancelled",
            jobId: job.jobId,
          },
        },
      };
  }
}

function applyJobToStudy(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
  switch (job.jobKind) {
    case "renderStudy":
      return applyRenderJob(study, job);
    case "analyzeStudy":
      return applyAnalyzeJob(study, job);
    case "processStudy":
      return applyProcessJob(study, job);
  }
}

class WorkbenchStore {
  private state = INITIAL_STATE;

  private listeners = new Set<Listener>();

  subscribe = (listener: Listener) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  getState = () => this.state;

  async ensureManifest() {
    if (
      this.state.manifestStatus === "loading" ||
      this.state.manifestStatus === "ready"
    ) {
      return;
    }

    this.setState((current) => ({
      ...current,
      manifestStatus: "loading",
    }));

    try {
      const manifest = await loadProcessingManifest();
      this.setState((current) => ({
        ...current,
        manifest,
        manifestStatus: "ready",
      }));
    } catch {
      this.setState((current) => ({
        ...current,
        manifest: FALLBACK_PROCESSING_MANIFEST,
        manifestStatus: "error",
      }));
    }
  }

  async openStudy() {
    if (this.state.isOpeningStudy) {
      return;
    }

    const selectedPath = await pickDicomFile();
    if (!selectedPath) {
      return;
    }

    this.setState((current) => ({
      ...current,
      isOpeningStudy: true,
      workbenchStatus: "Opening study...",
    }));

    try {
      const study = await openBackendStudy(selectedPath);
      const workbenchStudy = createWorkbenchStudy(
        study,
        defaultControlsForManifest(this.state.manifest),
      );

      this.setState((current) => ({
        ...current,
        activeStudyId: study.studyId,
        isOpeningStudy: false,
        studies: {
          ...current.studies,
          [study.studyId]: workbenchStudy,
        },
        studyOrder: [
          study.studyId,
          ...current.studyOrder.filter((entry) => entry !== study.studyId),
        ],
        workbenchStatus: workbenchStudy.status,
      }));

      const started = await startRenderStudyJob(study.studyId);
      this.receiveJobUpdate(
        createPendingJobSnapshot(
          started.jobId,
          "renderStudy",
          study.studyId,
          "Queued source preview render...",
        ),
      );
      await this.syncJob(started.jobId);
    } catch (error) {
      this.setState((current) => ({
        ...current,
        isOpeningStudy: false,
        workbenchStatus: formatBackendError(error, "Opening the study failed."),
      }));
    }
  }

  async measureActiveStudy() {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    if (isPendingJob(activeJob(study.analysisJobId, this.state.jobs))) {
      return;
    }

    try {
      const started = await startAnalyzeStudyJob(study.studyId);
      this.receiveJobUpdate(
        createPendingJobSnapshot(
          started.jobId,
          "analyzeStudy",
          study.studyId,
          "Queued tooth measurement...",
        ),
      );
      await this.syncJob(started.jobId);
    } catch (error) {
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        status: formatBackendError(error, "Tooth measurement failed."),
      }));
    }
  }

  setViewerTool(tool: ViewerTool) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      viewer: {
        ...current.viewer,
        tool,
      },
    }));
  }

  selectAnnotation(annotationId: string | null) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      viewer: {
        ...current.viewer,
        selectedAnnotationId: annotationId,
      },
    }));
  }

  async createLineAnnotation(annotation: LineAnnotation) {
    await this.measureAndStoreLineAnnotation(annotation, "Saved manual measurement.");
  }

  async updateLineAnnotation(annotation: LineAnnotation) {
    await this.measureAndStoreLineAnnotation(annotation, "Updated line measurement.");
  }

  deleteSelectedAnnotation() {
    const study = this.activeStudy();
    if (!study || !study.viewer.selectedAnnotationId) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      annotations: removeAnnotation(
        current.annotations,
        current.viewer.selectedAnnotationId ?? "",
      ),
      viewer: {
        ...current.viewer,
        selectedAnnotationId: null,
      },
      status: "Annotation removed.",
    }));
  }

  setProcessingControls(controls: ProcessingControls) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      processing: {
        ...current.processing,
        form: {
          ...current.processing.form,
          controls: { ...controls },
        },
      },
    }));
  }

  setProcessingCompare(compare: boolean) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      processing: {
        ...current.processing,
        form: {
          ...current.processing.form,
          compare,
        },
      },
    }));
  }

  setProcessingOutputPath(outputPath: string | null) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      processing: {
        ...current.processing,
        form: {
          ...current.processing.form,
          outputPath,
        },
      },
    }));
  }

  async pickProcessingOutputPath() {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    const selectedPath = await pickSaveDicomPath(buildOutputName(study.inputPath));
    if (!selectedPath) {
      return;
    }

    this.setProcessingOutputPath(ensureDicomExtension(selectedPath));
  }

  async runActiveStudyProcessing(request: ProcessingRequest) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    if (
      study.processing.runStatus.state === "running" ||
      study.processing.runStatus.state === "cancelling"
    ) {
      return;
    }

    try {
      const started = await startProcessStudyJob(study.studyId, request);
      this.receiveJobUpdate(
        createPendingJobSnapshot(
          started.jobId,
          "processStudy",
          study.studyId,
          "Queued processing job...",
        ),
      );
      await this.syncJob(started.jobId);
    } catch (error) {
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        status: formatBackendError(error, "Processing failed."),
        processing: {
          ...current.processing,
          runStatus: {
            state: "error",
            jobId: "local-error",
            error: {
              code: "internal",
              message: formatBackendError(error, "Processing failed."),
              details: [],
              recoverable: false,
            },
          },
        },
      }));
    }
  }

  async cancelJob(jobId: string) {
    try {
      const snapshot = await cancelBackendJob(jobId);
      this.receiveJobUpdate(snapshot);
    } catch (error) {
      this.setState((current) => ({
        ...current,
        workbenchStatus: formatBackendError(error, "Cancelling the job failed."),
      }));
    }
  }

  receiveJobUpdate(job: JobSnapshot) {
    this.setState((current) => {
      const previous = current.jobs[job.jobId];
      const nextJob: JobSnapshot = {
        ...job,
        timing: advanceJobProgressTiming(
          previous?.timing ?? job.timing,
          job,
        ),
      };
      const jobs = {
        ...current.jobs,
        [job.jobId]: nextJob,
      };
      const studies = { ...current.studies };
      if (nextJob.studyId && studies[nextJob.studyId]) {
        studies[nextJob.studyId] = applyJobToStudy(studies[nextJob.studyId], nextJob);
      }

      const activeStudy = current.activeStudyId ? studies[current.activeStudyId] : null;

      return {
        ...current,
        jobs,
        studies,
        jobOrder: nextJobOrder(current.jobOrder, nextJob.jobId),
        workbenchStatus: activeStudy?.status ?? current.workbenchStatus,
      };
    });
  }

  private activeStudy(): WorkbenchStudy | null {
    if (!this.state.activeStudyId) {
      return null;
    }

    return this.state.studies[this.state.activeStudyId] ?? null;
  }

  private async syncJob(jobId: string) {
    try {
      const snapshot = await getJob(jobId);
      this.receiveJobUpdate(snapshot);
    } catch {
      // Event listeners will still reconcile later if the job already emitted.
    }
  }

  private async measureAndStoreLineAnnotation(
    annotation: LineAnnotation,
    successStatus: string,
  ) {
    const study = this.activeStudy();
    if (!study) {
      return;
    }

    try {
      const measured = await measureLineAnnotation(study.studyId, annotation);
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        annotations: upsertLineAnnotation(current.annotations, measured),
        viewer: {
          ...current.viewer,
          selectedAnnotationId: measured.id,
        },
        status: successStatus,
      }));
    } catch (error) {
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        status: formatBackendError(error, "Line measurement failed."),
      }));
    }
  }

  private setStudyState(
    studyId: string,
    updater: (study: WorkbenchStudy) => WorkbenchStudy,
  ) {
    this.setState((current) => {
      const study = current.studies[studyId];
      if (!study) {
        return current;
      }

      const studies = {
        ...current.studies,
        [studyId]: updater(study),
      };

      return {
        ...current,
        studies,
        workbenchStatus:
          current.activeStudyId === studyId
            ? studies[studyId]?.status ?? current.workbenchStatus
            : current.workbenchStatus,
      };
    });
  }

  private setState(updater: (state: WorkbenchState) => WorkbenchState) {
    const nextState = updater(this.state);
    if (nextState === this.state) {
      return;
    }

    this.state = nextState;
    for (const listener of this.listeners) {
      listener();
    }
  }
}

export const workbenchActions = new WorkbenchStore();

export function useWorkbenchStore<T>(selector: (state: WorkbenchState) => T): T {
  return useSyncExternalStore(
    workbenchActions.subscribe,
    () => selector(workbenchActions.getState()),
    () => selector(workbenchActions.getState()),
  );
}

export const selectJobs = (s: WorkbenchState) => s.jobs;
export const selectJobOrder = (s: WorkbenchState) => s.jobOrder;
export const selectStudies = (s: WorkbenchState) => s.studies;
export const selectIsOpeningStudy = (s: WorkbenchState) => s.isOpeningStudy;
export const selectWorkbenchStatus = (s: WorkbenchState) => s.workbenchStatus;
export const selectManifest = (s: WorkbenchState) => s.manifest;
export const selectActiveStudy = (s: WorkbenchState) =>
  s.activeStudyId ? s.studies[s.activeStudyId] ?? null : null;

export type { ProcessingRunState };
