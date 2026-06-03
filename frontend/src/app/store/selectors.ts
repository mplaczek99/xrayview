import type { WorkbenchState, WorkbenchStudy } from "../../features/study/model";

let lastActiveStudyId: string | null = null;
let lastStudies: WorkbenchState["studies"] | null = null;
let lastActiveStudy: WorkbenchStudy | null = null;

export function selectActiveStudy(state: WorkbenchState): WorkbenchStudy | null {
  if (state.activeStudyId === lastActiveStudyId && state.studies === lastStudies) {
    return lastActiveStudy;
  }

  lastActiveStudyId = state.activeStudyId;
  lastStudies = state.studies;
  lastActiveStudy = state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
  return lastActiveStudy;
}
