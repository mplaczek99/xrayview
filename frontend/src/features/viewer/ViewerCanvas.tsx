import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type {
  PointerEvent as ReactPointerEvent,
  WheelEvent as ReactWheelEvent,
} from "react";
import type {
  AnnotationBundle,
  AnnotationPoint,
  LineAnnotation,
} from "../../lib/generated/contracts";
import { AnnotationLayer } from "../annotations/AnnotationLayer";
import {
  createManualLineAnnotation,
  getLineAnnotation,
  type ViewerTool,
} from "../annotations/tools";
import {
  clampPointToImage,
  createViewport,
  getViewerTransform,
  screenToImage,
  zoomAtPoint,
  type ViewerFrame,
  type ViewerImageSize,
} from "./viewport";

interface ViewerCanvasProps {
  previewUrl: string | null;
  imageSize: ViewerImageSize | null;
  annotations: AnnotationBundle;
  selectedAnnotationId: string | null;
  tool: ViewerTool;
  emptyTitle?: string;
  emptyDescription?: string;
  onSelectAnnotation: (annotationId: string | null) => void;
  onCreateLine: (annotation: LineAnnotation) => void | Promise<void>;
  onUpdateLine: (annotation: LineAnnotation) => void | Promise<void>;
}

type ViewerInteraction =
  | {
      kind: "pan";
      pointerStart: AnnotationPoint;
      panStart: Pick<ReturnType<typeof createViewport>, "panX" | "panY">;
    }
  | { kind: "draw" }
  | {
      kind: "edit";
      annotationId: string;
      endpoint: "start" | "end";
    };

function pointDistance(left: AnnotationPoint, right: AnnotationPoint): number {
  return Math.hypot(left.x - right.x, left.y - right.y);
}

export function ViewerCanvas({
  previewUrl,
  imageSize,
  annotations,
  selectedAnnotationId,
  tool,
  emptyTitle = "No image loaded",
  emptyDescription = "Open a DICOM file to view it here.",
  onSelectAnnotation,
  onCreateLine,
  onUpdateLine,
}: ViewerCanvasProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [frame, setFrame] = useState<ViewerFrame>({ width: 0, height: 0 });
  const [viewport, setViewport] = useState(createViewport);
  const [loadFailed, setLoadFailed] = useState(false);
  const [imageReady, setImageReady] = useState(false);
  const [resolvedImageSize, setResolvedImageSize] = useState<ViewerImageSize | null>(
    imageSize,
  );
  const [interaction, setInteraction] = useState<ViewerInteraction | null>(null);
  const [draftLine, setDraftLine] = useState<LineAnnotation | null>(null);

  useEffect(() => {
    setResolvedImageSize(imageSize);
  }, [imageSize]);

  useLayoutEffect(() => {
    const element = containerRef.current;
    if (!element) {
      return;
    }

    const updateFrame = () => {
      const rect = element.getBoundingClientRect();
      setFrame({
        width: rect.width,
        height: rect.height,
      });
    };

    updateFrame();

    const observer = new ResizeObserver(() => {
      updateFrame();
    });
    observer.observe(element);

    window.addEventListener("resize", updateFrame);
    return () => {
      observer.disconnect();
      window.removeEventListener("resize", updateFrame);
    };
  }, []);

  useEffect(() => {
    setLoadFailed(false);
    setImageReady(false);
    setInteraction(null);
    setDraftLine(null);
    setViewport(createViewport());
    if (!previewUrl && !imageSize) {
      setResolvedImageSize(null);
    }
  }, [previewUrl]);

  const transform = useMemo(() => {
    if (!resolvedImageSize || frame.width === 0 || frame.height === 0) {
      return null;
    }

    return getViewerTransform(frame, resolvedImageSize, viewport);
  }, [frame, resolvedImageSize, viewport]);

  useEffect(() => {
    const activeInteraction = interaction;
    const activeTransform = transform;
    const activeImageSize = resolvedImageSize;
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
      const nextDraft = draftLine;
      const nextInteraction = stableInteraction;
      setInteraction(null);
      setDraftLine(null);

      if (!nextDraft) {
        return;
      }

      if (
        pointDistance(nextDraft.start, nextDraft.end) < 2 ||
        !stableImageSize
      ) {
        return;
      }

      if (nextInteraction.kind === "draw") {
        void onCreateLine(nextDraft);
        onSelectAnnotation(nextDraft.id);
        return;
      }

      void onUpdateLine(nextDraft);
      onSelectAnnotation(nextDraft.id);
    }

    window.addEventListener("pointermove", handlePointerMove);
    window.addEventListener("pointerup", handlePointerUp);

    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
    };
  }, [
    draftLine,
    interaction,
    onCreateLine,
    onSelectAnnotation,
    onUpdateLine,
    resolvedImageSize,
    transform,
  ]);

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

  function pointerToLocalPoint(
    event: ReactPointerEvent<HTMLDivElement> | ReactWheelEvent<HTMLDivElement>,
  ): AnnotationPoint {
    const rect = event.currentTarget.getBoundingClientRect();
    return {
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
    };
  }

  function beginBackgroundInteraction(
    event: ReactPointerEvent<HTMLDivElement>,
  ) {
    if (!transform || !resolvedImageSize || !imageReady || event.button !== 0) {
      return;
    }

    const pointer = pointerToLocalPoint(event);
    if (tool === "measureLine") {
      const imagePoint = clampPointToImage(
        screenToImage(pointer, transform),
        resolvedImageSize,
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
    onSelectAnnotation(null);
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

  if (!previewUrl || loadFailed) {
    return (
      <div className="viewer-stage viewer-stage--interactive">
        <div className="viewer-placeholder">
          <div className="viewer-placeholder__title">
            {loadFailed ? "Preview Unavailable" : emptyTitle}
          </div>
          <p className="viewer-placeholder__copy">
            {loadFailed
              ? "The rendered preview file could not be loaded by the desktop webview."
              : emptyDescription}
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="viewer-stage viewer-stage--interactive">
      <div className="viewer-stage__hud">
        <span className="viewer-stage__hud-chip">
          {Math.round(viewport.zoom * 100)}%
        </span>
        <button
          className="button button--ghost viewer-stage__hud-button"
          type="button"
          onClick={() => setViewport(createViewport())}
        >
          Reset view
        </button>
      </div>

      <div
        ref={containerRef}
        className={`viewer-canvas viewer-canvas--${tool}`}
        onPointerDown={beginBackgroundInteraction}
        onWheel={(event) => {
          if (!resolvedImageSize || !imageReady) {
            return;
          }

          event.preventDefault();
          const pointer = pointerToLocalPoint(event);
          const factor = event.deltaY < 0 ? 1.12 : 0.9;
          setViewport((current) =>
            zoomAtPoint(current, frame, resolvedImageSize, pointer, factor),
          );
        }}
      >
        <img
          className={`viewer-canvas__image${imageReady ? " viewer-canvas__image--ready" : ""}`}
          src={previewUrl}
          alt="DICOM preview"
          draggable={false}
          onLoad={(event) => {
            setImageReady(true);
            setLoadFailed(false);
            if (!imageSize) {
              setResolvedImageSize({
                width: event.currentTarget.naturalWidth,
                height: event.currentTarget.naturalHeight,
              });
            }
          }}
          onError={() => {
            setImageReady(false);
            setLoadFailed(true);
          }}
          style={
            transform && resolvedImageSize
              ? {
                  left: `${transform.offsetX}px`,
                  top: `${transform.offsetY}px`,
                  width: `${resolvedImageSize.width * transform.scale}px`,
                  height: `${resolvedImageSize.height * transform.scale}px`,
                }
              : undefined
          }
        />
        {transform && imageReady ? (
          <AnnotationLayer
            width={frame.width}
            height={frame.height}
            transform={transform}
            annotations={displayedAnnotations}
            selectedAnnotationId={selectedAnnotationId}
            draftLine={interaction?.kind === "draw" ? draftLine : null}
            onSelectAnnotation={onSelectAnnotation}
            onStartHandleDrag={beginHandleDrag}
          />
        ) : null}
      </div>
    </div>
  );
}
