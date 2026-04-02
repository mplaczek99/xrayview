import type { ChangeEvent } from "react";
import type { ProcessingControls } from "../../lib/generated/contracts";
import type { ProcessingPresetOption } from "../../lib/types";

// Deprecated: this experimental inspector is disconnected from the current app
// shell and should not be expanded while the Phase 0-2 processing rewrite is
// underway.
interface ProcessingLabProps {
  controls: ProcessingControls;
  presets: ProcessingPresetOption[];
  busy: boolean;
  dirty: boolean;
  onPresetSelect: (preset: ProcessingPresetOption) => void;
  onChange: (next: ProcessingControls) => void;
}

export function ProcessingLab({ controls, presets, busy, dirty, onPresetSelect, onChange }: ProcessingLabProps) {
  function update<K extends keyof ProcessingControls>(key: K, value: ProcessingControls[K]) {
    // Bubble up a full controls object so the parent can derive dirty/preset
    // state from one immutable snapshot.
    onChange({ ...controls, [key]: value });
  }

  return (
    <div className="processing-lab">
      <section className="inspector-section">
        <div className="inspector-section__heading">PRESETS</div>
        <div className="preset-grid">
          {presets.map((preset) => (
            <button
              key={preset.id}
              className="preset-chip"
              type="button"
              onClick={() => onPresetSelect(preset)}
              disabled={busy}
            >
              <strong>{preset.label}</strong>
              <span>{preset.description}</span>
            </button>
          ))}
        </div>
      </section>

      <section className="inspector-section">
        <div className="inspector-section__heading">TONE</div>

        <div className="control-block">
          <label className="control-block__label" htmlFor="brightness-range">
            <span>Brightness</span>
            <strong>{controls.brightness >= 0 ? `+${controls.brightness}` : controls.brightness}</strong>
          </label>
          <input
            id="brightness-range"
            type="range"
            min={-100}
            max={100}
            step={1}
            value={controls.brightness}
            disabled={busy}
            onChange={(event: ChangeEvent<HTMLInputElement>) => update("brightness", Number(event.target.value))}
          />
        </div>

        <div className="control-block">
          <label className="control-block__label" htmlFor="contrast-range">
            <span>Contrast</span>
            <strong>{controls.contrast.toFixed(1)}x</strong>
          </label>
          <input
            id="contrast-range"
            type="range"
            min={0.5}
            max={2}
            step={0.1}
            value={controls.contrast}
            disabled={busy}
            onChange={(event: ChangeEvent<HTMLInputElement>) => update("contrast", Number(event.target.value))}
          />
        </div>

        <div className="control-block">
          <label className="control-block__label" htmlFor="palette-select">
            <span>Palette</span>
            <strong>{controls.palette}</strong>
          </label>
          <select
            id="palette-select"
            value={controls.palette}
            disabled={busy}
            onChange={(event) => update("palette", event.target.value as ProcessingControls["palette"])}
          >
            <option value="none">none</option>
            <option value="hot">hot</option>
            <option value="bone">bone</option>
          </select>
        </div>
      </section>

      <section className="inspector-section">
        <div className="inspector-section__heading">FLAGS</div>

        <label className="toggle-row">
          <input
            type="checkbox"
            checked={controls.invert}
            disabled={busy}
            onChange={(event) => update("invert", event.target.checked)}
          />
          <span>Invert grayscale</span>
        </label>

        <label className="toggle-row">
          <input
            type="checkbox"
            checked={controls.equalize}
            disabled={busy}
            onChange={(event) => update("equalize", event.target.checked)}
          />
          <span>Equalize histogram</span>
        </label>
      </section>

      <div className={`status-banner ${dirty ? "status-banner--warning" : ""}`}>
        {busy
          ? "Controls are locked until the current task finishes so the next output stays in sync."
          : dirty
          ? "Controls changed after the last render. Save stays locked until the next render refreshes the output."
          : "The rendered output matches the current controls."}
      </div>
    </div>
  );
}
