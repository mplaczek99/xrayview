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
  onModeChange: (mode: ViewerMode) => void;
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
  onModeChange,
}: ViewerStageProps) {
  const isCompare = activeMode === "compare" && canCompare;
  const primaryImage = activeMode === "processed" ? processedPreviewUrl : originalPreviewUrl;

  return (
    <section className="viewer-shell">
      <div className="viewer-shell__header">
        <div>
          <div className="panel-card__eyebrow">Large Canvas</div>
          <h2 className="viewer-shell__title">Viewer + Compare</h2>
          <p className="viewer-shell__subtitle">
            Baseline layout for a future report-heavy workstation, with the source image centered and controls kept
            at the edge.
          </p>
        </div>

        <div className="viewer-shell__meta">
          <span className="pill">Recipe {recipeName}</span>
          <span className="pill">{toneLabel}</span>
          <span className="pill">Palette {paletteLabel}</span>
          <span className={`pill ${dirty ? "pill--warning" : "pill--accent"}`}>{dirty ? "Refresh needed" : "Output synced"}</span>
        </div>
      </div>

      <div className="mode-switch">
        <button className={`mode-switch__button ${activeMode === "original" ? "is-active" : ""}`} type="button" onClick={() => onModeChange("original")}>
          Original
        </button>
        <button className={`mode-switch__button ${activeMode === "processed" ? "is-active" : ""}`} type="button" onClick={() => onModeChange("processed")} disabled={!processedPreviewUrl}>
          Processed
        </button>
        <button className={`mode-switch__button ${activeMode === "compare" ? "is-active" : ""}`} type="button" onClick={() => onModeChange("compare")} disabled={!canCompare}>
          Compare
        </button>
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
