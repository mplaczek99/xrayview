import type { Palette } from "./types";
import type {
  AnnotationBundle,
  LineAnnotation,
  ToothAnalysis,
} from "./generated/contracts";

// Mock previews are deterministic SVG data URLs, so cache by variant instead
// of re-encoding the same image every time controls rerender the UI.
const previewCache = new Map<string, string>();

function encodeSvg(svg: string): string {
  return `data:image/svg+xml;charset=UTF-8,${encodeURIComponent(svg)}`;
}

function overlayMarkup(palette: Palette, processed: boolean): string {
  if (!processed) {
    return "";
  }

  // The overlay exaggerates "findings" only on processed output so the mock
  // app demonstrates before/after behavior without a real backend.
  const tint =
    palette === "hot"
      ? "rgba(255,132,92,0.36)"
      : palette === "bone"
        ? "rgba(94,217,197,0.24)"
        : "rgba(86,207,226,0.22)";

  return `
    <rect x="118" y="94" width="170" height="88" rx="18" fill="${tint}" />
    <rect x="312" y="84" width="194" height="112" rx="22" fill="${tint}" />
    <rect x="520" y="236" width="206" height="102" rx="24" fill="${tint}" />
    <circle cx="404" cy="292" r="36" fill="rgba(244, 96, 154, 0.46)" />
    <circle cx="274" cy="214" r="20" fill="rgba(244, 96, 154, 0.42)" />
    <g stroke="rgba(222, 233, 111, 0.95)" stroke-width="2">
      <line x1="210" y1="96" x2="194" y2="208" />
      <line x1="360" y1="86" x2="350" y2="208" />
      <line x1="620" y1="234" x2="598" y2="360" />
    </g>
    <g font-family="Aptos, Segoe UI, sans-serif" font-size="12" font-weight="700">
      <rect x="260" y="126" width="92" height="28" rx="10" fill="rgba(53, 200, 156, 0.9)" />
      <text x="306" y="144" fill="#07120d" text-anchor="middle">Calculus</text>
      <rect x="374" y="182" width="86" height="48" rx="12" fill="rgba(243, 72, 149, 0.92)" />
      <text x="417" y="200" fill="#fff" text-anchor="middle">Caries</text>
      <text x="417" y="219" fill="#ffe7f2" text-anchor="middle">Dentin 84%</text>
    </g>
  `;
}

export function createMockPreview(processed: boolean, palette: Palette): string {
  const cacheKey = `${processed}:${palette}`;
  const cached = previewCache.get(cacheKey);
  if (cached) {
    return cached;
  }

  const preview = encodeSvg(`
    <svg xmlns="http://www.w3.org/2000/svg" width="1200" height="820" viewBox="0 0 1200 820">
      <defs>
        <linearGradient id="bg" x1="0" x2="1">
          <stop offset="0%" stop-color="#0b1015" />
          <stop offset="100%" stop-color="#131a22" />
        </linearGradient>
        <radialGradient id="scanGlow" cx="50%" cy="42%" r="58%">
          <stop offset="0%" stop-color="#f3f4ef" stop-opacity="0.92" />
          <stop offset="56%" stop-color="#d8dad4" stop-opacity="0.72" />
          <stop offset="100%" stop-color="#a9aea8" stop-opacity="0.16" />
        </radialGradient>
      </defs>
      <rect width="1200" height="820" fill="url(#bg)" />
      <rect x="68" y="52" width="1064" height="716" rx="42" fill="#06090d" stroke="rgba(255,255,255,0.08)" />
      <path d="M180 152 L938 118 L1046 230 L1020 638 L190 706 L96 582 L112 238 Z" fill="url(#scanGlow)" opacity="0.98" />
      <rect x="82" y="392" width="1032" height="22" fill="rgba(31, 35, 39, 0.86)" />
      <g fill="rgba(245,245,242,0.98)">
        <ellipse cx="258" cy="256" rx="112" ry="146" />
        <ellipse cx="430" cy="250" rx="116" ry="150" />
        <ellipse cx="616" cy="246" rx="124" ry="154" />
        <ellipse cx="790" cy="252" rx="114" ry="150" />
        <ellipse cx="286" cy="562" rx="138" ry="170" />
        <ellipse cx="492" cy="560" rx="136" ry="166" />
        <ellipse cx="696" cy="550" rx="132" ry="160" />
        <ellipse cx="894" cy="542" rx="136" ry="170" />
      </g>
      <g stroke="rgba(160, 166, 170, 0.34)" stroke-width="3" fill="none">
        <path d="M182 158 C250 120 344 106 418 120" />
        <path d="M406 136 C462 98 586 90 676 112" />
        <path d="M312 516 C404 470 534 470 616 502" />
        <path d="M650 504 C742 462 874 466 964 518" />
      </g>
      ${overlayMarkup(palette, processed)}
    </svg>
  `);

  previewCache.set(cacheKey, preview);
  return preview;
}

export function createMockToothAnalysis(): ToothAnalysis {
  return {
    image: {
      width: 1200,
      height: 820,
    },
    calibration: {
      pixelUnits: "px",
      measurementScale: null,
      realWorldMeasurementsAvailable: false,
    },
    tooth: {
      confidence: 0.74,
      maskAreaPixels: 18492,
      measurements: {
        pixel: {
          toothWidth: 133,
          toothHeight: 201,
          boundingBoxWidth: 140,
          boundingBoxHeight: 220,
          units: "px",
        },
        calibrated: null,
      },
      geometry: {
        boundingBox: {
          x: 422,
          y: 414,
          width: 140,
          height: 220,
        },
        widthLine: {
          start: { x: 428, y: 454 },
          end: { x: 560, y: 454 },
        },
        heightLine: {
          start: { x: 492, y: 420 },
          end: { x: 492, y: 620 },
        },
      },
    },
    warnings: [
      "Calibration metadata unavailable; returning pixel measurements only.",
    ],
  };
}

export function createMockSuggestedAnnotations(): AnnotationBundle {
  return {
    lines: [
      {
        id: "auto-tooth-width",
        label: "Tooth width",
        source: "autoTooth",
        start: { x: 428, y: 454 },
        end: { x: 560, y: 454 },
        editable: true,
        confidence: 0.74,
        measurement: {
          pixelLength: 132,
          calibratedLengthMm: null,
        },
      },
      {
        id: "auto-tooth-height",
        label: "Tooth height",
        source: "autoTooth",
        start: { x: 492, y: 420 },
        end: { x: 492, y: 620 },
        editable: true,
        confidence: 0.74,
        measurement: {
          pixelLength: 200,
          calibratedLengthMm: null,
        },
      },
    ],
    rectangles: [
      {
        id: "auto-tooth-bounding-box",
        label: "Tooth bounding box",
        source: "autoTooth",
        x: 422,
        y: 414,
        width: 140,
        height: 220,
        editable: false,
        confidence: 0.74,
      },
    ],
  };
}

export function measureMockLineAnnotation(annotation: LineAnnotation): LineAnnotation {
  const dx = annotation.end.x - annotation.start.x;
  const dy = annotation.end.y - annotation.start.y;
  const pixelLength = Math.round(Math.hypot(dx, dy) * 10) / 10;

  return {
    ...annotation,
    measurement: {
      pixelLength,
      calibratedLengthMm: null,
    },
  };
}
