import { useMemo } from "react";
import type { MeasurementScale } from "../../lib/generated/contracts";
import { workbenchActions, useWorkbenchStore } from "../../app/store/workbenchStore";
import { selectActiveStudy, selectIsOpeningStudy, selectJobs } from "../../app/store/selectors";
import { ViewerCanvas } from "../../features/viewer/ViewerCanvas";
import { ViewSidebar } from "./ViewSidebar";

export function ViewTab() {
  const study = useWorkbenchStore(selectActiveStudy);
  const isOpeningStudy = useWorkbenchStore(selectIsOpeningStudy);
  const jobs = useWorkbenchStore(selectJobs);
  const measurementScale: MeasurementScale | null = useMemo(
    () =>
      study?.measurementScale ??
      study?.originalPreview?.measurementScale ??
      null,
    [study?.measurementScale, study?.originalPreview?.measurementScale],
  );
  const analysisPreview = study?.analysisPreview ?? null;
  const originalPreview = study?.originalPreview ?? null;
  const analysisOverlayMode = study?.viewer.analysisOverlayMode ?? "outline";
  const previewUrl = analysisPreview
    ? analysisOverlayMode === "sections"
      ? analysisPreview.filledPreviewUrl
      : analysisPreview.previewUrl
    : originalPreview?.previewUrl ?? null;
  const imageSize = (analysisPreview ?? originalPreview)?.imageSize ?? null;
  const analysisJob = study?.analysisJobId ? jobs[study.analysisJobId] ?? null : null;
  const isAnalyzing =
    analysisJob?.state === "queued" ||
    analysisJob?.state === "running" ||
    analysisJob?.state === "cancelling";
  const annotations = useMemo(
    () => study?.annotations ?? { lines: [], rectangles: [], polylines: [] },
    [study?.annotations],
  );
  const selectedAnnotationId = study?.viewer.selectedAnnotationId ?? null;
  const selectedLine = useMemo(
    () =>
      annotations.lines.find((a) => a.id === selectedAnnotationId) ?? null,
    [annotations.lines, selectedAnnotationId],
  );
  const viewerTool = study?.viewer.tool ?? "pan";
  const inputName = study?.inputName ?? "No study loaded";

  if (!study) {
    return (
      <div className="view-tab">
        <h2 className="sr-only">View</h2>
        <div className="empty-state">
          {isOpeningStudy ? (
            <>
              <span className="empty-state__loader" aria-hidden="true" />
              <h3 className="empty-state__title">Opening study...</h3>
              <p className="empty-state__copy">
                Loading and rendering the study preview.
              </p>
            </>
          ) : (
            <>
              <svg className="empty-state__icon" viewBox="0 0 48 48" fill="none" aria-hidden="true">
                <rect x="6" y="8" width="36" height="32" rx="4" stroke="currentColor" strokeWidth="2" />
                <path d="M6 16h36" stroke="currentColor" strokeWidth="2" />
                <circle cx="18" cy="30" r="5" stroke="currentColor" strokeWidth="2" />
                <path d="M30 25l5 5-5 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
              </svg>
              <h3 className="empty-state__title">No study loaded</h3>
              <p className="empty-state__copy">
                Open a DICOM study or BMP/TIFF image to inspect it, pan and
                zoom, and draw manual line measurements.
              </p>
              <button
                className="button button--primary empty-state__cta"
                type="button"
                data-testid="action-open-study"
                onClick={() => void workbenchActions.openStudy()}
              >
                Open Study
              </button>
            </>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="view-tab">
      <h2 className="sr-only">View</h2>
      <div className="view-panel__toolbar">
        <div className="toolbar-group">
          <button
            className="button button--primary"
            type="button"
            data-testid="action-open-study"
            onClick={() => void workbenchActions.openStudy()}
            disabled={isOpeningStudy}
          >
            <svg className="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M2 3a1 1 0 011-1h4l2 2h4a1 1 0 011 1v7a1 1 0 01-1 1H3a1 1 0 01-1-1V3z" stroke="currentColor" strokeWidth="1.5" />
            </svg>
            {isOpeningStudy ? "Opening..." : "Open Study"}
          </button>
        </div>

        <span className="toolbar-divider" aria-hidden="true" />

        <div className="toolbar-group">
          <button
            className={`button button--ghost${viewerTool === "pan" ? " viewer-tool--active" : ""}`}
            type="button"
            data-testid="action-tool-pan"
            onClick={() => workbenchActions.setViewerTool("pan")}
          >
            <svg className="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M8 1v3M8 12v3M1 8h3M12 8h3M3.5 3.5l2 2M10.5 10.5l2 2M3.5 12.5l2-2M10.5 5.5l2-2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
            Pan
          </button>
          <button
            className={`button button--ghost${viewerTool === "measureLine" ? " viewer-tool--active" : ""}`}
            type="button"
            data-testid="action-tool-measure-line"
            onClick={() => workbenchActions.setViewerTool("measureLine")}
          >
            <svg className="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M2 14L14 2" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              <path d="M2 14v-4M2 14h4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
              <path d="M14 2v4M14 2h-4" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
            Measure line
          </button>
        </div>

        <span className="toolbar-divider" aria-hidden="true" />

        <div className="toolbar-group">
          <button
            className="button button--ghost"
            type="button"
            data-testid="action-measure-teeth"
            onClick={() => void workbenchActions.runActiveStudyAnalysis()}
            disabled={!study.originalPreview || isAnalyzing}
          >
            <svg className="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.5" />
              <path d="M8 5v6M5 8h6" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
            </svg>
            {isAnalyzing ? "Analyzing..." : "Analyze"}
          </button>
          {analysisPreview && (
            <div className="analysis-toggle" role="group" aria-label="Analysis overlay">
              <button
                className={`analysis-toggle__btn${
                  analysisOverlayMode === "outline" ? " analysis-toggle__btn--active" : ""
                }`}
                type="button"
                data-testid="action-analysis-outline"
                aria-pressed={analysisOverlayMode === "outline"}
                onClick={() => workbenchActions.setAnalysisOverlayMode("outline")}
              >
                Outline
              </button>
              <button
                className={`analysis-toggle__btn${
                  analysisOverlayMode === "sections" ? " analysis-toggle__btn--active" : ""
                }`}
                type="button"
                data-testid="action-analysis-sections"
                aria-pressed={analysisOverlayMode === "sections"}
                onClick={() => workbenchActions.setAnalysisOverlayMode("sections")}
              >
                Sections
              </button>
            </div>
          )}
          <button
            className="button button--ghost"
            type="button"
            data-testid="action-remove-annotation"
            onClick={() => workbenchActions.deleteSelectedAnnotation()}
            disabled={!selectedAnnotationId}
          >
            <svg className="button__icon" viewBox="0 0 16 16" fill="none" aria-hidden="true">
              <path d="M5 2h6M3 4h10M4 4l1 9a1 1 0 001 1h4a1 1 0 001-1l1-9" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
            </svg>
            Remove selected
          </button>
        </div>

        {previewUrl && (
          <span className="view-panel__filename u-mono">{inputName}</span>
        )}
      </div>

      <div className="study-layout">
        <div className="study-layout__viewer">
          <ViewerCanvas
            previewUrl={previewUrl}
            imageSize={imageSize}
            annotations={annotations}
            selectedAnnotationId={selectedAnnotationId}
            tool={viewerTool}
            emptyTitle="No study loaded"
            emptyDescription="Open a DICOM study or BMP/TIFF image to inspect it, pan and zoom, or draw a manual line measurement."
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
          measurementScale={measurementScale}
        />
      </div>
    </div>
  );
}
