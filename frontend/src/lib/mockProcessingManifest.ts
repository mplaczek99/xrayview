import type { ProcessingManifest } from "./types";

export const MOCK_PROCESSING_MANIFEST: ProcessingManifest = {
  defaultPresetId: "default",
  presets: [
    {
      id: "default",
      controls: {
        brightness: 0,
        contrast: 1.0,
        invert: false,
        equalize: false,
        palette: "none",
      },
    },
    {
      id: "xray",
      controls: {
        brightness: 10,
        contrast: 1.4,
        invert: false,
        equalize: true,
        palette: "bone",
      },
    },
    {
      id: "high-contrast",
      controls: {
        brightness: 0,
        contrast: 1.8,
        invert: false,
        equalize: true,
        palette: "none",
      },
    },
  ],
};
