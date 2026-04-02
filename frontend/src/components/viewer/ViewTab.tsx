import type {
  MeasurementScale,
  ToothMeasurementValues,
} from "../../lib/generated/contracts";
import { workbenchActions, useWorkbenchStore } from "../../app/store/workbenchStore";
import { formatBackendError } from "../../lib/backend";
import { DicomViewer } from "./DicomViewer";

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
      <div className="measurement-card__eyebrow">{title}</div>
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

function useActiveStudy() {
  return useWorkbenchStore((state) =>
    state.activeStudyId ? state.studies[state.activeStudyId] ?? null : null,
  );
}

export function ViewTab() {
  const study = useActiveStudy();
  const isOpeningStudy = useWorkbenchStore((state) => state.isOpeningStudy);
  const jobs = useWorkbenchStore((state) => state.jobs);
  const workbenchStatus = useWorkbenchStore((state) => state.workbenchStatus);
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
  const status = study?.status ?? workbenchStatus;
  const inputName = study?.inputName ?? "No study loaded";

  return (
    <div className="view-tab">
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
        {previewUrl && (
          <span className="view-panel__filename u-mono">{inputName}</span>
        )}
      </div>

      <p className="view-panel__status">{status}</p>

      <div className="study-analysis">
        <div className="study-analysis__viewer">
          <DicomViewer
            previewUrl={previewUrl}
            imageSize={study?.analysis?.image ?? null}
            overlay={tooth?.geometry ?? null}
            emptyTitle="No study loaded"
            emptyDescription="Open a DICOM study to run backend tooth detection and measurement."
          />
        </div>

        <aside className="study-analysis__sidebar">
          <section className="measurement-card">
            <div className="measurement-card__eyebrow">
              Automatic Measurement
            </div>
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
                  lines are returned by the backend from the selected candidate
                  geometry.
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
            <div className="measurement-card__eyebrow">Calibration</div>
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
              <div className="measurement-card__eyebrow">Backend notes</div>
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
              <div className="measurement-card__eyebrow">Job Error</div>
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
