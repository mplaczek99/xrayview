import { useEffect, useState } from "react";

interface DicomViewerProps {
  previewUrl: string | null;
  emptyTitle?: string;
  emptyDescription?: string;
}

// Deprecated as the primary study viewer in Phase 6. Processing still uses
// this lightweight image stage while the interactive View tab moves to
// `features/viewer/ViewerCanvas.tsx`.
export function DicomViewer({
  previewUrl,
  emptyTitle = "No image loaded",
  emptyDescription = "Open a DICOM study or BMP/TIFF image to view it here.",
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
