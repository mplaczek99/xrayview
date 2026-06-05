import type { JobSnapshot } from "../../features/jobs/model";
import type { WorkbenchState, WorkbenchStudy } from "../../features/study/model";
import { formatBackendError } from "../../lib/backendErrors";

function isPendingJob(job: JobSnapshot | null | undefined): boolean {
  return job?.state === "queued" || job?.state === "running" || job?.state === "cancelling";
}

function hasSupersedingJob(
  currentJobId: string | null | undefined,
  incomingJob: JobSnapshot,
  jobs: WorkbenchState["jobs"],
): boolean {
  if (!currentJobId || currentJobId === incomingJob.jobId) {
    return false;
  }

  const currentJob = jobs[currentJobId];
  if (!currentJob) {
    return false;
  }

  return isPendingJob(currentJob) || !isPendingJob(incomingJob);
}

function processRunJobId(study: WorkbenchStudy): string | null {
  const { runStatus } = study.processing;
  return runStatus.state === "idle" ? null : runStatus.jobId;
}

function applyRenderJob(
  study: WorkbenchStudy,
  job: JobSnapshot,
  jobs: WorkbenchState["jobs"],
): WorkbenchStudy {
  if (hasSupersedingJob(study.renderJobId, job, jobs)) {
    return study;
  }

  switch (job.state) {
    case "queued":
    case "running":
    case "cancelling":
      return {
        ...study,
        renderJobId: job.jobId,
        status: job.progress.message,
      };
    case "completed":
      if (job.result?.kind !== "renderStudy") {
        return study;
      }

      return {
        ...study,
        renderJobId: job.jobId,
        originalPreview: job.result.payload,
        measurementScale: job.result.payload.measurementScale ?? study.measurementScale,
        status: job.fromCache
          ? "Preview ready from cache."
          : "Study loaded. Drag to pan, scroll to zoom, or draw a line measurement.",
      };
    case "failed":
      return {
        ...study,
        renderJobId: job.jobId,
        status: formatBackendError(job.error, "Preview loading failed."),
      };
    case "cancelled":
      return {
        ...study,
        renderJobId: job.jobId,
        status: "Preview rendering cancelled.",
      };
  }
}

function applyAnalyzeJob(
  study: WorkbenchStudy,
  job: JobSnapshot,
  jobs: WorkbenchState["jobs"],
): WorkbenchStudy {
  if (hasSupersedingJob(study.analysisJobId, job, jobs)) {
    return study;
  }

  switch (job.state) {
    case "queued":
    case "running":
    case "cancelling":
      return {
        ...study,
        analysisJobId: job.jobId,
        status: job.progress.message,
      };
    case "completed": {
      if (job.result?.kind !== "analyzeStudy") {
        return study;
      }

      const mode = job.result.payload.mode;
      const toothUnreliable = mode.includes("no reliable tooth mask");
      const boneUnreliable = mode.includes("no reliable bone level");
      let status: string;
      if (toothUnreliable && boneUnreliable) {
        status = "Analysis completed, but no reliable tooth mask or bone level was found.";
      } else if (toothUnreliable) {
        status = "Analysis completed, but no reliable tooth mask was found.";
      } else if (boneUnreliable) {
        status = "Analysis completed, but no reliable bone level was found.";
      } else if (job.fromCache) {
        status = "Tooth and bone level overlay loaded from cache.";
      } else {
        status = "Tooth and bone level overlay generated.";
      }

      return {
        ...study,
        analysisJobId: job.jobId,
        analysisPreview: job.result.payload,
        measurementScale: job.result.payload.measurementScale ?? study.measurementScale,
        status,
      };
    }
    case "failed":
      return {
        ...study,
        analysisJobId: job.jobId,
        status: formatBackendError(job.error, "Tooth and bone level analysis failed."),
      };
    case "cancelled":
      return {
        ...study,
        analysisJobId: job.jobId,
        status: "Tooth and bone level analysis cancelled.",
      };
  }
}

function applyProcessJob(
  study: WorkbenchStudy,
  job: JobSnapshot,
  jobs: WorkbenchState["jobs"],
): WorkbenchStudy {
  if (hasSupersedingJob(processRunJobId(study), job, jobs)) {
    return study;
  }

  switch (job.state) {
    case "queued":
    case "running":
    case "cancelling":
      return {
        ...study,
        status: job.progress.message,
        processing: {
          ...study.processing,
          runStatus: {
            state: job.state === "cancelling" ? "cancelling" : "running",
            jobId: job.jobId,
            progress: job.progress,
            timing: job.timing,
          },
        },
      };
    case "completed":
      if (job.result?.kind !== "processStudy") {
        return study;
      }

      return {
        ...study,
        measurementScale: job.result.payload.measurementScale ?? study.measurementScale,
        status: job.fromCache ? "Processing loaded from cache." : "Processing complete.",
        processing: {
          ...study.processing,
          output: job.result.payload,
          runStatus: {
            state: "success",
            jobId: job.jobId,
            fromCache: job.fromCache,
          },
        },
      };
    case "failed":
      return {
        ...study,
        status: formatBackendError(job.error, "Processing failed."),
        processing: {
          ...study.processing,
          runStatus: {
            state: "error",
            jobId: job.jobId,
            error: job.error ?? {
              code: "internal",
              message: "Processing failed.",
              details: [],
              recoverable: false,
            },
          },
        },
      };
    case "cancelled":
      return {
        ...study,
        status: "Processing cancelled.",
        processing: {
          ...study.processing,
          runStatus: {
            state: "cancelled",
            jobId: job.jobId,
          },
        },
      };
  }
}

export function applyJobToStudy(
  study: WorkbenchStudy,
  job: JobSnapshot,
  jobs: WorkbenchState["jobs"],
): WorkbenchStudy {
  switch (job.jobKind) {
    case "renderStudy":
      return applyRenderJob(study, job, jobs);
    case "analyzeStudy":
      return applyAnalyzeJob(study, job, jobs);
    case "processStudy":
      return applyProcessJob(study, job, jobs);
  }
}
