import type { WorkbenchState } from "../../features/study/model";

type StateSelector<T> = (state: WorkbenchState) => T;
type SelectorValues<T extends readonly StateSelector<unknown>[]> = {
  [K in keyof T]: T[K] extends StateSelector<infer Value> ? Value : never;
};

// Memoize a derived value on one or more input slices using Object.is.
export function createSelector<const Inputs extends readonly StateSelector<unknown>[], Result>(
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

// Memoized on state.jobs: skips Object.values().filter() when jobs map is unchanged.
export const selectPendingJobCount = createSelector(
  [(state) => state.jobs],
  (jobs) =>
    Object.values(jobs).filter(
      (job) =>
        job.state === "queued" ||
        job.state === "running" ||
        job.state === "cancelling",
    ).length,
);

// Memoized on activeStudyId + studies: returns cached reference when neither changes.
export const selectActiveStudy = createSelector(
  [(state) => state.activeStudyId, (state) => state.studies],
  (activeStudyId, studies) =>
    activeStudyId ? studies[activeStudyId] ?? null : null,
);
