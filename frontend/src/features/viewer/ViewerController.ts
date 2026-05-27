import type { ViewerRenderModel } from "../../app/htmxView";
import { workbenchActions } from "../../app/store/workbenchStore";
import type {
  AnnotationBundle,
  AnnotationPoint,
  LineAnnotation,
} from "../../lib/generated/contracts";
import {
  createManualLineAnnotation,
  finalizeManualLineAnnotation,
  getLineAnnotation,
} from "../annotations/tools";
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

type CanvasRect = Pick<DOMRectReadOnly, "left" | "top" | "width" | "height">;

const SVG_NS = "http://www.w3.org/2000/svg";

function pointDistance(left: AnnotationPoint, right: AnnotationPoint): number {
  return Math.hypot(left.x - right.x, left.y - right.y);
}

function lineMidpoint(line: LineAnnotation): AnnotationPoint {
  return {
    x: (line.start.x + line.end.x) / 2,
    y: (line.start.y + line.end.y) / 2,
  };
}

function pointerToLocalPoint(event: MouseEvent, rect: CanvasRect): AnnotationPoint {
  return {
    x: event.clientX - rect.left,
    y: event.clientY - rect.top,
  };
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

function createSvgElement<K extends keyof SVGElementTagNameMap>(
  tagName: K,
): SVGElementTagNameMap[K] {
  return document.createElementNS(SVG_NS, tagName);
}

function setSvgAttributes(element: Element, attributes: Record<string, string | number>): void {
  for (const [name, value] of Object.entries(attributes)) {
    element.setAttribute(name, String(value));
  }
}

function setSvgElementHidden(element: SVGElement, hidden: boolean): void {
  if (hidden) {
    element.setAttribute("display", "none");
  } else {
    element.removeAttribute("display");
  }
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
  private canvasRect: CanvasRect | null = null;
  private canvasUpdateFrame: number | null = null;
  private resolvedImageSize: ViewerImageSize | null = null;
  private imageReady = false;
  private loadFailed = false;
  private previewUrl: string | null = null;
  private resetKey: string | null = null;
  private interaction: ViewerInteraction | null = null;
  private draftLine: LineAnnotation | null = null;
  private annotationSvg: SVGSVGElement | null = null;
  private annotationTransformGroup: SVGGElement | null = null;
  private annotationContentGroup: SVGGElement | null = null;
  private annotationDraftGroup: SVGGElement | null = null;
  private renderedAnnotations: AnnotationBundle | null = null;
  private renderedSelectedAnnotationId: string | null = null;
  private renderedDraftMode: "draw" | "edit" | null = null;
  private renderedDraftLineId: string | null = null;
  private draftLineNode: SVGLineElement | null = null;
  private draftLabelNode: SVGTextElement | null = null;
  private draftStartHandleNode: SVGCircleElement | null = null;
  private draftEndHandleNode: SVGCircleElement | null = null;
  private hiddenStaticAnnotationId: string | null = null;

  reusableStageFor(model: ViewerRenderModel): HTMLElement | null {
    if (
      !this.stage ||
      !this.image ||
      !model.previewUrl ||
      this.previewUrl !== model.previewUrl ||
      this.resetKey !== model.viewportResetKey
    ) {
      return null;
    }

    return this.stage;
  }

  mount(root: ParentNode, model: ViewerRenderModel) {
    const stage = root.querySelector<HTMLElement>("[data-viewer-stage]");
    if (!stage || !model.previewUrl) {
      this.detach();
      this.model = model;
      this.stage = null;
      this.canvas = null;
      this.image = null;
      this.annotationHost = null;
      this.clearAnnotationLayer();
      return;
    }

    const sameStage = this.stage === stage && this.canvas?.isConnected === true;
    const previewChanged = this.previewUrl !== model.previewUrl;
    const resetChanged = this.resetKey !== model.viewportResetKey;
    if (previewChanged || resetChanged) {
      this.viewport = createViewport();
      this.interaction = null;
      this.draftLine = null;
      this.imageReady = false;
      this.loadFailed = false;
    }

    if (!sameStage) {
      this.detach();
    }

    this.model = model;
    this.previewUrl = model.previewUrl;
    this.resetKey = model.viewportResetKey;
    this.resolvedImageSize = model.imageSize;
    this.stage = stage;
    this.canvas = sameStage
      ? this.canvas
      : stage.querySelector<HTMLElement>("[data-viewer-canvas]");
    this.image = sameStage
      ? this.image
      : stage.querySelector<HTMLImageElement>("[data-viewer-image]");
    this.annotationHost = sameStage
      ? this.annotationHost
      : stage.querySelector<HTMLElement>("[data-annotation-layer]");
    this.draftDistanceEl = sameStage
      ? this.draftDistanceEl
      : stage.querySelector<HTMLElement>("[data-viewer-draft-distance]");
    this.zoomEl = sameStage ? this.zoomEl : stage.querySelector<HTMLElement>("[data-viewer-zoom]");

    if (!this.canvas || !this.image || !this.annotationHost) {
      return;
    }

    this.canvas.classList.toggle("viewer-canvas--pan", model.tool === "pan");
    this.canvas.classList.toggle("viewer-canvas--measureLine", model.tool === "measureLine");

    if (!sameStage) {
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
    }

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
    this.cancelScheduledCanvasUpdate();
    this.clearAnnotationLayer();
    this.stage = null;
    this.canvas = null;
    this.image = null;
    this.annotationHost = null;
    this.draftDistanceEl = null;
    this.zoomEl = null;
    this.canvasRect = null;
  }

  resetViewport() {
    this.viewport = createViewport();
    this.interaction = null;
    this.draftLine = null;
    this.flushCanvasUpdate();
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
    const rect = this.updateFrame();
    if (!rect) {
      return;
    }
    const pointer = pointerToLocalPoint(event, rect);
    const factor = event.deltaY < 0 ? 1.12 : 0.9;
    this.viewport = zoomAtPoint(this.viewport, this.frame, this.resolvedImageSize, pointer, factor);
    this.flushCanvasUpdate();
  };

  private handlePointerDown = (event: PointerEvent) => {
    if (!this.canvas || !this.model || event.button !== 0) {
      return;
    }

    this.updateFrame();
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
    const rect = this.currentCanvasRect();
    if (!rect) {
      return;
    }
    const pointer = pointerToLocalPoint(event, rect);
    if (this.model.tool === "measureLine") {
      const imagePoint = clampPointToImage(
        screenToImage(pointer, transform),
        this.resolvedImageSize,
      );
      const annotation = createManualLineAnnotation(imagePoint, imagePoint);
      this.interaction = { kind: "draw" };
      this.draftLine = annotation;
      workbenchActions.selectAnnotation(null);
      this.flushCanvasUpdate();
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

    const rect = this.currentCanvasRect();
    if (!rect) {
      return;
    }
    const pointer = pointerToLocalPoint(event, rect);
    if (this.interaction.kind === "pan") {
      this.viewport = {
        ...this.viewport,
        panX: this.interaction.panStart.panX + (pointer.x - this.interaction.pointerStart.x),
        panY: this.interaction.panStart.panY + (pointer.y - this.interaction.pointerStart.y),
      };
      this.scheduleCanvasUpdate();
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
      this.scheduleCanvasUpdate();
      return;
    }

    this.draftLine = this.draftLine
      ? {
          ...this.draftLine,
          [this.interaction.endpoint]: imagePoint,
        }
      : null;
    this.scheduleCanvasUpdate();
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
    this.flushCanvasUpdate();

    if (!draft || pointDistance(draft.start, draft.end) < 2) {
      return;
    }

    if (interaction.kind === "draw") {
      const measurement = finalizeManualLineAnnotation(draft);
      void workbenchActions.createLineAnnotation(measurement);
      workbenchActions.selectAnnotation(measurement.id);
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
    this.flushCanvasUpdate();
  }

  private updateFrame(): CanvasRect | null {
    if (!this.canvas) {
      this.frame = { width: 0, height: 0 };
      this.canvasRect = null;
      return null;
    }

    const rect = this.canvas.getBoundingClientRect();
    this.canvasRect = {
      left: rect.left,
      top: rect.top,
      width: rect.width,
      height: rect.height,
    };
    this.frame = {
      width: rect.width,
      height: rect.height,
    };
    return this.canvasRect;
  }

  private currentCanvasRect(): CanvasRect | null {
    return this.canvasRect ?? this.updateFrame();
  }

  private scheduleCanvasUpdate(): void {
    if (this.canvasUpdateFrame !== null) {
      return;
    }

    this.canvasUpdateFrame = window.requestAnimationFrame(() => {
      this.canvasUpdateFrame = null;
      this.updateCanvas();
    });
  }

  private cancelScheduledCanvasUpdate(): void {
    if (this.canvasUpdateFrame === null) {
      return;
    }

    window.cancelAnimationFrame(this.canvasUpdateFrame);
    this.canvasUpdateFrame = null;
  }

  private flushCanvasUpdate(): void {
    this.cancelScheduledCanvasUpdate();
    this.updateCanvas();
  }

  private currentTransform(): ViewerTransform | null {
    if (!this.resolvedImageSize || this.frame.width === 0 || this.frame.height === 0) {
      return null;
    }

    return getViewerTransform(this.frame, this.resolvedImageSize, this.viewport);
  }

  private clearAnnotationLayer() {
    if (this.annotationHost) {
      this.annotationHost.replaceChildren();
    }
    this.annotationSvg = null;
    this.annotationTransformGroup = null;
    this.annotationContentGroup = null;
    this.annotationDraftGroup = null;
    this.renderedAnnotations = null;
    this.renderedSelectedAnnotationId = null;
    this.clearDraftNodeReferences();
    this.hiddenStaticAnnotationId = null;
  }

  private ensureAnnotationLayer(): SVGGElement | null {
    if (!this.annotationHost) {
      return null;
    }
    if (
      this.annotationSvg &&
      this.annotationTransformGroup &&
      this.annotationContentGroup &&
      this.annotationDraftGroup
    ) {
      return this.annotationContentGroup;
    }

    const svg = createSvgElement("svg");
    svg.classList.add("annotation-layer");
    svg.setAttribute("aria-hidden", "true");
    const transformGroup = createSvgElement("g");
    const contentGroup = createSvgElement("g");
    const draftGroup = createSvgElement("g");
    transformGroup.classList.add("annotation-transform");
    draftGroup.classList.add("annotation-draft");
    transformGroup.append(contentGroup, draftGroup);
    svg.append(transformGroup);
    this.annotationHost.replaceChildren(svg);
    this.annotationSvg = svg;
    this.annotationTransformGroup = transformGroup;
    this.annotationContentGroup = contentGroup;
    this.annotationDraftGroup = draftGroup;
    return contentGroup;
  }

  private syncAnnotationLayer(
    annotations: AnnotationBundle,
    selectedAnnotationId: string | null,
    transform: ViewerTransform,
    draftLine: LineAnnotation | null,
    draftLineOverride: LineAnnotation | null,
  ) {
    const contentGroup = this.ensureAnnotationLayer();
    if (!contentGroup || !this.annotationSvg || !this.annotationTransformGroup) {
      return;
    }

    setSvgAttributes(this.annotationSvg, {
      width: this.frame.width,
      height: this.frame.height,
      viewBox: `0 0 ${this.frame.width} ${this.frame.height}`,
    });
    this.annotationTransformGroup.setAttribute(
      "transform",
      `matrix(${transform.scale} 0 0 ${transform.scale} ${transform.offsetX} ${transform.offsetY})`,
    );

    if (
      this.renderedAnnotations !== annotations ||
      this.renderedSelectedAnnotationId !== selectedAnnotationId
    ) {
      contentGroup.replaceChildren(...this.buildAnnotationNodes(annotations, selectedAnnotationId));
      this.renderedAnnotations = annotations;
      this.renderedSelectedAnnotationId = selectedAnnotationId;
      this.hiddenStaticAnnotationId = null;
    }

    this.syncDraftLayer(draftLine, draftLineOverride);
  }

  private buildAnnotationNodes(
    annotations: AnnotationBundle,
    selectedAnnotationId: string | null,
  ): SVGElement[] {
    const nodes: SVGElement[] = [];
    const selectedBase = annotations.lines.find((line) => line.id === selectedAnnotationId) ?? null;

    for (const annotation of annotations.rectangles) {
      const rect = createSvgElement("rect");
      rect.classList.add("annotation-layer__rect");
      setSvgAttributes(rect, {
        x: annotation.x,
        y: annotation.y,
        width: annotation.width,
        height: annotation.height,
        "vector-effect": "non-scaling-stroke",
      });
      nodes.push(rect);
    }

    for (const annotation of annotations.polylines) {
      const polyline = createSvgElement(annotation.closed ? "polygon" : "polyline");
      polyline.classList.add(
        "annotation-layer__polyline",
        `annotation-layer__polyline--${annotation.source}`,
      );
      setSvgAttributes(polyline, {
        points: annotation.points.map((point) => `${point.x},${point.y}`).join(" "),
        "vector-effect": "non-scaling-stroke",
      });
      if (!annotation.closed) {
        polyline.setAttribute("fill", "none");
      }
      nodes.push(polyline);
    }

    for (const annotation of annotations.lines) {
      nodes.push(this.buildLineAnnotationNode(annotation, selectedAnnotationId));
    }

    if (selectedBase) {
      nodes.push(this.buildHandleNode(selectedBase, "start"));
      nodes.push(this.buildHandleNode(selectedBase, "end"));
    }

    return nodes;
  }

  private buildLineAnnotationNode(
    annotation: LineAnnotation,
    selectedAnnotationId: string | null,
  ): SVGGElement {
    const mid = lineMidpoint(annotation);
    const group = createSvgElement("g");
    group.dataset.annotationStaticId = annotation.id;
    const line = createSvgElement("line");
    line.classList.add("annotation-layer__line", `annotation-layer__line--${annotation.source}`);
    if (annotation.id === selectedAnnotationId) {
      line.classList.add("annotation-layer__line--selected");
    }
    setSvgAttributes(line, {
      x1: annotation.start.x,
      y1: annotation.start.y,
      x2: annotation.end.x,
      y2: annotation.end.y,
      "vector-effect": "non-scaling-stroke",
      "data-annotation-id": annotation.id,
    });
    line.dataset.annotationLine = "";

    const label = createSvgElement("text");
    label.classList.add("annotation-layer__label");
    setSvgAttributes(label, {
      x: mid.x,
      y: mid.y - 10,
      "text-anchor": "middle",
      "pointer-events": "none",
      opacity: "1",
    });
    label.textContent = lineLabel(annotation);
    group.append(line, label);
    return group;
  }

  private buildDraftLineNode(draftLine: LineAnnotation): SVGLineElement {
    const line = createSvgElement("line");
    line.classList.add("annotation-layer__line", "annotation-layer__line--draft");
    setSvgAttributes(line, {
      x1: draftLine.start.x,
      y1: draftLine.start.y,
      x2: draftLine.end.x,
      y2: draftLine.end.y,
      "vector-effect": "non-scaling-stroke",
    });
    return line;
  }

  private buildHandleNode(
    selectedLine: LineAnnotation,
    endpoint: "start" | "end",
  ): SVGCircleElement {
    const point = selectedLine[endpoint];
    const handle = createSvgElement("circle");
    handle.classList.add("annotation-layer__handle");
    setSvgAttributes(handle, {
      cx: point.x,
      cy: point.y,
      r: 7,
      "vector-effect": "non-scaling-stroke",
      "data-annotation-id": selectedLine.id,
      "data-endpoint": endpoint,
    });
    handle.dataset.annotationHandle = "";
    return handle;
  }

  private syncDraftLayer(
    draftLine: LineAnnotation | null,
    draftLineOverride: LineAnnotation | null,
  ): void {
    if (!this.annotationDraftGroup) {
      return;
    }

    const mode = draftLine ? "draw" : draftLineOverride ? "edit" : null;
    const line = draftLine ?? draftLineOverride;
    this.syncStaticAnnotationVisibility(mode === "edit" && line ? line.id : null);

    if (!mode || !line) {
      if (this.renderedDraftMode !== null || this.annotationDraftGroup.childNodes.length > 0) {
        this.annotationDraftGroup.replaceChildren();
        this.clearDraftNodeReferences();
      }
      return;
    }

    if (
      this.renderedDraftMode !== mode ||
      this.renderedDraftLineId !== line.id ||
      !this.draftLineNode ||
      (mode === "edit" &&
        (!this.draftLabelNode || !this.draftStartHandleNode || !this.draftEndHandleNode))
    ) {
      this.createDraftNodes(mode, line);
    }

    this.updateDraftNodes(mode, line);
  }

  private createDraftNodes(mode: "draw" | "edit", line: LineAnnotation): void {
    if (!this.annotationDraftGroup) {
      return;
    }

    this.clearDraftNodeReferences();
    if (mode === "draw") {
      const draftLine = this.buildDraftLineNode(line);
      this.annotationDraftGroup.replaceChildren(draftLine);
      this.draftLineNode = draftLine;
    } else {
      const group = createSvgElement("g");
      const draftLine = createSvgElement("line");
      draftLine.classList.add(
        "annotation-layer__line",
        `annotation-layer__line--${line.source}`,
        "annotation-layer__line--selected",
      );
      setSvgAttributes(draftLine, {
        "vector-effect": "non-scaling-stroke",
        "data-annotation-id": line.id,
      });
      draftLine.dataset.annotationLine = "";

      const label = createSvgElement("text");
      label.classList.add("annotation-layer__label");
      setSvgAttributes(label, {
        "text-anchor": "middle",
        "pointer-events": "none",
        opacity: "1",
      });

      const startHandle = this.buildHandleNode(line, "start");
      const endHandle = this.buildHandleNode(line, "end");
      group.append(draftLine, label, startHandle, endHandle);
      this.annotationDraftGroup.replaceChildren(group);
      this.draftLineNode = draftLine;
      this.draftLabelNode = label;
      this.draftStartHandleNode = startHandle;
      this.draftEndHandleNode = endHandle;
    }

    this.renderedDraftMode = mode;
    this.renderedDraftLineId = line.id;
  }

  private updateDraftNodes(mode: "draw" | "edit", line: LineAnnotation): void {
    if (!this.draftLineNode) {
      return;
    }

    setSvgAttributes(this.draftLineNode, {
      x1: line.start.x,
      y1: line.start.y,
      x2: line.end.x,
      y2: line.end.y,
    });

    if (mode !== "edit") {
      return;
    }

    const mid = lineMidpoint(line);
    if (this.draftLabelNode) {
      setSvgAttributes(this.draftLabelNode, {
        x: mid.x,
        y: mid.y - 10,
      });
      this.draftLabelNode.textContent = lineLabel(line);
    }
    if (this.draftStartHandleNode) {
      setSvgAttributes(this.draftStartHandleNode, {
        cx: line.start.x,
        cy: line.start.y,
      });
    }
    if (this.draftEndHandleNode) {
      setSvgAttributes(this.draftEndHandleNode, {
        cx: line.end.x,
        cy: line.end.y,
      });
    }
  }

  private syncStaticAnnotationVisibility(hiddenAnnotationId: string | null): void {
    if (!this.annotationContentGroup || this.hiddenStaticAnnotationId === hiddenAnnotationId) {
      return;
    }

    for (const group of this.annotationContentGroup.querySelectorAll<SVGGElement>(
      "[data-annotation-static-id]",
    )) {
      const shouldHide = group.dataset.annotationStaticId === hiddenAnnotationId;
      const wasHidden = group.dataset.annotationStaticId === this.hiddenStaticAnnotationId;
      if (shouldHide || wasHidden) {
        setSvgElementHidden(group, shouldHide);
      }
    }
    for (const handle of this.annotationContentGroup.querySelectorAll<SVGCircleElement>(
      "[data-annotation-handle]",
    )) {
      const shouldHide = handle.dataset.annotationId === hiddenAnnotationId;
      const wasHidden = handle.dataset.annotationId === this.hiddenStaticAnnotationId;
      if (shouldHide || wasHidden) {
        setSvgElementHidden(handle, shouldHide);
      }
    }
    this.hiddenStaticAnnotationId = hiddenAnnotationId;
  }

  private clearDraftNodeReferences(): void {
    this.renderedDraftMode = null;
    this.renderedDraftLineId = null;
    this.draftLineNode = null;
    this.draftLabelNode = null;
    this.draftStartHandleNode = null;
    this.draftEndHandleNode = null;
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
      this.clearAnnotationLayer();
      return;
    }

    this.image.style.width = `${this.resolvedImageSize.width}px`;
    this.image.style.height = `${this.resolvedImageSize.height}px`;
    this.image.style.transform = `translate(${transform.offsetX}px, ${transform.offsetY}px) scale(${transform.scale})`;
    this.image.style.transformOrigin = "0 0";
    this.image.classList.toggle("viewer-canvas__image--ready", this.imageReady);

    if (this.annotationHost && this.imageReady) {
      const draftLineOverride = this.interaction?.kind === "edit" ? this.draftLine : null;
      this.syncAnnotationLayer(
        this.model.annotations,
        this.model.selectedAnnotationId,
        transform,
        this.interaction?.kind === "draw" ? this.draftLine : null,
        draftLineOverride,
      );
    } else if (this.annotationHost) {
      this.clearAnnotationLayer();
    }
  }
}
