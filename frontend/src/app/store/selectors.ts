import type { WorkbenchState } from "../../features/study/model";

export const selectActiveStudy = (state: WorkbenchState) =>
  state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
