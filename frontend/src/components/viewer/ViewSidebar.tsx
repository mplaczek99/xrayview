import type {
  AnnotationBundle,
  LineAnnotation,
  MeasurementScale,
  ToothCandidate,
  ToothMeasurementValues,
} from "../../lib/generated/contracts";
import type { JobSnapshot } from "../../features/jobs/model";
import { workbenchActions } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backend";
import {
  annotationSourceLabel,
  formatLineMeasurement,
  formatSecondaryMeasurement,
} from "../../features/annotations/tools";

interface MeasurementSectionProps {
  title: string;
  measurements: ToothMeasurementValues;
}

function formatMeasurement(value: number, units: string): string {
  return units === "mm" ? `${value.toFixed(1)} ${units}` : `${Math.round(value)} ${units}`;
}

function MeasurementSection({ title, measurements }: MeasurementSectionProps) {
  return (
    <section className="measurement-card">
      <h3 className="measurement-card__eyebrow">{title}</h3>
      <div className="measurement-grid">
        <div className="measurement-grid__item">
          <span className="measurement-grid__label">Tooth width</span>
          <span className="measurement-grid__value">
            {formatMeasurement(measurements.toothWidth, measurements.units)}
          </span>
        </div>
        <div className="measurement-grid__item">
          <span className="measurement-grid__label">Tooth height</span>
          <span className="measurement-grid__value">
            {formatMeasurement(measurements.toothHeight, measurements.units)}
          </span>
        </div>
        <div className="measurement-grid__item">
          <span className="measurement-grid__label">BBox width</span>
          <span className="measurement-grid__value">
            {formatMeasurement(measurements.boundingBoxWidth, measurements.units)}
          </span>
        </div>
        <div className="measurement-grid__item">
          <span className="measurement-grid__label">BBox height</span>
          <span className="measurement-grid__value">
            {formatMeasurement(measurements.boundingBoxHeight, measurements.units)}
          </span>
        </div>
      </div>
    </section>
  );
}

export interface ViewSidebarProps {
  selectedLine: LineAnnotation | null;
  annotations: AnnotationBundle;
  selectedAnnotationId: string | null;
  tooth: ToothCandidate | null;
  measurementScale: MeasurementScale | null;
  warnings: string[];
  analysisJob: JobSnapshot | null;
}

export function ViewSidebar({
  selectedLine,
  annotations,
  selectedAnnotationId,
  tooth,
  measurementScale,
  warnings,
  analysisJob,
}: ViewSidebarProps) {
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
          <div className="annotation-list">
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
            No line annotations yet. Draw one to store a manual
            measurement, or run Measure tooth to load editable suggestions.
          </p>
        )}
      </section>

      {tooth && (
        <section className="measurement-card">
          <h3 className="measurement-card__eyebrow">
            Automatic Measurement
          </h3>
          <div className="measurement-card__hero">
            <span className="measurement-card__hero-label">Confidence</span>
            <span className="measurement-card__hero-value">
              {Math.round(tooth.confidence * 100)}%
            </span>
          </div>
          <p className="measurement-card__copy">
            Mask area {tooth.maskAreaPixels.toLocaleString()} px. Overlay
            guides are returned as editable annotation suggestions from
            the backend candidate geometry.
          </p>
        </section>
      )}

      {tooth && <MeasurementSection title="Pixel measurements" measurements={tooth.measurements.pixel} />}
      {tooth?.measurements.calibrated && (
        <MeasurementSection
          title="Calibrated measurements"
          measurements={tooth.measurements.calibrated}
        />
      )}

      {measurementScale && (
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
      )}

      {warnings.length ? (
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

      {analysisJob?.state === "failed" && (
        <section className="measurement-card">
          <h3 className="measurement-card__eyebrow">Job Error</h3>
          <p className="measurement-card__copy">
            {formatBackendError(analysisJob.error, "Measurement failed.")}
          </p>
        </section>
      )}
    </aside>
  );
}
