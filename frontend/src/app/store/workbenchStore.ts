import { useSyncExternalStore } from "react";
import {
  FALLBACK_PROCESSING_MANIFEST,
  analyzeStudy as runBackendAnalyzeStudy,
  buildOutputName,
  ensureDicomExtension,
  loadProcessingManifest,
  openStudy as openBackendStudy,
  pickDicomFile,
  pickSaveDicomPath,
  processStudy as runBackendProcessStudy,
  renderStudy as runBackendRenderStudy,
} from "../../lib/backend";
import type {
  ProcessingControls,
  ProcessingPipelineStep,
} from "../../lib/generated/contracts";
import type { ProcessingRequest } from "../../lib/types";
import type { ProcessingRunState } from "../../features/jobs/model";
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
  busyAction: null,
  workbenchStatus: "Open a DICOM study to begin.",
};

type Listener = () => void;

function describeError(error: unknown, fallback: string): string {
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  if (typeof error === "string" && error.trim()) {
    return error;
  }
  if (error && typeof error === "object" && "message" in error) {
    const message = error.message;
    if (typeof message === "string" && message.trim()) {
      return message;
    }
  }
  return fallback;
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
    if (this.state.busyAction !== null) {
      return;
    }

    const selectedPath = await pickDicomFile();
    if (!selectedPath) {
      return;
    }

    this.setState((current) => ({
      ...current,
      busyAction: "opening",
      workbenchStatus: "Loading source preview...",
    }));

    try {
      const study = await openBackendStudy(selectedPath);
      const preview = await runBackendRenderStudy(study.studyId);
      const workbenchStudy = createWorkbenchStudy(
        study,
        preview,
        defaultControlsForManifest(this.state.manifest),
      );

      this.setState((current) => ({
        ...current,
        activeStudyId: study.studyId,
        studies: {
          ...current.studies,
          [study.studyId]: workbenchStudy,
        },
        studyOrder: [
          study.studyId,
          ...current.studyOrder.filter((entry) => entry !== study.studyId),
        ],
        busyAction: null,
        workbenchStatus: workbenchStudy.status,
      }));
    } catch (error) {
      this.setState((current) => ({
        ...current,
        busyAction: null,
        workbenchStatus: describeError(error, "Preview loading failed."),
      }));
    }
  }

  async measureActiveStudy() {
    const study = this.activeStudy();
    if (!study || this.state.busyAction !== null) {
      return;
    }

    this.setStudyState(study.studyId, (current) => ({
      ...current,
      status: "Running backend tooth measurement...",
    }));
    this.setState((current) => ({
      ...current,
      busyAction: "measuring",
    }));

    try {
      const result = await runBackendAnalyzeStudy(study.studyId);
      const toothFound = Boolean(result.analysis.tooth);

      this.setStudyState(result.studyId, (current) => ({
        ...current,
        originalPreview: {
          studyId: result.studyId,
          previewUrl: result.previewUrl,
          measurementScale:
            result.analysis.calibration.measurementScale ??
            current.originalPreview?.measurementScale ??
            current.measurementScale,
          runtime: result.runtime,
        },
        measurementScale:
          result.analysis.calibration.measurementScale ?? current.measurementScale,
        analysis: result.analysis,
        runtime: result.runtime,
        status: toothFound
          ? "Tooth measurement complete."
          : "Measurement completed, but the backend could not isolate a tooth candidate.",
      }));
    } catch (error) {
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        status: describeError(error, "Tooth measurement failed."),
      }));
    } finally {
      this.setState((current) => ({
        ...current,
        busyAction: null,
      }));
    }
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

  setProcessingPipeline(pipeline: ProcessingPipelineStep[]) {
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
          pipeline: [...pipeline],
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
    if (!study || this.state.busyAction !== null) {
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
    if (!study || this.state.busyAction !== null) {
      return;
    }

    this.setState((current) => ({
      ...current,
      busyAction: "processing",
    }));
    this.setStudyState(study.studyId, (current) => ({
      ...current,
      status: "Running backend processing...",
      processing: {
        ...current.processing,
        runStatus: { state: "running" },
      },
    }));

    try {
      const result = await runBackendProcessStudy(study.studyId, request);
      this.setStudyState(result.studyId, (current) => ({
        ...current,
        measurementScale: result.measurementScale ?? current.measurementScale,
        runtime: result.runtime,
        status: "Processing complete.",
        processing: {
          ...current.processing,
          output: result,
          runStatus: {
            state: "success",
            outputPath: result.dicomPath,
          },
        },
      }));
    } catch (error) {
      this.setStudyState(study.studyId, (current) => ({
        ...current,
        status: describeError(error, "Processing failed."),
        processing: {
          ...current.processing,
          runStatus: {
            state: "error",
            message: describeError(error, "Processing failed."),
          },
        },
      }));
    } finally {
      this.setState((current) => ({
        ...current,
        busyAction: null,
      }));
    }
  }

  private activeStudy(): WorkbenchStudy | null {
    if (!this.state.activeStudyId) {
      return null;
    }

    return this.state.studies[this.state.activeStudyId] ?? null;
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

      return {
        ...current,
        studies: {
          ...current.studies,
          [studyId]: updater(study),
        },
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

export type { ProcessingRunState };
