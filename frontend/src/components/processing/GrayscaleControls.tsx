import type { ProcessingControls } from "../../lib/generated/contracts";

interface GrayscaleControlsProps {
  controls: ProcessingControls;
  busy: boolean;
  onUpdateControl: <K extends keyof ProcessingControls>(
    key: K,
    value: ProcessingControls[K],
  ) => void;
}

const BRIGHTNESS_MIN = -100;
const BRIGHTNESS_MAX = 100;
const CONTRAST_MIN = 0.1;
const CONTRAST_MAX = 3.0;

// clamp keeps spinbutton input inside the slider's range so the two stay in
// sync and we never ship an out-of-range value to the backend. Returns null
// for NaN (empty field, lone "-" or ".") so mid-edit states don't yank the
// value to min while the user is still typing a negative or decimal.
function clamp(value: number, min: number, max: number): number | null {
  if (Number.isNaN(value)) {
    return null;
  }
  return Math.min(max, Math.max(min, value));
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

      <div className="form-field form-field--slider">
        <label className="form-field__label" htmlFor="proc-brightness">
          Brightness
        </label>
        <input
          id="proc-brightness-range"
          className="form-range"
          type="range"
          min={BRIGHTNESS_MIN}
          max={BRIGHTNESS_MAX}
          step={1}
          value={controls.brightness}
          onChange={(event) =>
            onUpdateControl(
              "brightness",
              parseInt(event.target.value, 10),
            )
          }
          disabled={busy}
        />
        <input
          id="proc-brightness"
          className="form-input form-input--number"
          type="number"
          min={BRIGHTNESS_MIN}
          max={BRIGHTNESS_MAX}
          value={controls.brightness}
          step={1}
          onChange={(event) => {
            const next = clamp(
              parseInt(event.target.value, 10),
              BRIGHTNESS_MIN,
              BRIGHTNESS_MAX,
            );
            if (next !== null) {
              onUpdateControl("brightness", next);
            }
          }}
          disabled={busy}
        />
      </div>

      <div className="form-field form-field--slider">
        <label className="form-field__label" htmlFor="proc-contrast">
          Contrast
        </label>
        <input
          id="proc-contrast-range"
          className="form-range"
          type="range"
          min={CONTRAST_MIN}
          max={CONTRAST_MAX}
          step={0.1}
          value={controls.contrast}
          onChange={(event) =>
            onUpdateControl(
              "contrast",
              parseFloat(event.target.value),
            )
          }
          disabled={busy}
        />
        <input
          id="proc-contrast"
          className="form-input form-input--number"
          type="number"
          value={controls.contrast}
          step={0.1}
          min={CONTRAST_MIN}
          max={CONTRAST_MAX}
          onChange={(event) => {
            const next = clamp(
              parseFloat(event.target.value),
              CONTRAST_MIN,
              CONTRAST_MAX,
            );
            if (next !== null) {
              onUpdateControl("contrast", next);
            }
          }}
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
