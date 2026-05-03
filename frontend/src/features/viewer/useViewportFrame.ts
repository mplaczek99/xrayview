import { useLayoutEffect, useState } from "react";
import type { RefObject } from "react";
import type { ViewerFrame } from "./viewport";

export function useViewportFrame(
  containerRef: RefObject<HTMLElement>,
  enabled: boolean,
): ViewerFrame {
  const [frame, setFrame] = useState<ViewerFrame>({ width: 0, height: 0 });

  useLayoutEffect(() => {
    if (!enabled) {
      return;
    }

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

    return () => {
      observer.disconnect();
    };
  }, [containerRef, enabled]);

  return frame;
}
