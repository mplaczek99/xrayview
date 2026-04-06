import type {
  AnnotationBundle,
  LineAnnotation,
  MeasurementScale,
  ToothCandidate,
  ToothMeasurementValues,
} from "../../lib/generated/contracts";
import type { JobSnapshot } from "../../features/jobs/model";
import { workbenchActions } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backendErrors";
import {
  annotationSourceLabel,
  formatLineMeasurement,
  formatSecondaryMeasurement,
} from "../../features/annotations/tools";

interface MeasurementSectionProps {
  title: string;
  measurements: ToothMeasurementValues;
  compact?: boolean;
}

function formatMeasurement(value: number, units: string): string {
  return units === "mm" ? `${value.toFixed(1)} ${units}` : `${Math.round(value)} ${units}`;
}

function MeasurementSection({
  title,
  measurements,
  compact = false,
}: MeasurementSectionProps) {
  const Wrapper = compact ? "div" : "section";

  return (
    <Wrapper className={compact ? "measurement-panel" : "measurement-card"}>
      <h3 className={compact ? "measurement-panel__title" : "measurement-card__eyebrow"}>
        {title}
      </h3>
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
    </Wrapper>
  );
}

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

function toothIndexFromAnnotationId(annotationId: string | null): number | null {
  if (!annotationId) {
    return null;
  }

  const match = /^auto-tooth-(\d+)-(width|height)$/.exec(annotationId);
  if (!match) {
    return null;
  }

  const toothIndex = Number.parseInt(match[1], 10) - 1;
  return Number.isNaN(toothIndex) ? null : toothIndex;
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
  const selectedToothIndex = toothIndexFromAnnotationId(selectedAnnotationId);
  const activeTooth =
    (selectedToothIndex !== null ? teeth[selectedToothIndex] ?? null : null) ??
    tooth ??
    teeth[0] ??
    null;
  const activeToothLabel =
    selectedToothIndex !== null && teeth[selectedToothIndex]
      ? `Tooth ${selectedToothIndex + 1}`
      : teeth.length > 1
        ? "Top candidate"
        : teeth.length === 1
          ? "Tooth 1"
          : null;
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
            No line annotations yet. Draw one to store a manual
            measurement, or run Measure teeth to load editable suggestions.
          </p>
        )}
      </section>

      {activeTooth && (
        <section className="measurement-card measurement-card--analysis">
          <h3 className="measurement-card__eyebrow">
            Automatic Measurements
          </h3>
          <div className="measurement-summary-grid">
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Detected teeth</span>
              <span className="measurement-summary-grid__value">
                {teeth.length}
              </span>
            </div>
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Confidence</span>
              <span className="measurement-summary-grid__value">
                {Math.round(activeTooth.confidence * 100)}%
              </span>
            </div>
            {activeToothLabel ? (
              <div className="measurement-summary-grid__item">
                <span className="measurement-grid__label">Inspecting</span>
                <span className="measurement-summary-grid__value">
                  {activeToothLabel}
                </span>
              </div>
            ) : null}
            <div className="measurement-summary-grid__item">
              <span className="measurement-grid__label">Mask area</span>
              <span className="measurement-summary-grid__value">
                {activeTooth.maskAreaPixels.toLocaleString()} px
              </span>
            </div>
          </div>
          <p className="measurement-card__copy">
            Select an auto-tooth line to inspect a specific tooth; overlay
            guides stay editable in the viewer.
          </p>
          <div className="measurement-section-list">
            <MeasurementSection
              compact
              title={teeth.length > 1 ? "Active tooth pixels" : "Pixel measurements"}
              measurements={activeTooth.measurements.pixel}
            />
            {activeTooth.measurements.calibrated ? (
              <MeasurementSection
                compact
                title={teeth.length > 1 ? "Active tooth calibrated" : "Calibrated measurements"}
                measurements={activeTooth.measurements.calibrated}
              />
            ) : null}
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
          </div>
        </section>
      )}

      {!activeTooth && measurementScale && (
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
