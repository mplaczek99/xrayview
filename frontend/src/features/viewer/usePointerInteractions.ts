import { useEffect, useMemo, useRef, useState } from "react";
import type {
  Dispatch,
  MouseEvent as ReactMouseEvent,
  PointerEvent as ReactPointerEvent,
  RefObject,
  SetStateAction,
} from "react";
import type {
  AnnotationBundle,
  AnnotationPoint,
  LineAnnotation,
} from "../../lib/generated/contracts";
import {
  createManualLineAnnotation,
  getLineAnnotation,
  type ViewerTool,
} from "../annotations/tools";
import {
  clampPointToImage,
  screenToImage,
  type ViewerImageSize,
  type ViewerTransform,
  type ViewerViewport,
} from "./viewport";

type ViewerInteraction =
  | {
      kind: "pan";
      pointerStart: AnnotationPoint;
      panStart: Pick<ViewerViewport, "panX" | "panY">;
    }
  | { kind: "draw" }
  | {
      kind: "edit";
      annotationId: string;
      endpoint: "start" | "end";
    };

interface UsePointerInteractionsOptions {
  containerRef: RefObject<HTMLDivElement>;
  annotations: AnnotationBundle;
  imageReady: boolean;
  imageSize: ViewerImageSize | null;
  tool: ViewerTool;
  transform: ViewerTransform | null;
  viewport: ViewerViewport;
  setViewport: Dispatch<SetStateAction<ViewerViewport>>;
  onSelectAnnotation: (annotationId: string | null) => void;
  onCreateLine: (annotation: LineAnnotation) => void | Promise<void>;
  onUpdateLine: (annotation: LineAnnotation) => void | Promise<void>;
}

function pointDistance(left: AnnotationPoint, right: AnnotationPoint): number {
  return Math.hypot(left.x - right.x, left.y - right.y);
}

function pointerToLocalPoint(
  event: ReactPointerEvent<HTMLDivElement>,
): AnnotationPoint {
  const rect = event.currentTarget.getBoundingClientRect();
  return {
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  };
}

export function usePointerInteractions({
  containerRef,
  annotations,
  imageReady,
  imageSize,
  tool,
  transform,
  viewport,
  setViewport,
  onSelectAnnotation,
  onCreateLine,
  onUpdateLine,
}: UsePointerInteractionsOptions) {
  const [interaction, setInteraction] = useState<ViewerInteraction | null>(null);
  const [draftLine, setDraftLine] = useState<LineAnnotation | null>(null);
  const [hoverCoord, setHoverCoord] = useState<{ x: number; y: number } | null>(null);

  const draftLineRef = useRef(draftLine);
  useEffect(() => {
    draftLineRef.current = draftLine;
  }, [draftLine]);

  const callbacksRef = useRef({ onCreateLine, onSelectAnnotation, onUpdateLine });
  useEffect(() => {
    callbacksRef.current = { onCreateLine, onSelectAnnotation, onUpdateLine };
  }, [onCreateLine, onSelectAnnotation, onUpdateLine]);

  useEffect(() => {
    const activeInteraction = interaction;
    const activeTransform = transform;
    const activeImageSize = imageSize;
    if (!activeInteraction || !activeTransform || !activeImageSize) {
      return;
    }

    const stableInteraction = activeInteraction;
    const stableTransform = activeTransform;
    const stableImageSize = activeImageSize;

    function handlePointerMove(event: PointerEvent) {
      const rect = containerRef.current?.getBoundingClientRect();
      if (!rect) {
        return;
      }

      const pointer = {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      };

      if (stableInteraction.kind === "pan") {
        setViewport((current) => ({
          ...current,
          panX:
            stableInteraction.panStart.panX +
            (pointer.x - stableInteraction.pointerStart.x),
          panY:
            stableInteraction.panStart.panY +
            (pointer.y - stableInteraction.pointerStart.y),
        }));
        return;
      }

      const imagePoint = clampPointToImage(
        screenToImage(pointer, stableTransform),
        stableImageSize,
      );
      if (stableInteraction.kind === "draw") {
        setDraftLine((current) =>
          current
            ? {
                ...current,
                end: imagePoint,
              }
            : current,
        );
        return;
      }

      setDraftLine((current) =>
        current
          ? {
              ...current,
              [stableInteraction.endpoint]: imagePoint,
            }
          : current,
      );
    }

    function handlePointerUp() {
      const nextDraft = draftLineRef.current;
      const nextInteraction = stableInteraction;
      setInteraction(null);
      setDraftLine(null);

      if (!nextDraft || pointDistance(nextDraft.start, nextDraft.end) < 2) {
        return;
      }

      const cbs = callbacksRef.current;
      if (nextInteraction.kind === "draw") {
        void cbs.onCreateLine(nextDraft);
        cbs.onSelectAnnotation(nextDraft.id);
        return;
      }

      void cbs.onUpdateLine(nextDraft);
      cbs.onSelectAnnotation(nextDraft.id);
    }

    const container = containerRef.current;
    if (!container) {
      return;
    }

    container.addEventListener("pointermove", handlePointerMove);
    container.addEventListener("pointerup", handlePointerUp);

    return () => {
      container.removeEventListener("pointermove", handlePointerMove);
      container.removeEventListener("pointerup", handlePointerUp);
    };
  }, [containerRef, interaction, imageSize, transform, setViewport]);

  const displayedAnnotations = useMemo(() => {
    if (interaction?.kind !== "edit" || !draftLine) {
      return annotations;
    }

    return {
      ...annotations,
      lines: annotations.lines.map((annotation) =>
        annotation.id === draftLine.id ? draftLine : annotation,
      ),
    };
  }, [annotations, draftLine, interaction]);

  const draftDistance = useMemo(() => {
    if (!draftLine) {
      return null;
    }

    return pointDistance(draftLine.start, draftLine.end);
  }, [draftLine]);

  function handleMouseMove(event: ReactMouseEvent<HTMLDivElement>) {
    if (!transform || !imageSize || !imageReady) {
      setHoverCoord(null);
      return;
    }

    const rect = event.currentTarget.getBoundingClientRect();
    const pointer = {
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
    };
    const imagePoint = screenToImage(pointer, transform);
    if (
      imagePoint.x < 0 ||
      imagePoint.y < 0 ||
      imagePoint.x > imageSize.width ||
      imagePoint.y > imageSize.height
    ) {
      setHoverCoord(null);
      return;
    }

    setHoverCoord({ x: Math.round(imagePoint.x), y: Math.round(imagePoint.y) });
  }

  function handleMouseLeave() {
    setHoverCoord(null);
  }

  function beginBackgroundInteraction(
    event: ReactPointerEvent<HTMLDivElement>,
  ) {
    if (!transform || !imageSize || !imageReady || event.button !== 0) {
      return;
    }

    event.currentTarget.setPointerCapture(event.pointerId);
    const pointer = pointerToLocalPoint(event);
    if (tool === "measureLine") {
      const imagePoint = clampPointToImage(
        screenToImage(pointer, transform),
        imageSize,
      );
      const annotation = createManualLineAnnotation(imagePoint, imagePoint);
      setDraftLine(annotation);
      setInteraction({ kind: "draw" });
      onSelectAnnotation(null);
      return;
    }

    setInteraction({
      kind: "pan",
      pointerStart: pointer,
      panStart: {
        panX: viewport.panX,
        panY: viewport.panY,
      },
    });
  }

  function beginHandleDrag(annotationId: string, endpoint: "start" | "end") {
    const annotation = getLineAnnotation(annotations, annotationId);
    if (!annotation || !annotation.editable) {
      return;
    }

    setDraftLine(annotation);
    setInteraction({
      kind: "edit",
      annotationId,
      endpoint,
    });
    onSelectAnnotation(annotationId);
  }

  function resetPointerInteractions() {
    setInteraction(null);
    setDraftLine(null);
    setHoverCoord(null);
  }

  return {
    beginBackgroundInteraction,
    beginHandleDrag,
    displayedAnnotations,
    draftDistance,
    draftLine,
    handleMouseLeave,
    handleMouseMove,
    hoverCoord,
    isDrawingLine: interaction?.kind === "draw",
    resetPointerInteractions,
  };
}
