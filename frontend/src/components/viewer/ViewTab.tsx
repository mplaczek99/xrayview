import type {
  MeasurementScale,
  ToothMeasurementValues,
} from "../../lib/generated/contracts";
import { workbenchActions, useWorkbenchStore, selectActiveStudy, selectIsOpeningStudy, selectJobs, selectWorkbenchStatus } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backend";
import {
  annotationSourceLabel,
  formatLineMeasurement,
  formatSecondaryMeasurement,
} from "../../features/annotations/tools";
import { ViewerCanvas } from "../../features/viewer/ViewerCanvas";

interface MeasurementSectionProps {
  title: string;
  measurements: ToothMeasurementValues;
}

function formatMeasurement(value: number, units: string): string {
  return units === "mm" ? `${value.toFixed(1)} ${units}` : `${Math.round(value)} ${units}`;
}

function MeasurementSection({
  title,
  measurements,
}: MeasurementSectionProps) {
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

export function ViewTab() {
  const study = useWorkbenchStore(selectActiveStudy);
  const isOpeningStudy = useWorkbenchStore(selectIsOpeningStudy);
  const jobs = useWorkbenchStore(selectJobs);
  const workbenchStatus = useWorkbenchStore(selectWorkbenchStatus);
  const tooth = study?.analysis?.tooth ?? null;
  const warnings = study?.analysis?.warnings ?? [];
  const analysisJob = study?.analysisJobId ? jobs[study.analysisJobId] ?? null : null;
  const measurementScale: MeasurementScale | null =
    study?.analysis?.calibration.measurementScale ??
    study?.measurementScale ??
    study?.originalPreview?.measurementScale ??
    null;
  const isMeasuring =
    analysisJob?.state === "queued" ||
    analysisJob?.state === "running" ||
    analysisJob?.state === "cancelling";
  const previewUrl = study?.originalPreview?.previewUrl ?? null;
  const imageSize =
    study?.originalPreview?.imageSize ??
    (study?.analysis
      ? {
          width: study.analysis.image.width,
          height: study.analysis.image.height,
        }
      : null);
  const annotations = study?.annotations ?? { lines: [], rectangles: [] };
  const selectedAnnotationId = study?.viewer.selectedAnnotationId ?? null;
  const selectedLine =
    annotations.lines.find((annotation) => annotation.id === selectedAnnotationId) ??
    null;
  const viewerTool = study?.viewer.tool ?? "pan";
  const status = study?.status ?? workbenchStatus;
  const inputName = study?.inputName ?? "No study loaded";

  return (
    <div className="view-tab">
      <h2 className="sr-only">View</h2>
      <div className="view-panel__toolbar">
        <button
          className="button button--primary"
          type="button"
          onClick={() => void workbenchActions.openStudy()}
          disabled={isOpeningStudy}
        >
          {isOpeningStudy ? "Opening..." : "Open DICOM"}
        </button>
        <button
          className="button button--ghost"
          type="button"
          onClick={() => void workbenchActions.measureActiveStudy()}
          disabled={!study || isMeasuring}
        >
          {isMeasuring
            ? analysisJob?.state === "cancelling"
              ? "Cancelling..."
              : "Measuring..."
            : tooth
              ? "Re-run measurement"
              : "Measure tooth"}
        </button>
        <button
          className={`button button--ghost${viewerTool === "pan" ? " viewer-tool--active" : ""}`}
          type="button"
          onClick={() => workbenchActions.setViewerTool("pan")}
          disabled={!study}
        >
          Pan
        </button>
        <button
          className={`button button--ghost${viewerTool === "measureLine" ? " viewer-tool--active" : ""}`}
          type="button"
          onClick={() => workbenchActions.setViewerTool("measureLine")}
          disabled={!study}
        >
          Measure line
        </button>
        <button
          className="button button--ghost"
          type="button"
          onClick={() => workbenchActions.deleteSelectedAnnotation()}
          disabled={!selectedAnnotationId}
        >
          Remove selected
        </button>
        {previewUrl && (
          <span className="view-panel__filename u-mono">{inputName}</span>
        )}
      </div>

      <p className="view-panel__status" aria-live="polite">{status}</p>

      <div className="study-analysis">
        <div className="study-analysis__viewer">
          <ViewerCanvas
            previewUrl={previewUrl}
            imageSize={imageSize}
            annotations={annotations}
            selectedAnnotationId={selectedAnnotationId}
            tool={viewerTool}
            emptyTitle="No study loaded"
            emptyDescription="Open a DICOM study to inspect it, pan and zoom, or draw a manual line measurement."
            onSelectAnnotation={(annotationId) =>
              workbenchActions.selectAnnotation(annotationId)
            }
            onCreateLine={(annotation) =>
              workbenchActions.createLineAnnotation(annotation)
            }
            onUpdateLine={(annotation) =>
              workbenchActions.updateLineAnnotation(annotation)
            }
          />
        </div>

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

          <section className="measurement-card">
            <h3 className="measurement-card__eyebrow">
              Automatic Measurement
            </h3>
            {tooth ? (
              <>
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
              </>
            ) : (
              <p className="measurement-card__copy">
                Load a study, then click Measure tooth to run the backend
                analysis and populate the returned measurements here.
              </p>
            )}
          </section>

          {tooth && <MeasurementSection title="Pixel measurements" measurements={tooth.measurements.pixel} />}
          {tooth?.measurements.calibrated && (
            <MeasurementSection
              title="Calibrated measurements"
              measurements={tooth.measurements.calibrated}
            />
          )}

          <section className="measurement-card">
            <h3 className="measurement-card__eyebrow">Calibration</h3>
            {measurementScale ? (
              <>
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
              </>
            ) : (
              <p className="measurement-card__copy">
                No calibration metadata was available in the study, so the
                backend returned pixel units only.
              </p>
            )}
          </section>

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
      </div>
    </div>
  );
}
