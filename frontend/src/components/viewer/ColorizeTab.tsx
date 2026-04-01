interface ColorizeTabProps {
  previewUrl: string | null;
}

export function ColorizeTab({ previewUrl }: ColorizeTabProps) {
  return (
    <div className="colorize-tab">
      <div className="colorize-tab__controls">
        {/* Colorization controls will go here */}
        <div className="colorize-tab__placeholder">
          <p className="colorize-tab__placeholder-text">
            Colorization controls will go here
          </p>
        </div>
      </div>

      <div className="colorize-tab__preview">
        <div className="viewer-stage">
          {previewUrl ? (
            <img
              className="viewer-stage__image"
              src={previewUrl}
              alt="DICOM preview for colorization"
              draggable={false}
            />
          ) : (
            <div className="viewer-placeholder">
              <div className="viewer-placeholder__title">No study loaded</div>
              <p className="viewer-placeholder__copy">
                Open a DICOM file in the View tab to see a preview here.
              </p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
