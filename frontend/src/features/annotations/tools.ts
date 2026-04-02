import type {
  AnnotationBundle,
  AnnotationPoint,
  LineAnnotation,
  LineMeasurement,
} from "../../lib/generated/contracts";

export type ViewerTool = "pan" | "measureLine";

let manualLineSequence = 0;

export function emptyAnnotationBundle(): AnnotationBundle {
  return {
    lines: [],
    rectangles: [],
  };
}

export function createManualLineAnnotation(
  start: AnnotationPoint,
  end: AnnotationPoint,
): LineAnnotation {
  manualLineSequence += 1;

  return {
    id: `manual-line-${manualLineSequence}`,
    label: `Measurement ${manualLineSequence}`,
    source: "manual",
    start,
    end,
    editable: true,
    confidence: null,
    measurement: null,
  };
}

export function replaceSuggestedAnnotations(
  current: AnnotationBundle,
  suggested: AnnotationBundle,
): AnnotationBundle {
  return {
    lines: [
      ...current.lines.filter((annotation) => annotation.source === "manual"),
      ...suggested.lines,
    ],
    rectangles: [
      ...current.rectangles.filter((annotation) => annotation.source === "manual"),
      ...suggested.rectangles,
    ],
  };
}

export function upsertLineAnnotation(
  current: AnnotationBundle,
  line: LineAnnotation,
): AnnotationBundle {
  return {
    ...current,
    lines: [
      line,
      ...current.lines.filter((annotation) => annotation.id !== line.id),
    ],
  };
}

export function removeAnnotation(
  current: AnnotationBundle,
  annotationId: string,
): AnnotationBundle {
  return {
    lines: current.lines.filter((annotation) => annotation.id !== annotationId),
    rectangles: current.rectangles.filter(
      (annotation) => annotation.id !== annotationId,
    ),
  };
}

export function getLineAnnotation(
  current: AnnotationBundle,
  annotationId: string | null,
): LineAnnotation | null {
  if (!annotationId) {
    return null;
  }

  return (
    current.lines.find((annotation) => annotation.id === annotationId) ?? null
  );
}

export function formatLineMeasurement(
  measurement: LineMeasurement | null | undefined,
): string {
  if (!measurement) {
    return "Pending";
  }

  if (measurement.calibratedLengthMm !== null && measurement.calibratedLengthMm !== undefined) {
    return `${measurement.calibratedLengthMm.toFixed(1)} mm`;
  }

  return `${measurement.pixelLength.toFixed(1)} px`;
}

export function formatSecondaryMeasurement(
  measurement: LineMeasurement | null | undefined,
): string | null {
  if (!measurement) {
    return null;
  }

  if (measurement.calibratedLengthMm !== null && measurement.calibratedLengthMm !== undefined) {
    return `${measurement.pixelLength.toFixed(1)} px`;
  }

  return null;
}

export function annotationSourceLabel(source: LineAnnotation["source"]): string {
  return source === "manual" ? "Manual" : "Auto tooth";
}
