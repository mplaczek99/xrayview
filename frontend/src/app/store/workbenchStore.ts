import { useSyncExternalStore } from "react";
import {
  FALLBACK_PROCESSING_MANIFEST,
  buildOutputName,
  ensureDicomExtension,
  getRuntimeAdapter,
} from "../../lib/runtime";
import { formatBackendError } from "../../lib/backendErrors";
import type {
  LineAnnotation,
  ProcessingControls,
} from "../../lib/generated/contracts";
import type { ProcessingRequest } from "../../lib/types";
import type { JobSnapshot, ProcessingRunState } from "../../features/jobs/model";
import { recordJobSubmit } from "../../features/jobs/benchmarks";
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

const runtime = getRuntimeAdapter();

const INITIAL_STATE: WorkbenchState = {
  manifest: FALLBACK_PROCESSING_MANIFEST,
  manifestStatus: "idle",
  activeStudyId: null,
  studies: {},
  studyOrder: [],
  jobs: {},
  jobOrder: [],
  isOpeningStudy: false,
  workbenchStatus: "Open a DICOM study or BMP/TIFF image to begin.",
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

function detectedToothCount(analysis: WorkbenchStudy["analysis"]): number {
  if (!analysis) {
    return 0;
  }

  if (analysis.teeth.length > 0) {
    return analysis.teeth.length;
  }

  return analysis.tooth ? 1 : 0;
}

function formatAnalyzeStatus(toothCount: number, fromCache: boolean): string {
  if (toothCount === 0) {
    return "Measurement completed, but the backend could not isolate a tooth candidate.";
  }

  if (fromCache) {
    return toothCount === 1
      ? "Tooth suggestions loaded from cache."
      : `${toothCount} tooth suggestions loaded from cache.`;
  }

  return toothCount === 1
    ? "Tooth measurement complete. Suggestions are ready to edit."
    : `${toothCount} teeth measured. Suggestions are ready to edit.`;
}

// Returns true if the incoming backend snapshot has no meaningful change vs what
// is already stored. Skips state spreads and listener notifications for the
// common case where the poller receives the same queued/running snapshot twice.
//
// Note: `timing` is intentionally excluded — it is computed locally, not from
// the backend. Stall detection uses `lastProgressAtMs` (advanced only when
// percent changes) and `useProgressClock` (setInterval) for re-rendering, so
// skipping timing-only writes has no visible effect on the ETA display.
function jobSnapshotEqual(prev: JobSnapshot, next: JobSnapshot): boolean {
  return (
    prev.state === next.state &&
    prev.progress.percent === next.progress.percent &&
    prev.progress.stage === next.progress.stage &&
    prev.progress.message === next.progress.message &&
    prev.fromCache === next.fromCache &&
    // Null-transitions (null→value or value→null) must not be skipped.
    // Once both sides are non-null the job is terminal and immutable.
    (prev.result === null) === (next.result === null) &&
    (prev.error === null) === (next.error === null)
  );
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

      const toothCount = detectedToothCount(job.result.payload.analysis);
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
        status: formatAnalyzeStatus(toothCount, job.fromCache),
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

  private pendingNotification = false;

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
      const manifest = await runtime.loadProcessingManifest();
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

    const selectedPath = await runtime.pickDicomFile();
    if (!selectedPath) {
      return;
    }

    this.setState((current) => ({
      ...current,
      isOpeningStudy: true,
      workbenchStatus: "Opening study...",
    }));

    try {
      const study = await runtime.openStudy(selectedPath);
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

      const started = await runtime.startRenderStudyJob(study.studyId);
      recordJobSubmit(started.jobId);
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
      const started = await runtime.startAnalyzeStudyJob(study.studyId);
      recordJobSubmit(started.jobId);
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

    const selectedPath = await runtime.pickSaveDicomPath(buildOutputName(study.inputPath));
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
      const started = await runtime.startProcessStudyJob(study.studyId, request);
      recordJobSubmit(started.jobId);
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
      const snapshot = await runtime.cancelJob(jobId);
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
      // Skip when the polled snapshot carries no new information — same state,
      // progress, and terminal flags. Returning `current` triggers the
      // `nextState === this.state` guard in setState, preventing listener
      // notifications and React reconciliation for no-op polls.
      if (previous && jobSnapshotEqual(previous, job)) {
        return current;
      }
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
      const snapshot = await runtime.getJob(jobId);
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
      const measured = await runtime.measureLineAnnotation(study.studyId, annotation);
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

    if (!this.pendingNotification) {
      this.pendingNotification = true;
      queueMicrotask(() => {
        this.pendingNotification = false;
        for (const listener of this.listeners) {
          listener();
        }
      });
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

// createSelector: memoize a derived value on a single input slice.
// Re-runs resultFn only when inputSelector returns a new reference (Object.is).
function createSelector<T, R>(
  inputSelector: (s: WorkbenchState) => T,
  resultFn: (input: T) => R,
): (s: WorkbenchState) => R {
  let lastInput: T;
  let lastResult: R;
  let initialized = false;
  return (s: WorkbenchState): R => {
    const input = inputSelector(s);
    if (initialized && Object.is(lastInput, input)) {
      return lastResult;
    }
    lastInput = input;
    lastResult = resultFn(input);
    initialized = true;
    return lastResult;
  };
}

// createSelector2: memoize a derived value on two independent input slices.
function createSelector2<A, B, R>(
  selA: (s: WorkbenchState) => A,
  selB: (s: WorkbenchState) => B,
  resultFn: (a: A, b: B) => R,
): (s: WorkbenchState) => R {
  let lastA: A;
  let lastB: B;
  let lastResult: R;
  let initialized = false;
  return (s: WorkbenchState): R => {
    const a = selA(s);
    const b = selB(s);
    if (initialized && Object.is(lastA, a) && Object.is(lastB, b)) {
      return lastResult;
    }
    lastA = a;
    lastB = b;
    lastResult = resultFn(a, b);
    initialized = true;
    return lastResult;
  };
}

export const selectJobs = (s: WorkbenchState) => s.jobs;
export const selectJobOrder = (s: WorkbenchState) => s.jobOrder;
export const selectStudies = (s: WorkbenchState) => s.studies;
export const selectIsOpeningStudy = (s: WorkbenchState) => s.isOpeningStudy;
export const selectWorkbenchStatus = (s: WorkbenchState) => s.workbenchStatus;
export const selectManifest = (s: WorkbenchState) => s.manifest;

// Memoized on s.jobs: skips Object.values().filter() when jobs map is unchanged.
export const selectPendingJobCount = createSelector(
  (s) => s.jobs,
  (jobs) =>
    Object.values(jobs).filter(
      (job) =>
        job.state === "queued" ||
        job.state === "running" ||
        job.state === "cancelling",
    ).length,
);

// Memoized on activeStudyId + studies: returns cached reference when neither changes.
export const selectActiveStudy = createSelector2(
  (s) => s.activeStudyId,
  (s) => s.studies,
  (activeStudyId, studies) =>
    activeStudyId ? studies[activeStudyId] ?? null : null,
);

export type { ProcessingRunState };
