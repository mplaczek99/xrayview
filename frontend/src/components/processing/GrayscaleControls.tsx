import type { ProcessingControls } from "../../lib/generated/contracts";

interface GrayscaleControlsProps {
  controls: ProcessingControls;
  busy: boolean;
  onUpdateControl: <K extends keyof ProcessingControls>(
    key: K,
    value: ProcessingControls[K],
  ) => void;
}

export function GrayscaleControls({
  controls,
  busy,
  onUpdateControl,
}: GrayscaleControlsProps) {
  return (
    <section className="form-section">
      <div className="form-label">Grayscale Controls</div>

      <label className="form-toggle">
        <input
          type="checkbox"
          checked={controls.invert}
          onChange={(event) => onUpdateControl("invert", event.target.checked)}
          disabled={busy}
        />
        <span>Invert</span>
      </label>

      <div className="form-field">
        <label className="form-field__label" htmlFor="proc-brightness">
          Brightness
        </label>
        <input
          id="proc-brightness"
          className="form-input form-input--number"
          type="number"
          value={controls.brightness}
          step={1}
          onChange={(event) =>
            onUpdateControl(
              "brightness",
              parseInt(event.target.value, 10) || 0,
            )
          }
          disabled={busy}
        />
      </div>

      <div className="form-field">
        <label className="form-field__label" htmlFor="proc-contrast">
          Contrast
        </label>
        <input
          id="proc-contrast"
          className="form-input form-input--number"
          type="number"
          value={controls.contrast}
          step={0.1}
          min={0}
          onChange={(event) =>
            onUpdateControl(
              "contrast",
              parseFloat(event.target.value) || 0,
            )
          }
          disabled={busy}
        />
      </div>

      <label className="form-toggle">
        <input
          type="checkbox"
          checked={controls.equalize}
          onChange={(event) =>
            onUpdateControl("equalize", event.target.checked)
          }
          disabled={busy}
        />
        <span>Equalize</span>
      </label>
    </section>
  );
}
