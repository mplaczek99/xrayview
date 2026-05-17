import { useEffect, useMemo, useRef, useState } from "react";
import type {
  AnnotationBundle,
  LineAnnotation,
} from "../../lib/generated/contracts";
import { AnnotationLayer } from "../annotations/AnnotationLayer";
import { type ViewerTool } from "../annotations/tools";
import {
  createViewport,
  getViewerTransform,
  type ViewerImageSize,
} from "./viewport";
import { usePointerInteractions } from "./usePointerInteractions";
import { useViewportFrame } from "./useViewportFrame";
import { useWheelZoom } from "./useWheelZoom";

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

export function ViewerCanvas({
  previewUrl,
  imageSize,
  annotations,
  selectedAnnotationId,
  tool,
  emptyTitle = "No image loaded",
  emptyDescription = "Open a DICOM study or BMP/TIFF image to view it here.",
  onSelectAnnotation,
  onCreateLine,
  onUpdateLine,
}: ViewerCanvasProps) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const imgRef = useRef<HTMLImageElement | null>(null);
  const [viewport, setViewport] = useState(createViewport);
  const [loadFailed, setLoadFailed] = useState(false);
  const [imageReady, setImageReady] = useState(false);
  const [resolvedImageSize, setResolvedImageSize] = useState<ViewerImageSize | null>(
    imageSize,
  );

  useEffect(() => {
    setResolvedImageSize(imageSize);
  }, [imageSize]);

  const canvasVisible = !!previewUrl && !loadFailed;
  const frame = useViewportFrame(containerRef, canvasVisible);

  useWheelZoom({
    containerRef,
    enabled: canvasVisible,
    frame,
    imageReady,
    imageSize: resolvedImageSize,
    setViewport,
  });

  useEffect(() => {
    setLoadFailed(false);
    setImageReady(false);
    setViewport(createViewport());
    if (!previewUrl && !imageSize) {
      setResolvedImageSize(null);
    }
    // Handle cached images whose onLoad won't fire
    const img = imgRef.current;
    if (previewUrl && img && img.complete && img.naturalWidth > 0) {
      setImageReady(true);
      if (!imageSize) {
        setResolvedImageSize({
          width: img.naturalWidth,
          height: img.naturalHeight,
        });
      }
    }
  }, [previewUrl]);

  const transform = useMemo(() => {
    if (!resolvedImageSize || frame.width === 0 || frame.height === 0) {
      return null;
    }

    return getViewerTransform(frame, resolvedImageSize, viewport);
  }, [frame, resolvedImageSize, viewport]);
  const {
    beginBackgroundInteraction,
    beginHandleDrag,
    displayedAnnotations,
    draftDistance,
    draftLine,
    draftLineOverride,
    isDrawingLine,
    resetPointerInteractions,
  } = usePointerInteractions({
    containerRef,
    enabled: canvasVisible,
    annotations,
    imageReady,
    imageSize: resolvedImageSize,
    tool,
    transform,
    viewport,
    setViewport,
    onSelectAnnotation,
    onCreateLine,
    onUpdateLine,
  });

  useEffect(() => {
    resetPointerInteractions();
  }, [previewUrl]);

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
        {draftDistance !== null && draftDistance >= 2 && (
          <span className="viewer-stage__hud-chip viewer-stage__hud-chip--dist">
            {Math.round(draftDistance)} px
          </span>
        )}
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
      >
        <img
          ref={imgRef}
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
                  width: `${resolvedImageSize.width}px`,
                  height: `${resolvedImageSize.height}px`,
                  transform: `translate(${transform.offsetX}px, ${transform.offsetY}px) scale(${transform.scale})`,
                  transformOrigin: "0 0",
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
            draftLine={isDrawingLine ? draftLine : null}
            draftLineOverride={draftLineOverride}
            onSelectAnnotation={onSelectAnnotation}
            onStartHandleDrag={beginHandleDrag}
          />
        ) : null}
      </div>
    </div>
  );
}
