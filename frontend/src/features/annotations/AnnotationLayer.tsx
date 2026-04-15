import { memo, useMemo } from "react";
import type {
  AnnotationBundle,
  LineAnnotation,
} from "../../lib/generated/contracts";
import type { ViewerTransform } from "../viewer/viewport";

interface AnnotationLayerProps {
  width: number;
  height: number;
  transform: ViewerTransform;
  annotations: AnnotationBundle;
  selectedAnnotationId: string | null;
  draftLine: LineAnnotation | null;
  onSelectAnnotation: (annotationId: string) => void;
  onStartHandleDrag: (
    annotationId: string,
    endpoint: "start" | "end",
  ) => void;
}

interface LineAnnotationItemProps {
  annotation: LineAnnotation;
  isSelected: boolean;
  scale: number;
  onSelectAnnotation: (annotationId: string) => void;
}

function midpoint(line: LineAnnotation) {
  return {
    x: (line.start.x + line.end.x) / 2,
    y: (line.start.y + line.end.y) / 2,
  };
}

function lineItemPropsEqual(
  prev: LineAnnotationItemProps,
  next: LineAnnotationItemProps,
): boolean {
  return (
    Object.is(prev.annotation, next.annotation) &&
    prev.isSelected === next.isSelected &&
    prev.scale === next.scale
    // callbacks intentionally excluded — recreated each render but functionally stable
  );
}

const LineAnnotationItem = memo(
  function LineAnnotationItem({
    annotation,
    isSelected,
    scale,
    onSelectAnnotation,
  }: LineAnnotationItemProps) {
    const mid = midpoint(annotation);
    const labelOffset = 10 / Math.max(scale, 1);
    const label = annotation.measurement
      ? `${annotation.label} · ${
          annotation.measurement.calibratedLengthMm !== null &&
          annotation.measurement.calibratedLengthMm !== undefined
            ? `${annotation.measurement.calibratedLengthMm.toFixed(1)} mm`
            : `${annotation.measurement.pixelLength.toFixed(1)} px`
        }`
      : annotation.label;

    return (
      <g>
        <line
          className={`annotation-layer__line annotation-layer__line--${annotation.source}${
            isSelected ? " annotation-layer__line--selected" : ""
          }`}
          x1={annotation.start.x}
          y1={annotation.start.y}
          x2={annotation.end.x}
          y2={annotation.end.y}
          vectorEffect="non-scaling-stroke"
          onPointerDown={(event) => {
            event.stopPropagation();
            onSelectAnnotation(annotation.id);
          }}
        />
        <text
          className="annotation-layer__label"
          x={mid.x}
          y={mid.y - labelOffset}
          textAnchor="middle"
          pointerEvents="none"
        >
          {label}
        </text>
      </g>
    );
  },
  lineItemPropsEqual,
);

function annotationLayerPropsEqual(
  prev: AnnotationLayerProps,
  next: AnnotationLayerProps,
): boolean {
  return (
    prev.width === next.width &&
    prev.height === next.height &&
    prev.transform.offsetX === next.transform.offsetX &&
    prev.transform.offsetY === next.transform.offsetY &&
    prev.transform.scale === next.transform.scale &&
    Object.is(prev.annotations, next.annotations) &&
    prev.selectedAnnotationId === next.selectedAnnotationId &&
    Object.is(prev.draftLine, next.draftLine)
    // callbacks intentionally excluded — ViewerCanvas recreates them each render
  );
}

export const AnnotationLayer = memo(
  function AnnotationLayer({
    width,
    height,
    transform,
    annotations,
    selectedAnnotationId,
    draftLine,
    onSelectAnnotation,
    onStartHandleDrag,
  }: AnnotationLayerProps) {
    const selectedLine = useMemo(
      () =>
        annotations.lines.find((a) => a.id === selectedAnnotationId) ?? null,
      [annotations.lines, selectedAnnotationId],
    );
    const handleRadius = 7 / Math.max(transform.scale, 1);

    return (
      <svg
        className="annotation-layer"
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        aria-hidden="true"
      >
        <g
          transform={`translate(${transform.offsetX} ${transform.offsetY}) scale(${transform.scale})`}
        >
          {annotations.rectangles.map((annotation) => (
            <rect
              key={annotation.id}
              className="annotation-layer__rect"
              x={annotation.x}
              y={annotation.y}
              width={annotation.width}
              height={annotation.height}
              vectorEffect="non-scaling-stroke"
            />
          ))}

          {annotations.lines.map((annotation) => (
            <LineAnnotationItem
              key={annotation.id}
              annotation={annotation}
              isSelected={annotation.id === selectedAnnotationId}
              scale={transform.scale}
              onSelectAnnotation={onSelectAnnotation}
            />
          ))}

          {draftLine ? (
            <line
              className="annotation-layer__line annotation-layer__line--draft"
              x1={draftLine.start.x}
              y1={draftLine.start.y}
              x2={draftLine.end.x}
              y2={draftLine.end.y}
              vectorEffect="non-scaling-stroke"
            />
          ) : null}

          {selectedLine ? (
            <>
              <circle
                className="annotation-layer__handle"
                cx={selectedLine.start.x}
                cy={selectedLine.start.y}
                r={handleRadius}
                vectorEffect="non-scaling-stroke"
                onPointerDown={(event) => {
                  event.stopPropagation();
                  onStartHandleDrag(selectedLine.id, "start");
                }}
              />
              <circle
                className="annotation-layer__handle"
                cx={selectedLine.end.x}
                cy={selectedLine.end.y}
                r={handleRadius}
                vectorEffect="non-scaling-stroke"
                onPointerDown={(event) => {
                  event.stopPropagation();
                  onStartHandleDrag(selectedLine.id, "end");
                }}
              />
            </>
          ) : null}
        </g>
      </svg>
    );
  },
  annotationLayerPropsEqual,
);
