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
    const previousInputs = lastInputs;
    if (
      previousInputs &&
      inputs.length === previousInputs.length &&
      inputs.every((input, index) => Object.is(input, previousInputs[index]))
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

function activeStudy(state: WorkbenchState) {
  return state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
}

interface ProcessingTabStudyState {
  studyId: string;
  form: ProcessingForm;
  runStatus: ProcessingSession["runStatus"];
  originalPreviewUrl: string | null;
  processedPreviewUrl: string | null;
}

export const selectProcessingTabStudy = createSelector(
  [
    (state) => activeStudy(state)?.studyId ?? null,
    (state) => activeStudy(state)?.processing.form ?? null,
    (state) => activeStudy(state)?.processing.runStatus ?? null,
    (state) => activeStudy(state)?.originalPreview?.previewUrl ?? null,
    (state) => activeStudy(state)?.processing.output?.previewUrl ?? null,
  ],
  (
    studyId,
    form,
    runStatus,
    originalPreviewUrl,
    processedPreviewUrl,
  ): ProcessingTabStudyState | null =>
    studyId && form && runStatus
      ? {
          studyId,
          form,
          runStatus,
          originalPreviewUrl,
          processedPreviewUrl,
        }
      : null,
);
