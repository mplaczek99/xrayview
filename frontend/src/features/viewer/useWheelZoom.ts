import { useEffect, useRef } from "react";
import type { Dispatch, RefObject, SetStateAction } from "react";
import type {
  ViewerFrame,
  ViewerImageSize,
  ViewerViewport,
} from "./viewport";
import { zoomAtPoint } from "./viewport";

interface UseWheelZoomOptions {
  containerRef: RefObject<HTMLElement>;
  enabled: boolean;
  frame: ViewerFrame;
  imageReady: boolean;
  imageSize: ViewerImageSize | null;
  setViewport: Dispatch<SetStateAction<ViewerViewport>>;
}

export function useWheelZoom({
  containerRef,
  enabled,
  frame,
  imageReady,
  imageSize,
  setViewport,
}: UseWheelZoomOptions) {
  const wheelStateRef = useRef({ imageSize, imageReady, frame });

  useEffect(() => {
    wheelStateRef.current = { imageSize, imageReady, frame };
  }, [imageSize, imageReady, frame]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const element = containerRef.current;
    if (!element) {
      return;
    }
    const target = element;

    function handleWheel(event: WheelEvent) {
      const { imageSize: imgSize, imageReady: ready, frame: fr } =
        wheelStateRef.current;
      if (!imgSize || !ready) {
        return;
      }

      event.preventDefault();
      const rect = target.getBoundingClientRect();
      const pointer = {
        x: event.clientX - rect.left,
        y: event.clientY - rect.top,
      };
      const factor = event.deltaY < 0 ? 1.12 : 0.9;
      setViewport((current) =>
        zoomAtPoint(current, fr, imgSize, pointer, factor),
      );
    }

    target.addEventListener("wheel", handleWheel, { passive: false });
    return () => {
      target.removeEventListener("wheel", handleWheel);
    };
  }, [containerRef, enabled, setViewport]);
}
