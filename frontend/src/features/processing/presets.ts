import type {
  ProcessingControls,
  ProcessingManifest,
} from "../../lib/generated/contracts";
import type { ProcessingPresetOption } from "../../lib/types";

interface ProcessingUiState {
  defaultControls: ProcessingControls;
  presets: ProcessingPresetOption[];
}

const CUSTOM_PRESET_LABEL = "Custom";

const PRESET_COPY_BY_ID: Record<string, { label: string; description: string }> = {
  default: {
    label: "Neutral",
    description: "Balanced grayscale for a first review pass.",
  },
  xray: {
    label: "Bone Focus",
    description: "Bone palette with added punch and equalized detail.",
  },
  "high-contrast": {
    label: "High Contrast",
    description: "Sharper tonal separation without pseudocolor.",
  },
};

function toTitleCase(value: string): string {
  return value
    .split("-")
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(" ");
}

function presetCopy(presetId: string): { label: string; description: string } {
  return (
    PRESET_COPY_BY_ID[presetId] ?? {
      label: toTitleCase(presetId),
      description: "Backend-defined processing preset.",
    }
  );
}

export function processingControlsEqual(
  left: ProcessingControls,
  right: ProcessingControls,
): boolean {
  // Sliders round contrast values in the UI, so allow a small tolerance before
  // deciding the current controls no longer match a preset exactly.
  return (
    left.brightness === right.brightness &&
    Math.abs(left.contrast - right.contrast) < 0.05 &&
    left.invert === right.invert &&
    left.equalize === right.equalize &&
    left.palette === right.palette
  );
}

export function buildProcessingUiState(
  manifest: ProcessingManifest,
): ProcessingUiState {
  const presets = manifest.presets.map((preset) => ({
    id: preset.id,
    controls: { ...preset.controls },
    ...presetCopy(preset.id),
  }));

  const defaultPreset =
    manifest.presets.find((preset) => preset.id === manifest.defaultPresetId) ??
    manifest.presets[0];

  return {
    defaultControls: defaultPreset
      ? { ...defaultPreset.controls }
      : {
          brightness: 0,
          contrast: 1.0,
          invert: false,
          equalize: false,
          palette: "none",
        },
    presets,
  };
}

export function matchPreset(
  controls: ProcessingControls,
  presets: readonly ProcessingPresetOption[],
): string {
  const matched = presets.find((preset) =>
    processingControlsEqual(preset.controls, controls),
  );

  return matched?.label ?? CUSTOM_PRESET_LABEL;
}
