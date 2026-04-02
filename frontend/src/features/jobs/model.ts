import type {
  BackendError,
  JobKind,
  JobProgress,
  JobState,
} from "../../lib/generated/contracts";
import type {
  PreviewResult,
  ProcessResult,
  ToothAnalysisResult,
} from "../../lib/types";

export type JobResultPayload =
  | { kind: "renderStudy"; payload: PreviewResult }
  | { kind: "processStudy"; payload: ProcessResult }
  | { kind: "analyzeStudy"; payload: ToothAnalysisResult };

export interface JobSnapshot {
  jobId: string;
  jobKind: JobKind;
  studyId: string | null;
  state: JobState;
  progress: JobProgress;
  fromCache: boolean;
  result: JobResultPayload | null;
  error: BackendError | null;
}

export type ProcessingRunState =
  | { state: "idle" }
  | { state: "running"; jobId: string; progress: JobProgress }
  | { state: "cancelling"; jobId: string; progress: JobProgress }
  | { state: "success"; jobId: string; outputPath: string; fromCache: boolean }
  | { state: "error"; jobId: string; error: BackendError }
  | { state: "cancelled"; jobId: string };
