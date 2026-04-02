import type { AnnotationPoint } from "../../lib/generated/contracts";

export interface ViewerFrame {
  width: number;
  height: number;
}

export interface ViewerImageSize {
  width: number;
  height: number;
}

export interface ViewerViewport {
  zoom: number;
  panX: number;
  panY: number;
}

export interface ViewerTransform {
  scale: number;
  offsetX: number;
  offsetY: number;
}

const MIN_ZOOM = 1;
const MAX_ZOOM = 12;

export function createViewport(): ViewerViewport {
  return {
    zoom: 1,
    panX: 0,
    panY: 0,
  };
}

export function clampZoom(zoom: number): number {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, zoom));
}

export function getViewerTransform(
  frame: ViewerFrame,
  image: ViewerImageSize,
  viewport: ViewerViewport,
): ViewerTransform {
  const fitScale = Math.min(frame.width / image.width, frame.height / image.height);
  const scale = fitScale * clampZoom(viewport.zoom);
  const offsetX = (frame.width - image.width * scale) / 2 + viewport.panX;
  const offsetY = (frame.height - image.height * scale) / 2 + viewport.panY;

  return {
    scale,
    offsetX,
    offsetY,
  };
}

export function imageToScreen(
  point: AnnotationPoint,
  transform: ViewerTransform,
): AnnotationPoint {
  return {
    x: transform.offsetX + point.x * transform.scale,
    y: transform.offsetY + point.y * transform.scale,
  };
}

export function screenToImage(
  point: AnnotationPoint,
  transform: ViewerTransform,
): AnnotationPoint {
  return {
    x: (point.x - transform.offsetX) / transform.scale,
    y: (point.y - transform.offsetY) / transform.scale,
  };
}

export function clampPointToImage(
  point: AnnotationPoint,
  image: ViewerImageSize,
): AnnotationPoint {
  return {
    x: clamp(point.x, 0, image.width),
    y: clamp(point.y, 0, image.height),
  };
}

export function zoomAtPoint(
  viewport: ViewerViewport,
  frame: ViewerFrame,
  image: ViewerImageSize,
  pointer: AnnotationPoint,
  factor: number,
): ViewerViewport {
  const currentTransform = getViewerTransform(frame, image, viewport);
  const anchor = screenToImage(pointer, currentTransform);
  const next = {
    ...viewport,
    zoom: clampZoom(viewport.zoom * factor),
  };
  const nextTransform = getViewerTransform(frame, image, next);
  const anchoredScreen = imageToScreen(anchor, nextTransform);

  return {
    ...next,
    panX: next.panX + (pointer.x - anchoredScreen.x),
    panY: next.panY + (pointer.y - anchoredScreen.y),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}
