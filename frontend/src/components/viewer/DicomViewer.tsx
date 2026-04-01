interface DicomViewerProps {
  previewUrl: string | null;
  emptyTitle?: string;
  emptyDescription?: string;
}

export function DicomViewer({
  previewUrl,
  emptyTitle = "No image loaded",
  emptyDescription = "Open a DICOM file to view it here.",
}: DicomViewerProps) {
  return (
    <div className="viewer-stage">
      {previewUrl ? (
        <img
          className="viewer-stage__image"
          src={previewUrl}
          alt="DICOM preview"
          draggable={false}
        />
      ) : (
        <div className="viewer-placeholder">
          <div className="viewer-placeholder__title">{emptyTitle}</div>
          <p className="viewer-placeholder__copy">{emptyDescription}</p>
        </div>
      )}
    </div>
  );
}
