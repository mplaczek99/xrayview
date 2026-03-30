import type { ChangeEvent } from "react";
import type { ProcessingControls, ProcessingPreset } from "../../lib/types";

interface ProcessingLabProps {
  controls: ProcessingControls;
  presets: ProcessingPreset[];
  dirty: boolean;
  onPresetSelect: (preset: ProcessingPreset) => void;
  onChange: (next: ProcessingControls) => void;
}

export function ProcessingLab({ controls, presets, dirty, onPresetSelect, onChange }: ProcessingLabProps) {
  function update<K extends keyof ProcessingControls>(key: K, value: ProcessingControls[K]) {
    onChange({ ...controls, [key]: value });
  }

  return (
    <div className="processing-lab">
      <div className="preset-grid">
        {presets.map((preset) => (
          <button key={preset.label} className="preset-chip" type="button" onClick={() => onPresetSelect(preset)}>
            <strong>{preset.label}</strong>
            <span>{preset.description}</span>
          </button>
        ))}
      </div>

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
          onChange={(event: ChangeEvent<HTMLInputElement>) => update("contrast", Number(event.target.value))}
        />
      </div>

      <div className="control-block">
        <label className="control-block__label" htmlFor="palette-select">
          <span>Palette</span>
          <strong>{controls.palette}</strong>
        </label>
        <select id="palette-select" value={controls.palette} onChange={(event) => update("palette", event.target.value as ProcessingControls["palette"])}>
          <option value="none">none</option>
          <option value="hot">hot</option>
          <option value="bone">bone</option>
        </select>
      </div>

      <label className="toggle-row">
        <input type="checkbox" checked={controls.invert} onChange={(event) => update("invert", event.target.checked)} />
        <span>Invert grayscale</span>
      </label>

      <label className="toggle-row">
        <input type="checkbox" checked={controls.equalize} onChange={(event) => update("equalize", event.target.checked)} />
        <span>Equalize histogram</span>
      </label>

      <div className={`status-banner ${dirty ? "status-banner--warning" : ""}`}>
        {dirty
          ? "Controls changed after the last render. Save stays locked until the next render refreshes the output."
          : "The rendered output matches the current controls."}
      </div>
    </div>
  );
}
