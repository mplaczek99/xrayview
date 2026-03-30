import type { ViewerMode } from "../../lib/types";

interface ViewerStageProps {
  activeMode: ViewerMode;
  canCompare: boolean;
  originalPreviewUrl: string | null;
  processedPreviewUrl: string | null;
  recipeName: string;
  toneLabel: string;
  paletteLabel: string;
  dirty: boolean;
}

function StageImage({ src, alt }: { src: string | null; alt: string }) {
  if (!src) {
    return (
      <div className="viewer-placeholder">
        <div className="viewer-placeholder__title">Preview waiting</div>
        <p className="viewer-placeholder__copy">
          Load a DICOM study and render a processed output to unlock compare mode and export.
        </p>
      </div>
    );
  }

  return <img className="viewer-stage__image" src={src} alt={alt} />;
}

export function ViewerStage({
  activeMode,
  canCompare,
  originalPreviewUrl,
  processedPreviewUrl,
  recipeName,
  toneLabel,
  paletteLabel,
  dirty,
}: ViewerStageProps) {
  const isCompare = activeMode === "compare" && canCompare;
  const primaryImage = activeMode === "processed" ? processedPreviewUrl : originalPreviewUrl;
  const editorName = activeMode === "compare" ? "compare.diff" : activeMode === "processed" ? "processed.dcm" : "source.dcm";
  const editorTitle = activeMode === "compare" ? "Compare Editor" : activeMode === "processed" ? "Processed Preview" : "Source Preview";
  const editorDescription =
    activeMode === "compare"
      ? "Review baseline and rendered output side-by-side before exporting the derived study."
      : activeMode === "processed"
        ? "Inspect the rendered derivative in the active editor surface."
        : "Keep the original DICOM pinned while you tune the processing pipeline from the inspector.";

  return (
    <section className="viewer-shell">
      <div className="viewer-shell__header">
        <div>
          <div className="viewer-shell__eyebrow">EDITOR / {editorName}</div>
          <h2 className="viewer-shell__title">{editorTitle}</h2>
          <p className="viewer-shell__subtitle">{editorDescription}</p>
        </div>

        <div className="viewer-shell__meta">
          <span className="pill">recipe:{recipeName}</span>
          <span className="pill">tone:{toneLabel}</span>
          <span className="pill">palette:{paletteLabel}</span>
          <span className={`pill ${dirty ? "pill--warning" : "pill--accent"}`}>{dirty ? "refresh-needed" : "output-synced"}</span>
        </div>
      </div>

      <div className="viewer-stage">
        {isCompare ? (
          <div className="compare-grid">
            <div className="compare-grid__slot">
              <div className="compare-grid__label">Original</div>
              <StageImage src={originalPreviewUrl} alt="Original DICOM preview" />
            </div>
            <div className="compare-grid__slot">
              <div className="compare-grid__label">Processed</div>
              <StageImage src={processedPreviewUrl} alt="Processed DICOM preview" />
            </div>
          </div>
        ) : (
          <StageImage
            src={primaryImage}
            alt={activeMode === "processed" ? "Processed DICOM preview" : "Original DICOM preview"}
          />
        )}
      </div>
    </section>
  );
}
