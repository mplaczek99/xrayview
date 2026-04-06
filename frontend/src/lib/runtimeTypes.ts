import type { JobSnapshot } from "../features/jobs/model";
import type {
  JobSnapshot as ContractJobSnapshot,
  LineAnnotation,
  OpenStudyCommandResult,
  ProcessingManifest,
  StartedJob,
} from "./generated/contracts";
import type { OpenedStudy, ProcessingRequest, RuntimeMode } from "./types";

export interface ShellAPI {
  mode: RuntimeMode;
  pickDicomFile(): Promise<string | null>;
  pickSaveDicomPath(defaultName: string): Promise<string | null>;
  resolvePreviewUrl(previewPath: string): string;
}

export interface BackendAPI {
  mode: RuntimeMode;
  loadProcessingManifest(): Promise<ProcessingManifest>;
  openStudy(inputPath: string): Promise<OpenStudyCommandResult>;
  startRenderStudyJob(studyId: string): Promise<StartedJob>;
  startProcessStudyJob(
    studyId: string,
    request: ProcessingRequest,
  ): Promise<StartedJob>;
  startAnalyzeStudyJob(studyId: string): Promise<StartedJob>;
  getJob(jobId: string): Promise<ContractJobSnapshot>;
  cancelJob(jobId: string): Promise<ContractJobSnapshot>;
  measureLineAnnotation(
    studyId: string,
    annotation: LineAnnotation,
  ): Promise<LineAnnotation>;
}

export interface RuntimeAdapter {
  mode: RuntimeMode;
  shell: ShellAPI;
  backend: BackendAPI;
  loadProcessingManifest(): Promise<ProcessingManifest>;
  pickDicomFile(): Promise<string | null>;
  pickSaveDicomPath(defaultName: string): Promise<string | null>;
  openStudy(inputPath: string): Promise<OpenedStudy>;
  startRenderStudyJob(studyId: string): Promise<StartedJob>;
  startProcessStudyJob(
    studyId: string,
    request: ProcessingRequest,
  ): Promise<StartedJob>;
  startAnalyzeStudyJob(studyId: string): Promise<StartedJob>;
  getJob(jobId: string): Promise<JobSnapshot>;
  cancelJob(jobId: string): Promise<JobSnapshot>;
  measureLineAnnotation(
    studyId: string,
    annotation: LineAnnotation,
  ): Promise<LineAnnotation>;
}
