import type {
  AnnotationBundle,
  LineAnnotation,
  MeasurementScale,
  ToothCandidate,
} from "../../lib/generated/contracts";
import type { JobSnapshot } from "../../features/jobs/model";
import { workbenchActions } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backendErrors";
import {
  annotationSourceLabel,
  formatLineMeasurement,
  formatSecondaryMeasurement,
} from "../../features/annotations/tools";

export interface ViewSidebarProps {
  selectedLine: LineAnnotation | null;
  annotations: AnnotationBundle;
  selectedAnnotationId: string | null;
  teeth: ToothCandidate[];
  tooth: ToothCandidate | null;
  measurementScale: MeasurementScale | null;
  warnings: string[];
  analysisJob: JobSnapshot | null;
}

export function ViewSidebar({
  selectedLine,
  annotations,
  selectedAnnotationId,
  teeth,
  tooth,
  measurementScale,
  warnings,
  analysisJob,
}: ViewSidebarProps) {
  const activeTooth = tooth ?? teeth[0] ?? null;
  const annotationListClassName =
    annotations.lines.length > 4
      ? "annotation-list annotation-list--scrollable"
      : "annotation-list";

  return (
    <aside className="study-analysis__sidebar">
      <section className="measurement-card">
        <h3 className="measurement-card__eyebrow">Viewer Tools</h3>
        <p className="measurement-card__copy">
          Drag to pan, use the mouse wheel to zoom, then switch to Measure
          line to place calibrated annotations in source pixel space.
        </p>
        {selectedLine ? (
          <div className="measurement-card__hero">
            <span className="measurement-card__hero-label">
              Selected line
            </span>
            <span className="measurement-card__hero-value">
              {formatLineMeasurement(selectedLine.measurement)}
            </span>
          </div>
        ) : null}
      </section>

      <section className="measurement-card">
        <h3 className="measurement-card__eyebrow">Line Annotations</h3>
        {annotations.lines.length ? (
          <div className={annotationListClassName}>
            {annotations.lines.map((annotation) => (
              <button
                key={annotation.id}
                className={`annotation-list__item${
                  annotation.id === selectedAnnotationId
                    ? " annotation-list__item--selected"
                    : ""
                }`}
                type="button"
                aria-label={annotation.label}
                aria-pressed={annotation.id === selectedAnnotationId}
                onClick={() =>
                  workbenchActions.selectAnnotation(annotation.id)
                }
              >
                <span className="annotation-list__title">
                  {annotation.label}
                </span>
                <span className="annotation-list__meta">
                  {annotationSourceLabel(annotation.source)}
                </span>
                <span className="annotation-list__value">
                  {formatLineMeasurement(annotation.measurement)}
                </span>
                {formatSecondaryMeasurement(annotation.measurement) ? (
                  <span className="annotation-list__subvalue">
                    {formatSecondaryMeasurement(annotation.measurement)}
                  </span>
                ) : null}
              </button>
            ))}
          </div>
        ) : (
          <p className="measurement-card__copy">
            No manual line annotations yet. Draw one to store a measurement,
            or run Analyze to overlay the red trace.
          </p>
        )}
      </section>

      {activeTooth ? (
        <section className="measurement-card measurement-card--analysis">
          <h3 className="measurement-card__eyebrow">Tooth Analysis</h3>
          <div className="measurement-summary-grid">
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Confidence</span>
              <span className="measurement-summary-grid__value">
                {Math.round(activeTooth.confidence * 100)}%
              </span>
            </div>
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Mask area</span>
              <span className="measurement-summary-grid__value">
                {activeTooth.maskAreaPixels.toLocaleString()} px
              </span>
            </div>
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Trace vertices</span>
              <span className="measurement-summary-grid__value">
                {activeTooth.geometry.outline.length}
              </span>
            </div>
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Detected candidates</span>
              <span className="measurement-summary-grid__value">
                {teeth.length}
              </span>
            </div>
          </div>
          <p className="measurement-card__copy">
            The backend traced the boundary of the detected white region and
            rendered it as a red closed outline.
          </p>
          {measurementScale ? (
            <div className="measurement-panel">
              <h3 className="measurement-panel__title">Calibration</h3>
              <div className="measurement-panel__meta">
                <span className="measurement-grid__label">Source</span>
                <span className="measurement-panel__value">
                  {measurementScale.source}
                </span>
              </div>
              <p className="measurement-card__copy">
                Row {measurementScale.rowSpacingMm.toFixed(3)} mm, column{" "}
                {measurementScale.columnSpacingMm.toFixed(3)} mm.
              </p>
            </div>
          ) : null}
          {warnings.length ? (
            <div className="measurement-panel">
              <h3 className="measurement-panel__title">Backend notes</h3>
              <div className="measurement-note-list">
                {warnings.map((warning) => (
                  <p key={warning} className="measurement-card__copy">
                    {warning}
                  </p>
                ))}
              </div>
            </div>
          ) : null}
        </section>
      ) : null}

      {!activeTooth && measurementScale ? (
        <section className="measurement-card">
          <h3 className="measurement-card__eyebrow">Calibration</h3>
          <div className="measurement-card__hero">
            <span className="measurement-card__hero-label">Source</span>
            <span className="measurement-card__hero-value">
              {measurementScale.source}
            </span>
          </div>
          <p className="measurement-card__copy">
            Row {measurementScale.rowSpacingMm.toFixed(3)} mm, column{" "}
            {measurementScale.columnSpacingMm.toFixed(3)} mm.
          </p>
        </section>
      ) : null}

      {!activeTooth && warnings.length ? (
        <section className="measurement-card">
          <h3 className="measurement-card__eyebrow">Backend notes</h3>
          <div className="measurement-note-list">
            {warnings.map((warning) => (
              <p key={warning} className="measurement-card__copy">
                {warning}
              </p>
            ))}
          </div>
        </section>
      ) : null}

      {analysisJob?.state === "failed" ? (
        <section className="measurement-card">
          <h3 className="measurement-card__eyebrow">Job Error</h3>
          <p className="measurement-card__copy">
            {formatBackendError(analysisJob.error, "Tooth analysis failed.")}
          </p>
        </section>
      ) : null}
    </aside>
  );
}
