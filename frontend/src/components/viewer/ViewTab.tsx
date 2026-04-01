interface ViewTabProps {
  previewUrl: string | null;
  busy: boolean;
  onOpenStudy: () => void;
}

export function ViewTab({ previewUrl, busy, onOpenStudy }: ViewTabProps) {
  return (
    <div className="view-tab">
      <div className="view-tab__toolbar">
        <button
          className="button button--primary"
          type="button"
          onClick={onOpenStudy}
          disabled={busy}
        >
          {busy ? "Loading..." : "Open DICOM"}
        </button>
      </div>

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
            <div className="viewer-placeholder__title">No study loaded</div>
            <p className="viewer-placeholder__copy">
              Open a DICOM file to view it here.
            </p>
          </div>
        )}
      </div>
    </div>
  );
}
