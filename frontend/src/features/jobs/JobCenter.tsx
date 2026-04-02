import { useMemo } from "react";
import { workbenchActions, useWorkbenchStore } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backend";

function titleForJob(kind: string): string {
  switch (kind) {
    case "renderStudy":
      return "Render Preview";
    case "processStudy":
      return "Process Study";
    case "analyzeStudy":
      return "Measure Tooth";
    default:
      return kind;
  }
}

function stateLabel(state: string): string {
  switch (state) {
    case "queued":
      return "Queued";
    case "running":
      return "Running";
    case "cancelling":
      return "Cancelling";
    case "completed":
      return "Completed";
    case "failed":
      return "Failed";
    case "cancelled":
      return "Cancelled";
    default:
      return state;
  }
}

export function JobCenter() {
  const jobOrder = useWorkbenchStore((state) => state.jobOrder);
  const jobMap = useWorkbenchStore((state) => state.jobs);
  const studies = useWorkbenchStore((state) => state.studies);
  const jobs = useMemo(
    () =>
      jobOrder
        .map((jobId) => jobMap[jobId])
        .filter((job): job is NonNullable<typeof jobMap[string]> => Boolean(job))
        .slice(0, 6),
    [jobMap, jobOrder],
  );

  if (!jobs.length) {
    return null;
  }

  return (
    <section className="job-center" aria-label="Background jobs">
      <div className="job-center__header">
        <div>
          <div className="job-center__eyebrow">Jobs</div>
          <div className="job-center__title">Background work</div>
        </div>
      </div>

      <div className="job-center__list">
        {jobs.map((job) => {
          const studyName = job.studyId ? studies[job.studyId]?.inputName ?? null : null;
          const canCancel = job.state === "queued" || job.state === "running";
          const message =
            job.state === "failed"
              ? formatBackendError(job.error, "Job failed.")
              : job.progress.message;

          return (
            <article key={job.jobId} className="job-card">
              <div className="job-card__row">
                <div>
                  <div className="job-card__title">{titleForJob(job.jobKind)}</div>
                  <div className="job-card__meta">
                    {stateLabel(job.state)}
                    {studyName ? ` • ${studyName}` : ""}
                    {job.fromCache ? " • cache" : ""}
                  </div>
                </div>
                {canCancel ? (
                  <button
                    className="button button--ghost job-card__cancel"
                    type="button"
                    onClick={() => void workbenchActions.cancelJob(job.jobId)}
                  >
                    Cancel
                  </button>
                ) : null}
              </div>

              <div className="job-card__progress">
                <div
                  className={`job-card__progress-bar job-card__progress-bar--${job.state}`}
                  style={{ width: `${Math.max(job.progress.percent, job.state === "completed" ? 100 : 4)}%` }}
                />
              </div>
              <p className="job-card__message">{message}</p>
            </article>
          );
        })}
      </div>
    </section>
  );
}
