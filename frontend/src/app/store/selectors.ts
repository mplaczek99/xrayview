import type { ProcessingForm, ProcessingSession, WorkbenchState } from "../../features/study/model";

type StateSelector<T> = (state: WorkbenchState) => T;
type SelectorValues<T extends readonly StateSelector<unknown>[]> = {
  [K in keyof T]: T[K] extends StateSelector<infer Value> ? Value : never;
};

// Memoize a derived value on one or more input slices using Object.is.
function createSelector<const Inputs extends readonly StateSelector<unknown>[], Result>(
  inputSelectors: Inputs,
  resultFn: (...inputs: SelectorValues<Inputs>) => Result,
): StateSelector<Result> {
  let lastInputs: SelectorValues<Inputs> | null = null;
  let lastResult: Result;

  return (state: WorkbenchState): Result => {
    const inputs = inputSelectors.map((selector) => selector(state)) as SelectorValues<Inputs>;
    if (
      lastInputs &&
      inputs.length === lastInputs.length &&
      inputs.every((input, index) => Object.is(input, lastInputs?.[index]))
    ) {
      return lastResult;
    }

    lastInputs = inputs;
    lastResult = resultFn(...inputs);
    return lastResult;
  };
}

export const selectJobs = (state: WorkbenchState) => state.jobs;
export const selectJobOrder = (state: WorkbenchState) => state.jobOrder;
export const selectStudies = (state: WorkbenchState) => state.studies;
export const selectIsOpeningStudy = (state: WorkbenchState) => state.isOpeningStudy;
export const selectWorkbenchStatus = (state: WorkbenchState) => state.workbenchStatus;
export const selectManifest = (state: WorkbenchState) => state.manifest;

export const selectPendingJobCount = (state: WorkbenchState) => state.pendingJobIds.size;

// Memoized on activeStudyId + studies: returns cached reference when neither changes.
export const selectActiveStudy = createSelector(
  [(state) => state.activeStudyId, (state) => state.studies],
  (activeStudyId, studies) => (activeStudyId ? (studies[activeStudyId] ?? null) : null),
);

interface ProcessingTabStudyState {
  studyId: string;
  form: ProcessingForm;
  runStatus: ProcessingSession["runStatus"];
  originalPreviewUrl: string | null;
  processedPreviewUrl: string | null;
}

export const selectProcessingTabStudy = (() => {
  let lastStudyId: string | null = null;
  let lastForm: ProcessingForm | null = null;
  let lastRunStatus: ProcessingSession["runStatus"] | null = null;
  let lastOriginalPreviewUrl: string | null = null;
  let lastProcessedPreviewUrl: string | null = null;
  let lastResult: ProcessingTabStudyState | null = null;
  let initialized = false;

  return (state: WorkbenchState): ProcessingTabStudyState | null => {
    const study = state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
    const studyId = study?.studyId ?? null;
    const form = study?.processing.form ?? null;
    const runStatus = study?.processing.runStatus ?? null;
    const originalPreviewUrl = study?.originalPreview?.previewUrl ?? null;
    const processedPreviewUrl = study?.processing.output?.previewUrl ?? null;

    if (
      initialized &&
      studyId === lastStudyId &&
      Object.is(form, lastForm) &&
      Object.is(runStatus, lastRunStatus) &&
      originalPreviewUrl === lastOriginalPreviewUrl &&
      processedPreviewUrl === lastProcessedPreviewUrl
    ) {
      return lastResult;
    }

    initialized = true;
    lastStudyId = studyId;
    lastForm = form;
    lastRunStatus = runStatus;
    lastOriginalPreviewUrl = originalPreviewUrl;
    lastProcessedPreviewUrl = processedPreviewUrl;
    lastResult =
      study && form && runStatus
        ? {
            studyId: study.studyId,
            form,
            runStatus,
            originalPreviewUrl,
            processedPreviewUrl,
          }
        : null;
    return lastResult;
  };
})();
