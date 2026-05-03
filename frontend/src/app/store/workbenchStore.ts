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
import type { JobSnapshot } from "../../features/jobs/model";
import { recordJobSubmit } from "../../features/jobs/benchmarks";
import { advanceJobProgressTiming } from "../../features/jobs/progressTiming";
import {
  removeAnnotation,
  upsertLineAnnotation,
  type ViewerTool,
} from "../../features/annotations/tools";
import {
  createWorkbenchStudy,
  defaultControlsForManifest,
  type WorkbenchState,
  type WorkbenchStudy,
} from "../../features/study/model";
import { applyJobToStudy } from "./applyJob";

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
  return job !== null && (
    job.state === "queued" ||
    job.state === "running" ||
    job.state === "cancelling"
  );
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

class WorkbenchStore {
  private state = INITIAL_STATE;

  private listeners = new Set<Listener>();

  private pendingNotification = false;

  // rAF debounce for setProcessingControls: coalesces rapid slider events
  // (brightness, contrast) into one state update per animation frame.
  private _pendingControls: ProcessingControls | null = null;
  private _pendingControlsStudyId: string | null = null;
  private _controlsRaf = 0; // 0 = no pending rAF

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

  async runActiveStudyAnalysis() {
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
          "Queued tooth color analysis...",
        ),
      );
      await this.syncJob(started.jobId);
    } catch (error) {
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        status: formatBackendError(error, "Tooth analysis failed."),
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

    // Accumulate latest value. If a rAF is already scheduled, it will pick
    // up this value when it fires — coalescing rapid slider events into one
    // state update per animation frame.
    this._pendingControls = controls;
    this._pendingControlsStudyId = study.studyId;

    if (!this._controlsRaf) {
      this._controlsRaf = requestAnimationFrame(() => {
        this._controlsRaf = 0;
        this.commitPendingControls();
      });
    }
  }

  private commitPendingControls() {
    const controls = this._pendingControls;
    const studyId = this._pendingControlsStudyId;
    this._pendingControls = null;
    this._pendingControlsStudyId = null;
    if (!controls || !studyId) {
      return;
    }

    this.setStudyState(studyId, (current) => ({
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
