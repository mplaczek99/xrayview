import { useEffect, useState } from "react";

import type { ToothGeometry } from "../../lib/generated/contracts";

interface DicomViewerProps {
  previewUrl: string | null;
  imageSize?: { width: number; height: number } | null;
  overlay?: ToothGeometry | null;
  emptyTitle?: string;
  emptyDescription?: string;
}

// Presentation-only viewer: callers decide where the preview URL came from
// and provide the empty-state copy that matches their workflow.
export function DicomViewer({
  previewUrl,
  imageSize = null,
  overlay = null,
  emptyTitle = "No image loaded",
  emptyDescription = "Open a DICOM file to view it here.",
}: DicomViewerProps) {
  const [loadFailed, setLoadFailed] = useState(false);

  useEffect(() => {
    setLoadFailed(false);
  }, [previewUrl]);

  return (
    <div className="viewer-stage">
      {previewUrl && !loadFailed ? (
        <div className="viewer-stage__media">
          <img
            className="viewer-stage__image"
            src={previewUrl}
            alt="DICOM preview"
            draggable={false}
            onError={() => setLoadFailed(true)}
          />
          {overlay && imageSize && (
            <svg
              className="viewer-stage__overlay"
              viewBox={`0 0 ${imageSize.width} ${imageSize.height}`}
              preserveAspectRatio="xMidYMid meet"
              aria-hidden="true"
            >
              <rect
                className="viewer-stage__overlay-box"
                x={overlay.boundingBox.x}
                y={overlay.boundingBox.y}
                width={overlay.boundingBox.width}
                height={overlay.boundingBox.height}
              />
              <line
                className="viewer-stage__overlay-line viewer-stage__overlay-line--width"
                x1={overlay.widthLine.start.x}
                y1={overlay.widthLine.start.y}
                x2={overlay.widthLine.end.x}
                y2={overlay.widthLine.end.y}
              />
              <line
                className="viewer-stage__overlay-line viewer-stage__overlay-line--height"
                x1={overlay.heightLine.start.x}
                y1={overlay.heightLine.start.y}
                x2={overlay.heightLine.end.x}
                y2={overlay.heightLine.end.y}
              />
            </svg>
          )}
        </div>
      ) : (
        <div className="viewer-placeholder">
          <div className="viewer-placeholder__title">
            {loadFailed ? "Preview Unavailable" : emptyTitle}
          </div>
          <p className="viewer-placeholder__copy">
            {loadFailed
              ? "The rendered preview file could not be loaded by the desktop webview."
              : emptyDescription}
          </p>
        </div>
      )}
    </div>
  );
}
