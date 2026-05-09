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
  const imageSizeRef = useRef(imageSize);
  const interactionRef = useRef<ViewerInteraction | null>(interaction);
  const transformRef = useRef(transform);
  draftLineRef.current = draftLine;
  imageSizeRef.current = imageSize;
  interactionRef.current = interaction;
  transformRef.current = transform;

  const callbacksRef = useRef({ onCreateLine, onSelectAnnotation, onUpdateLine });
  useEffect(() => {
    callbacksRef.current = { onCreateLine, onSelectAnnotation, onUpdateLine };
  }, [onCreateLine, onSelectAnnotation, onUpdateLine]);

  useEffect(() => {
    function handlePointerMove(event: PointerEvent) {
      const activeInteraction = interactionRef.current;
      const activeTransform = transformRef.current;
      const activeImageSize = imageSizeRef.current;
      if (!activeInteraction || !activeTransform || !activeImageSize) {
        return;
      }

      const rect = containerRef.current?.getBoundingClientRect();
      if (!rect) {
        return;
      }

      const pointer = {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      };

      if (activeInteraction.kind === "pan") {
        setViewport((current) => ({
          ...current,
          panX:
            activeInteraction.panStart.panX +
            (pointer.x - activeInteraction.pointerStart.x),
          panY:
            activeInteraction.panStart.panY +
            (pointer.y - activeInteraction.pointerStart.y),
        }));
        return;
      }

      const imagePoint = clampPointToImage(
        screenToImage(pointer, activeTransform),
        activeImageSize,
      );
      if (activeInteraction.kind === "draw") {
        setDraftLine((current) => {
          const next = current
            ? {
                ...current,
                end: imagePoint,
              }
            : current;
          draftLineRef.current = next;
          return next;
        });
        return;
      }

      setDraftLine((current) => {
        const next = current
          ? {
              ...current,
              [activeInteraction.endpoint]: imagePoint,
            }
          : current;
        draftLineRef.current = next;
        return next;
      });
    }

    function handlePointerUp() {
      const nextInteraction = interactionRef.current;
      if (!nextInteraction) {
        return;
      }

      const nextDraft = draftLineRef.current;
      interactionRef.current = null;
      draftLineRef.current = null;
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
  }, [containerRef, setViewport]);

  const displayedAnnotations = annotations;
  const draftLineOverride = interaction?.kind === "edit" ? draftLine : null;

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
      const nextInteraction = { kind: "draw" } satisfies ViewerInteraction;
      draftLineRef.current = annotation;
      interactionRef.current = nextInteraction;
      setDraftLine(annotation);
      setInteraction(nextInteraction);
      onSelectAnnotation(null);
      return;
    }

    const nextInteraction = {
      kind: "pan",
      pointerStart: pointer,
      panStart: {
        panX: viewport.panX,
        panY: viewport.panY,
      },
    } satisfies ViewerInteraction;
    interactionRef.current = nextInteraction;
    setInteraction(nextInteraction);
  }

  function beginHandleDrag(annotationId: string, endpoint: "start" | "end") {
    const annotation = getLineAnnotation(annotations, annotationId);
    if (!annotation || !annotation.editable) {
      return;
    }

    const nextInteraction = {
      kind: "edit",
      annotationId,
      endpoint,
    } satisfies ViewerInteraction;
    draftLineRef.current = annotation;
    interactionRef.current = nextInteraction;
    setDraftLine(annotation);
    setInteraction(nextInteraction);
    onSelectAnnotation(annotationId);
  }

  function resetPointerInteractions() {
    interactionRef.current = null;
    draftLineRef.current = null;
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
    draftLineOverride,
    handleMouseLeave,
    handleMouseMove,
    hoverCoord,
    isDrawingLine: interaction?.kind === "draw",
    resetPointerInteractions,
  };
}
