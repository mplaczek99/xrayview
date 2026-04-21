import { useMemo, useState, useCallback } from "react";
import { workbenchActions, useWorkbenchStore, selectJobOrder, selectJobs, selectStudies } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backendErrors";
import { describeProgress, useProgressClock } from "./progressTiming";

function titleForJob(kind: string): string {
  switch (kind) {
    case "renderStudy":
      return "Render Preview";
    case "processStudy":
      return "Process Study";
    case "analyzeStudy":
      return "Analyze";
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

function isTerminal(state: string): boolean {
  return state === "completed" || state === "failed" || state === "cancelled";
}

export function JobCenter() {
  const jobOrder = useWorkbenchStore(selectJobOrder);
  const jobMap = useWorkbenchStore(selectJobs);
  const studies = useWorkbenchStore(selectStudies);
  const [expanded, setExpanded] = useState(true);
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());

  const jobs = useMemo(
    () =>
      jobOrder
        .map((jobId) => jobMap[jobId])
        .filter((job): job is NonNullable<typeof jobMap[string]> => Boolean(job))
        .filter((job) => !dismissed.has(job.jobId))
        .slice(0, 6),
    [jobMap, jobOrder, dismissed],
  );

  const activeCount = useMemo(
    () => jobs.filter((job) => !isTerminal(job.state)).length,
    [jobs],
  );
  const nowMs = useProgressClock(activeCount > 0);

  const dismissJob = useCallback((jobId: string) => {
    setDismissed((prev) => new Set(prev).add(jobId));
  }, []);

  const clearTerminal = useCallback(() => {
    setDismissed((prev) => {
      const next = new Set(prev);
      for (const job of jobs) {
        if (isTerminal(job.state)) next.add(job.jobId);
      }
      return next;
    });
  }, [jobs]);

  if (!jobs.length) {
    return null;
  }

  return (
    <section className="job-center" aria-label="Background jobs">
      <div className="job-center__header">
        <button
          className="job-center__toggle"
          type="button"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
        >
          <span
            className={`form-collapse-arrow${expanded ? " form-collapse-arrow--open" : ""}`}
          >
            &#9654;
          </span>
          <span className="job-center__eyebrow">Jobs</span>
          {activeCount > 0 && (
            <span className="job-center__badge">{activeCount}</span>
          )}
        </button>
        {expanded && jobs.some((j) => isTerminal(j.state)) && (
          <button
            className="button button--ghost job-center__clear"
            type="button"
            onClick={clearTerminal}
          >
            Clear finished
          </button>
        )}
      </div>

      {expanded && (
        <div className="job-center__list">
          {jobs.map((job) => {
            const studyName = job.studyId ? studies[job.studyId]?.inputName ?? null : null;
            const canCancel = job.state === "queued" || job.state === "running";
            const terminal = isTerminal(job.state);
            const progressView = describeProgress(job, nowMs);
            const message =
              job.state === "failed"
                ? formatBackendError(job.error, "Job failed.")
                : job.progress.message;

            return (
              <article
                key={job.jobId}
                className="job-card"
                data-testid="job-row"
                data-job-kind={job.jobKind}
                data-job-state={job.state}
              >
                <div className="job-card__row">
                  <div>
                    <div className="job-card__title">{titleForJob(job.jobKind)}</div>
                    <div
                      className="job-card__meta"
                      data-testid="job-state"
                      data-job-state={job.state}
                    >
                      {stateLabel(job.state)}
                      {studyName ? ` • ${studyName}` : ""}
                      {job.fromCache ? " • cache" : ""}
                    </div>
                  </div>
                  {canCancel ? (
                    <button
                      className="button button--ghost job-card__cancel"
                      type="button"
                      data-testid="action-cancel-job"
                      onClick={() => void workbenchActions.cancelJob(job.jobId)}
                    >
                      Cancel
                    </button>
                  ) : terminal ? (
                    <button
                      className="button button--ghost job-card__cancel"
                      type="button"
                      onClick={() => dismissJob(job.jobId)}
                      aria-label="Dismiss"
                    >
                      ✕
                    </button>
                  ) : null}
                </div>

                <div className="job-card__progress">
                  <div
                    className={[
                      "job-card__progress-bar",
                      `job-card__progress-bar--${job.state}`,
                      progressView.indeterminate
                        ? "job-card__progress-bar--indeterminate"
                        : "",
                    ]
                      .filter(Boolean)
                      .join(" ")}
                    style={
                      progressView.indeterminate
                        ? undefined
                        : {
                            width: `${Math.max(
                              job.progress.percent,
                              job.state === "completed" ? 100 : 4,
                            )}%`,
                          }
                    }
                  />
                </div>
                <p className="job-card__message">{message}</p>
                {progressView.detailLabel ? (
                  <p className="job-card__detail">{progressView.detailLabel}</p>
                ) : null}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}
