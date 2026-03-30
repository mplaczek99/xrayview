import { useEffect, useMemo, useRef, useState } from "react";
import type { PointerEvent as ReactPointerEvent } from "react";
import type { MeasurementScale, ViewerMode } from "../../lib/types";

type MeasurementImageKind = "original" | "processed";

interface ViewerStageProps {
  activeMode: ViewerMode;
  canCompare: boolean;
  originalPreviewUrl: string | null;
  processedPreviewUrl: string | null;
  originalMeasurementScale: MeasurementScale | null;
  processedMeasurementScale: MeasurementScale | null;
  recipeName: string;
  toneLabel: string;
  paletteLabel: string;
  dirty: boolean;
}

interface MeasurementPoint {
  x: number;
  y: number;
}

interface ViewerMeasurement {
  id: string;
  start: MeasurementPoint;
  end: MeasurementPoint;
}

interface ImageSize {
  width: number;
  height: number;
}

interface StageImageProps {
  src: string | null;
  alt: string;
  imageKind: MeasurementImageKind;
  measurementMode: boolean;
  measurements: ViewerMeasurement[];
  draftStart: MeasurementPoint | null;
  hoverPoint: MeasurementPoint | null;
  imageSize: ImageSize | null;
  measurementScale: MeasurementScale | null;
  onImageLoad: (imageKind: MeasurementImageKind, imageSize: ImageSize) => void;
  onPointerDown: (
    imageKind: MeasurementImageKind,
    event: ReactPointerEvent<HTMLDivElement>,
  ) => void;
  onPointerMove: (
    imageKind: MeasurementImageKind,
    event: ReactPointerEvent<HTMLDivElement>,
  ) => void;
  onPointerLeave: (imageKind: MeasurementImageKind) => void;
}

const VIEWER_MODE_CONTENT: Record<
  ViewerMode,
  {
    editorName: string;
    editorTitle: string;
    editorDescription: string;
    primaryImageAlt: string;
    primaryImageKind: MeasurementImageKind;
  }
> = {
  original: {
    editorName: "source.dcm",
    editorTitle: "Source Preview",
    editorDescription:
      "Keep the original DICOM pinned while you tune the processing pipeline from the inspector.",
    primaryImageAlt: "Original DICOM preview",
    primaryImageKind: "original",
  },
  processed: {
    editorName: "processed.dcm",
    editorTitle: "Processed Preview",
    editorDescription: "Inspect the rendered derivative in the active editor surface.",
    primaryImageAlt: "Processed DICOM preview",
    primaryImageKind: "processed",
  },
  compare: {
    editorName: "compare.diff",
    editorTitle: "Compare Editor",
    editorDescription:
      "Review baseline and rendered output side-by-side before exporting the derived study.",
    primaryImageAlt: "Original DICOM preview",
    primaryImageKind: "original",
  },
};

function emptyMeasurements(): Record<MeasurementImageKind, ViewerMeasurement[]> {
  return { original: [], processed: [] };
}

function emptyPoints(): Record<MeasurementImageKind, MeasurementPoint | null> {
  return { original: null, processed: null };
}

function emptyImageSizes(): Record<MeasurementImageKind, ImageSize | null> {
  return { original: null, processed: null };
}

function clamp01(value: number): number {
  return Math.max(0, Math.min(1, value));
}

function pointFromPointer(event: ReactPointerEvent<HTMLDivElement>): MeasurementPoint {
  const rect = event.currentTarget.getBoundingClientRect();
  return {
    x: rect.width === 0 ? 0 : clamp01((event.clientX - rect.left) / rect.width),
    y: rect.height === 0 ? 0 : clamp01((event.clientY - rect.top) / rect.height),
  };
}

function measurementDistancePx(measurement: ViewerMeasurement, imageSize: ImageSize | null): number {
  if (!imageSize) {
    return 0;
  }

  const deltaX = (measurement.end.x - measurement.start.x) * imageSize.width;
  const deltaY = (measurement.end.y - measurement.start.y) * imageSize.height;
  return Math.hypot(deltaX, deltaY);
}

function measurementDistanceMm(
  measurement: ViewerMeasurement,
  imageSize: ImageSize | null,
  measurementScale: MeasurementScale | null,
): number | null {
  if (!imageSize || !measurementScale) {
    return null;
  }

  const deltaX = (measurement.end.x - measurement.start.x) * imageSize.width;
  const deltaY = (measurement.end.y - measurement.start.y) * imageSize.height;

  return Math.hypot(
    deltaX * measurementScale.columnSpacingMm,
    deltaY * measurementScale.rowSpacingMm,
  );
}

function measurementLabel(
  measurement: ViewerMeasurement,
  imageSize: ImageSize | null,
  measurementScale: MeasurementScale | null,
): string {
  const millimeters = measurementDistanceMm(measurement, imageSize, measurementScale);
  if (millimeters !== null) {
    if (millimeters >= 100) {
      return `${Math.round(millimeters)} mm`;
    }
    if (millimeters >= 10) {
      return `${millimeters.toFixed(1)} mm`;
    }
    return `${millimeters.toFixed(2)} mm`;
  }

  const pixels = measurementDistancePx(measurement, imageSize);
  if (pixels >= 100) {
    return `${Math.round(pixels)} px`;
  }
  return `${pixels.toFixed(1)} px`;
}

function midpoint(start: MeasurementPoint, end: MeasurementPoint): MeasurementPoint {
  return {
    x: (start.x + end.x) / 2,
    y: (start.y + end.y) / 2,
  };
}

function measurementCountLabel(count: number): string {
  return `${count} measurement${count === 1 ? "" : "s"}`;
}

function comparePaneLabel(label: string, measurementCount: number): string {
  if (measurementCount === 0) {
    return label;
  }

  return `${label} · ${measurementCountLabel(measurementCount)}`;
}

function measurementSourceLabel(measurementScale: MeasurementScale | null): string {
  if (!measurementScale) {
    return "pixel distances only";
  }

  switch (measurementScale.source) {
    case "PixelSpacing":
      return "DICOM pixel spacing";
    case "ImagerPixelSpacing":
      return "imager pixel spacing";
    case "NominalScannedPixelSpacing":
      return "nominal scanned spacing";
    default:
      return "DICOM spacing metadata";
  }
}

function MeasurementOverlay({
  measurements,
  draftStart,
  hoverPoint,
  imageSize,
  measurementScale,
}: {
  measurements: ViewerMeasurement[];
  draftStart: MeasurementPoint | null;
  hoverPoint: MeasurementPoint | null;
  imageSize: ImageSize | null;
  measurementScale: MeasurementScale | null;
}) {
  const draftEnd = draftStart && hoverPoint ? hoverPoint : null;

  if (measurements.length === 0 && !draftEnd) {
    return null;
  }

  return (
    <div className="viewer-image-frame__overlay" aria-hidden="true">
      <svg className="viewer-image-frame__svg" viewBox="0 0 100 100">
        {measurements.map((measurement) => (
          <g key={measurement.id}>
            <line
              className="viewer-measurement__line"
              x1={measurement.start.x * 100}
              y1={measurement.start.y * 100}
              x2={measurement.end.x * 100}
              y2={measurement.end.y * 100}
            />
            <circle
              className="viewer-measurement__point"
              cx={measurement.start.x * 100}
              cy={measurement.start.y * 100}
              r="0.9"
            />
            <circle
              className="viewer-measurement__point"
              cx={measurement.end.x * 100}
              cy={measurement.end.y * 100}
              r="0.9"
            />
          </g>
        ))}

        {draftStart && draftEnd ? (
          <g>
            <line
              className="viewer-measurement__line viewer-measurement__line--draft"
              x1={draftStart.x * 100}
              y1={draftStart.y * 100}
              x2={draftEnd.x * 100}
              y2={draftEnd.y * 100}
            />
            <circle
              className="viewer-measurement__point viewer-measurement__point--draft"
              cx={draftStart.x * 100}
              cy={draftStart.y * 100}
              r="0.9"
            />
            <circle
              className="viewer-measurement__point viewer-measurement__point--draft"
              cx={draftEnd.x * 100}
              cy={draftEnd.y * 100}
              r="0.9"
            />
          </g>
        ) : null}
      </svg>

      {measurements.map((measurement) => {
        const center = midpoint(measurement.start, measurement.end);
        return (
          <div
            key={`${measurement.id}-label`}
            className="viewer-measurement__tag"
            style={{ left: `${center.x * 100}%`, top: `${center.y * 100}%` }}
          >
            {measurementLabel(measurement, imageSize, measurementScale)}
          </div>
        );
      })}
    </div>
  );
}

function StageImage({
  src,
  alt,
  imageKind,
  measurementMode,
  measurements,
  draftStart,
  hoverPoint,
  imageSize,
  measurementScale,
  onImageLoad,
  onPointerDown,
  onPointerMove,
  onPointerLeave,
}: StageImageProps) {
  if (!src) {
    return (
      <div className="viewer-placeholder">
        <div className="viewer-placeholder__title">Preview waiting</div>
        <p className="viewer-placeholder__copy">
          Load a DICOM study and render a processed output to unlock compare mode and export.
        </p>
      </div>
    );
  }

  return (
    <div
      className={`viewer-image-frame ${measurementMode ? "is-measuring" : ""}`}
      onPointerDown={measurementMode ? (event) => onPointerDown(imageKind, event) : undefined}
      onPointerMove={measurementMode ? (event) => onPointerMove(imageKind, event) : undefined}
      onPointerLeave={measurementMode ? () => onPointerLeave(imageKind) : undefined}
    >
      <img
        className="viewer-stage__image"
        src={src}
        alt={alt}
        draggable={false}
        onLoad={(event) =>
          onImageLoad(imageKind, {
            width: event.currentTarget.naturalWidth,
            height: event.currentTarget.naturalHeight,
          })
        }
      />
      <MeasurementOverlay
        measurements={measurements}
        draftStart={draftStart}
        hoverPoint={hoverPoint}
        imageSize={imageSize}
        measurementScale={measurementScale}
      />
    </div>
  );
}

export function ViewerStage({
  activeMode,
  canCompare,
  originalPreviewUrl,
  processedPreviewUrl,
  originalMeasurementScale,
  processedMeasurementScale,
  recipeName,
  toneLabel,
  paletteLabel,
  dirty,
}: ViewerStageProps) {
  const modeContent = VIEWER_MODE_CONTENT[activeMode];
  const isCompare = activeMode === "compare" && canCompare;
  const primaryImage =
    modeContent.primaryImageKind === "processed" ? processedPreviewUrl : originalPreviewUrl;
  const measurementScaleByImage: Record<MeasurementImageKind, MeasurementScale | null> = {
    original: originalMeasurementScale,
    processed: processedMeasurementScale,
  };

  const nextMeasurementID = useRef(0);
  const [measurementMode, setMeasurementMode] = useState(false);
  const [measurementsByImage, setMeasurementsByImage] =
    useState<Record<MeasurementImageKind, ViewerMeasurement[]>>(emptyMeasurements);
  const [draftPointsByImage, setDraftPointsByImage] =
    useState<Record<MeasurementImageKind, MeasurementPoint | null>>(emptyPoints);
  const [hoverPointsByImage, setHoverPointsByImage] =
    useState<Record<MeasurementImageKind, MeasurementPoint | null>>(emptyPoints);
  const [imageSizesByImage, setImageSizesByImage] =
    useState<Record<MeasurementImageKind, ImageSize | null>>(emptyImageSizes);

  useEffect(() => {
    setMeasurementsByImage((current) => ({ ...current, original: [] }));
    setDraftPointsByImage((current) => ({ ...current, original: null }));
    setHoverPointsByImage((current) => ({ ...current, original: null }));
    setImageSizesByImage((current) => ({ ...current, original: null }));
  }, [originalPreviewUrl]);

  useEffect(() => {
    setMeasurementsByImage((current) => ({ ...current, processed: [] }));
    setDraftPointsByImage((current) => ({ ...current, processed: null }));
    setHoverPointsByImage((current) => ({ ...current, processed: null }));
    setImageSizesByImage((current) => ({ ...current, processed: null }));
  }, [processedPreviewUrl]);

  const totalMeasurements = useMemo(
    () => measurementsByImage.original.length + measurementsByImage.processed.length,
    [measurementsByImage],
  );
  const hasAnyImage = Boolean(originalPreviewUrl || processedPreviewUrl);
  const hasDraftMeasurement = Boolean(draftPointsByImage.original || draftPointsByImage.processed);
  const compareMeasurementSource =
    measurementSourceLabel(measurementScaleByImage.original) ===
    measurementSourceLabel(measurementScaleByImage.processed)
      ? measurementSourceLabel(measurementScaleByImage.original)
      : "each pane's DICOM spacing metadata";

  function handleImageLoad(imageKind: MeasurementImageKind, imageSize: ImageSize) {
    setImageSizesByImage((current) => ({
      ...current,
      [imageKind]: imageSize,
    }));
  }

  function handlePointerDown(
    imageKind: MeasurementImageKind,
    event: ReactPointerEvent<HTMLDivElement>,
  ) {
    event.preventDefault();

    const point = pointFromPointer(event);
    const draftStart = draftPointsByImage[imageKind];
    if (!draftStart) {
      setDraftPointsByImage((current) => ({
        ...current,
        [imageKind]: point,
      }));
      setHoverPointsByImage((current) => ({
        ...current,
        [imageKind]: point,
      }));
      return;
    }

    const measurement: ViewerMeasurement = {
      id: `measurement-${nextMeasurementID.current}`,
      start: draftStart,
      end: point,
    };
    nextMeasurementID.current += 1;

    const minimumDistance = imageSizesByImage[imageKind]
      ? measurementDistancePx(measurement, imageSizesByImage[imageKind])
      : Math.hypot(point.x - draftStart.x, point.y - draftStart.y) * 100;

    setDraftPointsByImage((current) => ({ ...current, [imageKind]: null }));
    setHoverPointsByImage((current) => ({ ...current, [imageKind]: null }));

    if (minimumDistance < 8) {
      return;
    }

    setMeasurementsByImage((current) => ({
      ...current,
      [imageKind]: [...current[imageKind], measurement],
    }));
  }

  function handlePointerMove(
    imageKind: MeasurementImageKind,
    event: ReactPointerEvent<HTMLDivElement>,
  ) {
    if (!draftPointsByImage[imageKind]) {
      return;
    }

    setHoverPointsByImage((current) => ({
      ...current,
      [imageKind]: pointFromPointer(event),
    }));
  }

  function handlePointerLeave(imageKind: MeasurementImageKind) {
    setHoverPointsByImage((current) => ({
      ...current,
      [imageKind]: null,
    }));
  }

  function clearDraftMeasurements() {
    setDraftPointsByImage(emptyPoints());
    setHoverPointsByImage(emptyPoints());
  }

  function clearMeasurements() {
    setMeasurementsByImage(emptyMeasurements());
    clearDraftMeasurements();
  }

  const activeMeasurementScale = measurementScaleByImage[modeContent.primaryImageKind];
  const measurementHint = measurementMode
    ? hasDraftMeasurement
      ? `Click the second point to finish the line. Distances use ${isCompare ? compareMeasurementSource : measurementSourceLabel(activeMeasurementScale)} when available.`
      : `Click any preview twice to place a distance measurement. ${
          isCompare
            ? `Compare mode uses ${compareMeasurementSource} when available.`
            : `Distances use ${measurementSourceLabel(activeMeasurementScale)} when available.`
        }`
    : `Turn on measurement mode to place distance lines on the preview. ${
        isCompare
          ? `Compare mode uses ${compareMeasurementSource} when available.`
          : `Measurements use ${measurementSourceLabel(activeMeasurementScale)} when available.`
      }`;

  return (
    <section className="viewer-shell">
      <div className="viewer-shell__header">
        <div>
          <div className="viewer-shell__eyebrow">EDITOR / {modeContent.editorName}</div>
          <h2 className="viewer-shell__title">{modeContent.editorTitle}</h2>
          <p className="viewer-shell__subtitle">{modeContent.editorDescription}</p>
        </div>

        <div className="viewer-shell__meta">
          <span className="pill">recipe:{recipeName}</span>
          <span className="pill">tone:{toneLabel}</span>
          <span className="pill">palette:{paletteLabel}</span>
          <span className="pill">measurements:{totalMeasurements}</span>
          <span className={`pill ${dirty ? "pill--warning" : "pill--accent"}`}>
            {dirty ? "refresh-needed" : "output-synced"}
          </span>
        </div>
      </div>

      <div className="viewer-shell__toolbar">
        <div className="viewer-shell__tools">
          <button
            className={`button ${measurementMode ? "button--primary" : "button--ghost"}`}
            type="button"
            onClick={() => setMeasurementMode((current) => !current)}
            disabled={!hasAnyImage}
          >
            {measurementMode ? "Measurements On" : "Add Measurements"}
          </button>
          <button
            className="button button--ghost"
            type="button"
            onClick={clearDraftMeasurements}
            disabled={!hasDraftMeasurement}
          >
            Cancel Draft
          </button>
          <button
            className="button button--ghost"
            type="button"
            onClick={clearMeasurements}
            disabled={totalMeasurements === 0 && !hasDraftMeasurement}
          >
            Clear Measurements
          </button>
        </div>
        <p className="viewer-shell__hint">{measurementHint}</p>
      </div>

      <div className="viewer-stage">
        {isCompare ? (
          <div className="compare-grid">
            <div className="compare-grid__slot">
              <div className="compare-grid__label">
                {comparePaneLabel("Original", measurementsByImage.original.length)}
              </div>
              <StageImage
                src={originalPreviewUrl}
                alt="Original DICOM preview"
                imageKind="original"
                measurementMode={measurementMode}
                measurements={measurementsByImage.original}
                draftStart={draftPointsByImage.original}
                hoverPoint={hoverPointsByImage.original}
                imageSize={imageSizesByImage.original}
                measurementScale={measurementScaleByImage.original}
                onImageLoad={handleImageLoad}
                onPointerDown={handlePointerDown}
                onPointerMove={handlePointerMove}
                onPointerLeave={handlePointerLeave}
              />
            </div>
            <div className="compare-grid__slot">
              <div className="compare-grid__label">
                {comparePaneLabel("Processed", measurementsByImage.processed.length)}
              </div>
              <StageImage
                src={processedPreviewUrl}
                alt="Processed DICOM preview"
                imageKind="processed"
                measurementMode={measurementMode}
                measurements={measurementsByImage.processed}
                draftStart={draftPointsByImage.processed}
                hoverPoint={hoverPointsByImage.processed}
                imageSize={imageSizesByImage.processed}
                measurementScale={measurementScaleByImage.processed}
                onImageLoad={handleImageLoad}
                onPointerDown={handlePointerDown}
                onPointerMove={handlePointerMove}
                onPointerLeave={handlePointerLeave}
              />
            </div>
          </div>
        ) : (
          <StageImage
            src={primaryImage}
            alt={modeContent.primaryImageAlt}
            imageKind={modeContent.primaryImageKind}
            measurementMode={measurementMode}
            measurements={measurementsByImage[modeContent.primaryImageKind]}
            draftStart={draftPointsByImage[modeContent.primaryImageKind]}
            hoverPoint={hoverPointsByImage[modeContent.primaryImageKind]}
            imageSize={imageSizesByImage[modeContent.primaryImageKind]}
            measurementScale={measurementScaleByImage[modeContent.primaryImageKind]}
            onImageLoad={handleImageLoad}
            onPointerDown={handlePointerDown}
            onPointerMove={handlePointerMove}
            onPointerLeave={handlePointerLeave}
          />
        )}
      </div>
    </section>
  );
}
