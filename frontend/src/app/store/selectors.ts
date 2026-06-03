import type { WorkbenchState } from "../../features/study/model";

export const selectJobs = (state: WorkbenchState) => state.jobs;
export const selectJobOrder = (state: WorkbenchState) => state.jobOrder;
export const selectStudies = (state: WorkbenchState) => state.studies;
export const selectIsOpeningStudy = (state: WorkbenchState) => state.isOpeningStudy;
export const selectWorkbenchStatus = (state: WorkbenchState) => state.workbenchStatus;
export const selectManifest = (state: WorkbenchState) => state.manifest;

export const selectPendingJobCount = (state: WorkbenchState) => state.pendingJobIds.size;

export const selectActiveStudy = (state: WorkbenchState) =>
  state.activeStudyId ? (state.studies[state.activeStudyId] ?? null) : null;
