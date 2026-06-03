import type { BackendError, JobKind, JobProgress, JobState } from "../../lib/generated/contracts";
import type { AnalysisResult, PreviewResult, ProcessResult } from "../../lib/types";

export interface JobProgressSample {
  atMs: number;
  percent: number;
}

export interface JobProgressTiming {
  startedAtMs: number;
  lastUpdatedAtMs: number;
  lastProgressAtMs: number;
  lastProgressPercent: number;
  firstMeasuredSample: JobProgressSample | null;
  measuredSampleCount: number;
  smoothedRate: number | null;
}

export type JobResultPayload =
  | { kind: "renderStudy"; payload: PreviewResult }
  | { kind: "analyzeStudy"; payload: AnalysisResult }
  | { kind: "processStudy"; payload: ProcessResult };

export interface JobSnapshot {
  jobId: string;
  jobKind: JobKind;
  studyId: string | null;
  state: JobState;
  progress: JobProgress;
  fromCache: boolean;
  result: JobResultPayload | null;
  error: BackendError | null;
  timing: JobProgressTiming | null;
}

export type ProcessingRunState =
  | { state: "idle" }
  | {
      state: "running";
      jobId: string;
      progress: JobProgress;
      timing: JobProgressTiming | null;
    }
  | {
      state: "cancelling";
      jobId: string;
      progress: JobProgress;
      timing: JobProgressTiming | null;
    }
  | { state: "success"; jobId: string; fromCache: boolean }
  | { state: "error"; jobId: string; error: BackendError }
  | { state: "cancelled"; jobId: string };
