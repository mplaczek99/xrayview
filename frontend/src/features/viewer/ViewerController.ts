import { escapeHtml, type ViewerRenderModel } from "../../app/htmxView";
import { workbenchActions } from "../../app/store/workbenchStore";
import type {
  AnnotationBundle,
  AnnotationPoint,
  LineAnnotation,
} from "../../lib/generated/contracts";
import { createManualLineAnnotation, getLineAnnotation } from "../annotations/tools";
import {
  clampPointToImage,
  createViewport,
  getViewerTransform,
  screenToImage,
  type ViewerFrame,
  type ViewerImageSize,
  type ViewerTransform,
  type ViewerViewport,
  zoomAtPoint,
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

function pointDistance(left: AnnotationPoint, right: AnnotationPoint): number {
  return Math.hypot(left.x - right.x, left.y - right.y);
}

function lineMidpoint(line: LineAnnotation): AnnotationPoint {
  return {
    x: (line.start.x + line.end.x) / 2,
    y: (line.start.y + line.end.y) / 2,
  };
}

function pointerToLocalPoint(event: MouseEvent, element: HTMLElement): AnnotationPoint {
  const rect = element.getBoundingClientRect();
  return {
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  };
}

function attr(value: unknown): string {
  return escapeHtml(value);
}

function lineLabel(annotation: LineAnnotation): string {
  if (!annotation.measurement) {
    return annotation.label;
  }

  const measurement =
    annotation.measurement.calibratedLengthMm !== null &&
    annotation.measurement.calibratedLengthMm !== undefined
      ? `${annotation.measurement.calibratedLengthMm.toFixed(1)} mm`
      : `${annotation.measurement.pixelLength.toFixed(1)} px`;

  return `${annotation.label} - ${measurement}`;
}

function renderAnnotationLayer(
  frame: ViewerFrame,
  transform: ViewerTransform,
  annotations: AnnotationBundle,
  selectedAnnotationId: string | null,
  draftLine: LineAnnotation | null,
  draftLineOverride: LineAnnotation | null,
): string {
  const selectedBase = annotations.lines.find((line) => line.id === selectedAnnotationId) ?? null;
  const selectedLine =
    selectedBase && draftLineOverride?.id === selectedBase.id ? draftLineOverride : selectedBase;
  const handleRadius = 7 / Math.max(transform.scale, 1);

  return `
    <svg
      class="annotation-layer"
      width="${attr(frame.width)}"
      height="${attr(frame.height)}"
      viewBox="0 0 ${attr(frame.width)} ${attr(frame.height)}"
      aria-hidden="true"
    >
      <g transform="translate(${attr(transform.offsetX)} ${attr(transform.offsetY)}) scale(${attr(transform.scale)})">
        ${annotations.rectangles
          .map(
            (annotation) => `
          <rect
            class="annotation-layer__rect"
            x="${attr(annotation.x)}"
            y="${attr(annotation.y)}"
            width="${attr(annotation.width)}"
            height="${attr(annotation.height)}"
            vector-effect="non-scaling-stroke"
          ></rect>
        `,
          )
          .join("")}

        ${annotations.polylines
          .map((annotation) => {
            const points = annotation.points.map((point) => `${point.x},${point.y}`).join(" ");
            return annotation.closed
              ? `
            <polygon
              class="annotation-layer__polyline annotation-layer__polyline--${attr(annotation.source)}"
              points="${attr(points)}"
              vector-effect="non-scaling-stroke"
            ></polygon>
          `
              : `
            <polyline
              class="annotation-layer__polyline annotation-layer__polyline--${attr(annotation.source)}"
              points="${attr(points)}"
              vector-effect="non-scaling-stroke"
              fill="none"
            ></polyline>
          `;
          })
          .join("")}

        ${annotations.lines
          .map((annotation) => {
            const visible =
              draftLineOverride?.id === annotation.id ? draftLineOverride : annotation;
            const mid = lineMidpoint(visible);
            const labelOffset = 10 / Math.max(transform.scale, 1);

            return `
            <g>
              <line
                class="annotation-layer__line annotation-layer__line--${attr(annotation.source)}${annotation.id === selectedAnnotationId ? " annotation-layer__line--selected" : ""}"
                x1="${attr(visible.start.x)}"
                y1="${attr(visible.start.y)}"
                x2="${attr(visible.end.x)}"
                y2="${attr(visible.end.y)}"
                vector-effect="non-scaling-stroke"
                data-annotation-line
                data-annotation-id="${attr(annotation.id)}"
              ></line>
              <text
                class="annotation-layer__label"
                x="${attr(mid.x)}"
                y="${attr(mid.y - labelOffset)}"
                text-anchor="middle"
                pointer-events="none"
                opacity="1"
              >${escapeHtml(lineLabel(visible))}</text>
            </g>
          `;
          })
          .join("")}

        ${
          draftLine
            ? `
          <line
            class="annotation-layer__line annotation-layer__line--draft"
            x1="${attr(draftLine.start.x)}"
            y1="${attr(draftLine.start.y)}"
            x2="${attr(draftLine.end.x)}"
            y2="${attr(draftLine.end.y)}"
            vector-effect="non-scaling-stroke"
          ></line>
        `
            : ""
        }

        ${
          selectedLine
            ? `
          <circle
            class="annotation-layer__handle"
            cx="${attr(selectedLine.start.x)}"
            cy="${attr(selectedLine.start.y)}"
            r="${attr(handleRadius)}"
            vector-effect="non-scaling-stroke"
            data-annotation-handle
            data-annotation-id="${attr(selectedLine.id)}"
            data-endpoint="start"
          ></circle>
          <circle
            class="annotation-layer__handle"
            cx="${attr(selectedLine.end.x)}"
            cy="${attr(selectedLine.end.y)}"
            r="${attr(handleRadius)}"
            vector-effect="non-scaling-stroke"
            data-annotation-handle
            data-annotation-id="${attr(selectedLine.id)}"
            data-endpoint="end"
          ></circle>
        `
            : ""
        }
      </g>
    </svg>
  `;
}

export class ViewerController {
  private stage: HTMLElement | null = null;
  private canvas: HTMLElement | null = null;
  private image: HTMLImageElement | null = null;
  private annotationHost: HTMLElement | null = null;
  private draftDistanceEl: HTMLElement | null = null;
  private zoomEl: HTMLElement | null = null;
  private resizeObserver: ResizeObserver | null = null;
  private model: ViewerRenderModel | null = null;
  private viewport: ViewerViewport = createViewport();
  private frame: ViewerFrame = { width: 0, height: 0 };
  private resolvedImageSize: ViewerImageSize | null = null;
  private imageReady = false;
  private loadFailed = false;
  private previewUrl: string | null = null;
  private resetKey: string | null = null;
  private interaction: ViewerInteraction | null = null;
  private draftLine: LineAnnotation | null = null;

  mount(root: ParentNode, model: ViewerRenderModel) {
    this.detach();
    this.model = model;

    const stage = root.querySelector<HTMLElement>("[data-viewer-stage]");
    if (!stage || !model.previewUrl) {
      this.stage = null;
      this.canvas = null;
      this.image = null;
      this.annotationHost = null;
      return;
    }

    const previewChanged = this.previewUrl !== model.previewUrl;
    const resetChanged = this.resetKey !== model.viewportResetKey;
    if (previewChanged || resetChanged) {
      this.viewport = createViewport();
      this.interaction = null;
      this.draftLine = null;
      this.imageReady = false;
      this.loadFailed = false;
    }

    this.previewUrl = model.previewUrl;
    this.resetKey = model.viewportResetKey;
    this.resolvedImageSize = model.imageSize;
    this.stage = stage;
    this.canvas = stage.querySelector<HTMLElement>("[data-viewer-canvas]");
    this.image = stage.querySelector<HTMLImageElement>("[data-viewer-image]");
    this.annotationHost = stage.querySelector<HTMLElement>("[data-annotation-layer]");
    this.draftDistanceEl = stage.querySelector<HTMLElement>("[data-viewer-draft-distance]");
    this.zoomEl = stage.querySelector<HTMLElement>("[data-viewer-zoom]");

    if (!this.canvas || !this.image || !this.annotationHost) {
      return;
    }

    this.canvas.addEventListener("pointerdown", this.handlePointerDown);
    this.canvas.addEventListener("pointermove", this.handlePointerMove);
    this.canvas.addEventListener("pointerup", this.handlePointerUp);
    this.canvas.addEventListener("pointercancel", this.handlePointerUp);
    this.canvas.addEventListener("wheel", this.handleWheel, { passive: false });
    this.image.addEventListener("load", this.handleImageLoad);
    this.image.addEventListener("error", this.handleImageError);

    this.resizeObserver = new ResizeObserver(() => {
      this.updateFrame();
      this.updateCanvas();
    });
    this.resizeObserver.observe(this.canvas);

    if (this.image.complete && this.image.naturalWidth > 0) {
      this.imageReady = true;
      this.loadFailed = false;
      if (!model.imageSize) {
        this.resolvedImageSize = {
          width: this.image.naturalWidth,
          height: this.image.naturalHeight,
        };
      }
    }

    this.updateFrame();
    this.updateCanvas();
  }

  detach() {
    if (this.canvas) {
      this.canvas.removeEventListener("pointerdown", this.handlePointerDown);
      this.canvas.removeEventListener("pointermove", this.handlePointerMove);
      this.canvas.removeEventListener("pointerup", this.handlePointerUp);
      this.canvas.removeEventListener("pointercancel", this.handlePointerUp);
      this.canvas.removeEventListener("wheel", this.handleWheel);
    }
    if (this.image) {
      this.image.removeEventListener("load", this.handleImageLoad);
      this.image.removeEventListener("error", this.handleImageError);
    }
    this.resizeObserver?.disconnect();
    this.resizeObserver = null;
    this.stage = null;
    this.canvas = null;
    this.image = null;
    this.annotationHost = null;
    this.draftDistanceEl = null;
    this.zoomEl = null;
  }

  resetViewport() {
    this.viewport = createViewport();
    this.interaction = null;
    this.draftLine = null;
    this.updateCanvas();
  }

  private handleImageLoad = () => {
    this.imageReady = true;
    this.loadFailed = false;
    if (this.image && !this.model?.imageSize) {
      this.resolvedImageSize = {
        width: this.image.naturalWidth,
        height: this.image.naturalHeight,
      };
    }
    this.updateCanvas();
  };

  private handleImageError = () => {
    this.imageReady = false;
    this.loadFailed = true;
    if (this.stage) {
      this.stage.innerHTML = `
        <div class="viewer-placeholder">
          <div class="viewer-placeholder__title">Preview Unavailable</div>
          <p class="viewer-placeholder__copy">
            The rendered preview file could not be loaded by the desktop webview.
          </p>
        </div>
      `;
    }
  };

  private handleWheel = (event: WheelEvent) => {
    if (!this.canvas || !this.resolvedImageSize || !this.imageReady) {
      return;
    }

    event.preventDefault();
    const pointer = pointerToLocalPoint(event, this.canvas);
    const factor = event.deltaY < 0 ? 1.12 : 0.9;
    this.viewport = zoomAtPoint(this.viewport, this.frame, this.resolvedImageSize, pointer, factor);
    this.updateCanvas();
  };

  private handlePointerDown = (event: PointerEvent) => {
    if (!this.canvas || !this.model || event.button !== 0) {
      return;
    }

    const target = event.target instanceof Element ? event.target : null;
    const handle = target?.closest<SVGElement>("[data-annotation-handle]");
    if (handle) {
      const annotationId = handle.dataset.annotationId;
      const endpoint = handle.dataset.endpoint;
      if (annotationId && (endpoint === "start" || endpoint === "end")) {
        event.preventDefault();
        event.stopPropagation();
        this.canvas.setPointerCapture(event.pointerId);
        this.beginHandleDrag(annotationId, endpoint);
      }
      return;
    }

    const line = target?.closest<SVGElement>("[data-annotation-line]");
    if (line) {
      const annotationId = line.dataset.annotationId;
      if (annotationId) {
        event.preventDefault();
        event.stopPropagation();
        workbenchActions.selectAnnotation(annotationId);
      }
      return;
    }

    const transform = this.currentTransform();
    if (!transform || !this.resolvedImageSize || !this.imageReady || this.loadFailed) {
      return;
    }

    this.canvas.setPointerCapture(event.pointerId);
    const pointer = pointerToLocalPoint(event, this.canvas);
    if (this.model.tool === "measureLine") {
      const imagePoint = clampPointToImage(
        screenToImage(pointer, transform),
        this.resolvedImageSize,
      );
      const annotation = createManualLineAnnotation(imagePoint, imagePoint);
      this.interaction = { kind: "draw" };
      this.draftLine = annotation;
      workbenchActions.selectAnnotation(null);
      this.updateCanvas();
      return;
    }

    this.interaction = {
      kind: "pan",
      pointerStart: pointer,
      panStart: {
        panX: this.viewport.panX,
        panY: this.viewport.panY,
      },
    };
  };

  private handlePointerMove = (event: PointerEvent) => {
    if (!this.canvas || !this.interaction) {
      return;
    }

    const transform = this.currentTransform();
    if (!transform || !this.resolvedImageSize) {
      return;
    }

    const pointer = pointerToLocalPoint(event, this.canvas);
    if (this.interaction.kind === "pan") {
      this.viewport = {
        ...this.viewport,
        panX: this.interaction.panStart.panX + (pointer.x - this.interaction.pointerStart.x),
        panY: this.interaction.panStart.panY + (pointer.y - this.interaction.pointerStart.y),
      };
      this.updateCanvas();
      return;
    }

    const imagePoint = clampPointToImage(screenToImage(pointer, transform), this.resolvedImageSize);
    if (this.interaction.kind === "draw") {
      this.draftLine = this.draftLine
        ? {
            ...this.draftLine,
            end: imagePoint,
          }
        : null;
      this.updateCanvas();
      return;
    }

    this.draftLine = this.draftLine
      ? {
          ...this.draftLine,
          [this.interaction.endpoint]: imagePoint,
        }
      : null;
    this.updateCanvas();
  };

  private handlePointerUp = (event: PointerEvent) => {
    const interaction = this.interaction;
    if (!interaction) {
      return;
    }

    if (this.canvas?.hasPointerCapture(event.pointerId)) {
      this.canvas.releasePointerCapture(event.pointerId);
    }

    const draft = this.draftLine;
    this.interaction = null;
    this.draftLine = null;
    this.updateCanvas();

    if (!draft || pointDistance(draft.start, draft.end) < 2) {
      return;
    }

    if (interaction.kind === "draw") {
      void workbenchActions.createLineAnnotation(draft);
      workbenchActions.selectAnnotation(draft.id);
      return;
    }

    if (interaction.kind === "edit") {
      void workbenchActions.updateLineAnnotation(draft);
      workbenchActions.selectAnnotation(draft.id);
    }
  };

  private beginHandleDrag(annotationId: string, endpoint: "start" | "end") {
    if (!this.model) {
      return;
    }

    const annotation = getLineAnnotation(this.model.annotations, annotationId);
    if (!annotation?.editable) {
      return;
    }

    this.interaction = {
      kind: "edit",
      annotationId,
      endpoint,
    };
    this.draftLine = annotation;
    workbenchActions.selectAnnotation(annotationId);
    this.updateCanvas();
  }

  private updateFrame() {
    if (!this.canvas) {
      this.frame = { width: 0, height: 0 };
      return;
    }

    const rect = this.canvas.getBoundingClientRect();
    this.frame = {
      width: rect.width,
      height: rect.height,
    };
  }

  private currentTransform(): ViewerTransform | null {
    if (!this.resolvedImageSize || this.frame.width === 0 || this.frame.height === 0) {
      return null;
    }

    return getViewerTransform(this.frame, this.resolvedImageSize, this.viewport);
  }

  private updateCanvas() {
    const transform = this.currentTransform();
    if (this.zoomEl) {
      this.zoomEl.textContent = `${Math.round(this.viewport.zoom * 100)}%`;
    }
    if (this.draftDistanceEl) {
      const distance = this.draftLine
        ? pointDistance(this.draftLine.start, this.draftLine.end)
        : null;
      if (distance !== null && distance >= 2) {
        this.draftDistanceEl.hidden = false;
        this.draftDistanceEl.textContent = `${Math.round(distance)} px`;
      } else {
        this.draftDistanceEl.hidden = true;
        this.draftDistanceEl.textContent = "";
      }
    }

    if (!transform || !this.image || !this.resolvedImageSize || !this.model) {
      if (this.annotationHost) {
        this.annotationHost.innerHTML = "";
      }
      return;
    }

    this.image.style.width = `${this.resolvedImageSize.width}px`;
    this.image.style.height = `${this.resolvedImageSize.height}px`;
    this.image.style.transform = `translate(${transform.offsetX}px, ${transform.offsetY}px) scale(${transform.scale})`;
    this.image.style.transformOrigin = "0 0";
    this.image.classList.toggle("viewer-canvas__image--ready", this.imageReady);

    if (this.annotationHost && this.imageReady) {
      const draftLineOverride = this.interaction?.kind === "edit" ? this.draftLine : null;
      this.annotationHost.innerHTML = renderAnnotationLayer(
        this.frame,
        transform,
        this.model.annotations,
        this.model.selectedAnnotationId,
        this.interaction?.kind === "draw" ? this.draftLine : null,
        draftLineOverride,
      );
    } else if (this.annotationHost) {
      this.annotationHost.innerHTML = "";
    }
  }
}
