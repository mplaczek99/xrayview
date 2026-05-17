import { formatBackendError } from "../../lib/backendErrors";
import type { JobSnapshot } from "../../features/jobs/model";
import type { WorkbenchStudy } from "../../features/study/model";

export function applyRenderJob(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
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

export function applyAnalyzeJob(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
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

      const status = job.result.payload.mode.includes("no reliable tooth mask")
        ? "Analysis completed, but no reliable tooth mask was found."
        : job.fromCache
          ? "Tooth and bone level overlay loaded from cache."
          : "Tooth and bone level overlay generated.";

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

export function applyProcessJob(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
  switch (job.state) {
    case "queued":
    case "running":
      return {
        ...study,
        status: job.progress.message,
        processing: {
          ...study.processing,
          runStatus: {
            state: "running",
            jobId: job.jobId,
            progress: job.progress,
            timing: job.timing,
          },
        },
      };
    case "cancelling":
      return {
        ...study,
        status: job.progress.message,
        processing: {
          ...study.processing,
          runStatus: {
            state: "cancelling",
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
            outputPath: job.result.payload.dicomPath,
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
            error:
              job.error ?? {
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

export function applyJobToStudy(study: WorkbenchStudy, job: JobSnapshot): WorkbenchStudy {
  switch (job.jobKind) {
    case "renderStudy":
      return applyRenderJob(study, job);
    case "analyzeStudy":
      return applyAnalyzeJob(study, job);
    case "processStudy":
      return applyProcessJob(study, job);
  }
}
