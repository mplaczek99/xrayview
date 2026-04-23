import type {
  AnnotationBundle,
  LineAnnotation,
  MeasurementScale,
} from "../../lib/generated/contracts";
import { workbenchActions } from "../../app/store/workbenchStore";
import {
  annotationSourceLabel,
  formatLineMeasurement,
  formatSecondaryMeasurement,
} from "../../features/annotations/tools";

export interface ViewSidebarProps {
  selectedLine: LineAnnotation | null;
  annotations: AnnotationBundle;
  selectedAnnotationId: string | null;
  measurementScale: MeasurementScale | null;
}

export function ViewSidebar({
  selectedLine,
  annotations,
  selectedAnnotationId,
  measurementScale,
}: ViewSidebarProps) {
  const annotationListClassName =
    annotations.lines.length > 4
      ? "annotation-list annotation-list--scrollable"
      : "annotation-list";

  return (
    <aside className="study-layout__sidebar">
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
            No manual line annotations yet. Draw one to store a measurement.
          </p>
        )}
      </section>

      {measurementScale ? (
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
    </aside>
  );
}
