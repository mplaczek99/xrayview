import { useMemo } from "react";
import type { MeasurementScale } from "../../lib/generated/contracts";
import { workbenchActions, useWorkbenchStore, selectActiveStudy, selectIsOpeningStudy, selectJobs, selectWorkbenchStatus } from "../../app/store/workbenchStore";
import { ViewerCanvas } from "../../features/viewer/ViewerCanvas";
import { ViewSidebar } from "./ViewSidebar";

export function ViewTab() {
  const study = useWorkbenchStore(selectActiveStudy);
  const isOpeningStudy = useWorkbenchStore(selectIsOpeningStudy);
  const jobs = useWorkbenchStore(selectJobs);
  const workbenchStatus = useWorkbenchStore(selectWorkbenchStatus);

  const tooth = useMemo(() => study?.analysis?.tooth ?? null, [study?.analysis?.tooth]);
  const warnings = useMemo(() => study?.analysis?.warnings ?? [], [study?.analysis?.warnings]);
  const analysisJob = useMemo(
    () => (study?.analysisJobId ? jobs[study.analysisJobId] ?? null : null),
    [study?.analysisJobId, jobs],
  );
  const measurementScale: MeasurementScale | null = useMemo(
    () =>
      study?.analysis?.calibration.measurementScale ??
      study?.measurementScale ??
      study?.originalPreview?.measurementScale ??
      null,
    [study?.analysis?.calibration.measurementScale, study?.measurementScale, study?.originalPreview?.measurementScale],
  );
  const isMeasuring =
    analysisJob?.state === "queued" ||
    analysisJob?.state === "running" ||
    analysisJob?.state === "cancelling";
  const previewUrl = study?.originalPreview?.previewUrl ?? null;
  const imageSize = useMemo(
    () =>
      study?.originalPreview?.imageSize ??
      (study?.analysis
        ? {
            width: study.analysis.image.width,
            height: study.analysis.image.height,
          }
        : null),
    [study?.originalPreview?.imageSize, study?.analysis],
  );
  const annotations = useMemo(
    () => study?.annotations ?? { lines: [], rectangles: [] },
    [study?.annotations],
  );
  const selectedAnnotationId = study?.viewer.selectedAnnotationId ?? null;
  const selectedLine = useMemo(
    () =>
      annotations.lines.find((a) => a.id === selectedAnnotationId) ?? null,
    [annotations.lines, selectedAnnotationId],
  );
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

        <ViewSidebar
          selectedLine={selectedLine}
          annotations={annotations}
          selectedAnnotationId={selectedAnnotationId}
          tooth={tooth}
          measurementScale={measurementScale}
          warnings={warnings}
          analysisJob={analysisJob}
        />
      </div>
    </div>
  );
}
