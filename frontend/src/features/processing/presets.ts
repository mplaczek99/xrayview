import type { ProcessingControls, ProcessingPreset } from "../../lib/types";

export const DEFAULT_CONTROLS: ProcessingControls = {
  brightness: 0,
  contrast: 1.0,
  invert: false,
  equalize: false,
  palette: "none",
};

export const PROCESSING_PRESETS: ProcessingPreset[] = [
  {
    label: "Neutral",
    description: "Balanced grayscale for a first review pass.",
    controls: { ...DEFAULT_CONTROLS },
  },
  {
    label: "Bone Focus",
    description: "Bone palette with added punch and equalized detail.",
    controls: {
      brightness: 10,
      contrast: 1.4,
      invert: false,
      equalize: true,
      palette: "bone",
    },
  },
  {
    label: "High Contrast",
    description: "Sharper tonal separation without pseudocolor.",
    controls: {
      brightness: 0,
      contrast: 1.8,
      invert: false,
      equalize: true,
      palette: "none",
    },
  },
];

export function matchPreset(controls: ProcessingControls): string {
  const matched = PROCESSING_PRESETS.find((preset) => {
    const candidate = preset.controls;
    return (
      candidate.brightness === controls.brightness &&
      Math.abs(candidate.contrast - controls.contrast) < 0.05 &&
      candidate.invert === controls.invert &&
      candidate.equalize === controls.equalize &&
      candidate.palette === controls.palette
    );
  });

  return matched?.label ?? "Custom";
}
