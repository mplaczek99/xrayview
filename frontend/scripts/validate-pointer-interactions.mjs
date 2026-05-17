import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { chromium } from "playwright";
import { createServer } from "vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(__dirname, "..");
const benchDirName = ".pointer-interactions-bench";
const benchDir = path.join(frontendRoot, benchDirName);
const indexPath = path.join(benchDir, "index.html");
const mainPath = path.join(benchDir, "main.tsx");

const samples = Number.parseInt(process.env.XRAYVIEW_POINTER_BENCH_SAMPLES ?? "7", 10);
const moves = Number.parseInt(process.env.XRAYVIEW_POINTER_BENCH_MOVES ?? "400", 10);

await fs.rm(benchDir, { force: true, recursive: true });
await fs.mkdir(benchDir, { recursive: true });
await fs.writeFile(
  indexPath,
  '<!doctype html><html><head><meta charset="utf-8"><title>Pointer Bench</title></head><body><div id="root"></div><script type="module" src="./main.tsx"></script></body></html>\n',
);
await fs.writeFile(
  mainPath,
  String.raw`import React, { useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { act } from "react-dom/test-utils";
import { emptyAnnotationBundle } from "../src/features/annotations/tools";
import { usePointerInteractions } from "../src/features/viewer/usePointerInteractions";
import { createViewport, getViewerTransform } from "../src/features/viewer/viewport";
import type { ViewerViewport } from "../src/features/viewer/viewport";

declare global {
  interface Window {
    __runPointerInteractionsBench: (options: { samples: number; moves: number }) => Promise<{
      moves: number;
      samples: Array<{ addCount: number; elapsedMs: number; listenerOps: number; removeCount: number }>;
    }>;
  }
}

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const originalSetPointerCapture = HTMLElement.prototype.setPointerCapture;
HTMLElement.prototype.setPointerCapture = function setPointerCapture(pointerId: number) {
  try {
    originalSetPointerCapture.call(this, pointerId);
  } catch {
    // Synthetic pointer events are enough for this benchmark; capture state is not.
  }
};

const counts = {
  add: 0,
  remove: 0,
};
const originalAddEventListener = EventTarget.prototype.addEventListener;
const originalRemoveEventListener = EventTarget.prototype.removeEventListener;

function isBenchPointerListener(target: EventTarget, type: string) {
  return (
    target instanceof HTMLElement &&
    target.dataset.pointerBenchTarget === "true" &&
    (type === "pointermove" || type === "pointerup")
  );
}

EventTarget.prototype.addEventListener = function addEventListener(
  type: string,
  listener: EventListenerOrEventListenerObject | null,
  options?: boolean | AddEventListenerOptions,
) {
  if (isBenchPointerListener(this, type)) {
    counts.add += 1;
  }
  return originalAddEventListener.call(this, type, listener, options);
};

EventTarget.prototype.removeEventListener = function removeEventListener(
  type: string,
  listener: EventListenerOrEventListenerObject | null,
  options?: boolean | EventListenerOptions,
) {
  if (isBenchPointerListener(this, type)) {
    counts.remove += 1;
  }
  return originalRemoveEventListener.call(this, type, listener, options);
};

const frame = { width: 1200, height: 900 };
const imageSize = { width: 1000, height: 800 };
const annotations = emptyAnnotationBundle();

function Harness() {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [viewport, setViewport] = useState<ViewerViewport>(createViewport);
  const transform = useMemo(
    () => getViewerTransform(frame, imageSize, viewport),
    [viewport],
  );
  const pointerInteractions = usePointerInteractions({
    containerRef,
    enabled: true,
    annotations,
    imageReady: true,
    imageSize,
    tool: "pan",
    transform,
    viewport,
    setViewport,
    onSelectAnnotation: () => {},
    onCreateLine: () => {},
    onUpdateLine: () => {},
  });

  return (
    <div
      ref={containerRef}
      data-pointer-bench-target="true"
      onPointerDown={pointerInteractions.beginBackgroundInteraction}
      style={{ height: "900px", width: "1200px" }}
    />
  );
}

const rootElement = document.getElementById("root");
if (!rootElement) {
  throw new Error("missing root element");
}

await act(async () => {
  createRoot(rootElement).render(<Harness />);
});

function pointerEvent(type: string, x: number, y: number) {
  return new PointerEvent(type, {
    bubbles: true,
    button: 0,
    buttons: type === "pointerup" ? 0 : 1,
    clientX: x,
    clientY: y,
    pointerId: 7,
    pointerType: "mouse",
  });
}

async function dispatchPointerEvent(
  container: HTMLElement,
  type: string,
  x: number,
  y: number,
) {
  await act(async () => {
    container.dispatchEvent(pointerEvent(type, x, y));
  });
}

window.__runPointerInteractionsBench = async ({ samples, moves }) => {
  const container = document.querySelector<HTMLElement>("[data-pointer-bench-target=true]");
  if (!container) {
    throw new Error("missing pointer benchmark container");
  }

  const results = [];
  for (let sample = 0; sample < samples + 1; sample += 1) {
    await dispatchPointerEvent(container, "pointerdown", 120, 160);
    counts.add = 0;
    counts.remove = 0;
    const start = performance.now();
    for (let move = 0; move < moves; move += 1) {
      await dispatchPointerEvent(container, "pointermove", 121 + (move % 600), 161 + (move % 320));
    }
    await dispatchPointerEvent(container, "pointerup", 760, 480);
    const elapsedMs = performance.now() - start;
    if (sample > 0) {
      results.push({
        addCount: counts.add,
        elapsedMs,
        listenerOps: counts.add + counts.remove,
        removeCount: counts.remove,
      });
    }
  }

  return {
    moves,
    samples: results,
  };
};
`,
);

let server;
let browser;
try {
  server = await createServer({
    root: frontendRoot,
    configFile: false,
    plugins: [react()],
    clearScreen: false,
    logLevel: "error",
    server: {
      host: "127.0.0.1",
      port: 0,
      strictPort: false,
    },
  });
  await server.listen();
  const baseUrl = server.resolvedUrls?.local?.[0];
  if (!baseUrl) {
    throw new Error("Vite did not report a local URL");
  }

  try {
    browser = await chromium.launch({ channel: "chromium" });
  } catch {
    browser = await chromium.launch();
  }

  const page = await browser.newPage();
  await page.goto(new URL(`${benchDirName}/index.html`, baseUrl).toString());
  await page.waitForFunction(() => typeof window.__runPointerInteractionsBench === "function");
  const result = await page.evaluate(
    ({ moves, samples }) => window.__runPointerInteractionsBench({ moves, samples }),
    { moves, samples },
  );

  const elapsed = result.samples.map((sample) => sample.elapsedMs);
  const listenerOps = result.samples.map((sample) => sample.listenerOps);
  const meanElapsed = mean(elapsed);
  const meanListenerOps = mean(listenerOps);
  const minElapsed = Math.min(...elapsed);

  console.log(`Pointer interaction drag benchmark (${result.moves} moves, ${result.samples.length} samples)`);
  console.log(`mean: ${meanElapsed.toFixed(3)} ms`);
  console.log(`min: ${minElapsed.toFixed(3)} ms`);
  console.log(`mean listener ops during drag: ${meanListenerOps.toFixed(1)}`);
  console.log(`samples: ${result.samples.map((sample) => `${sample.elapsedMs.toFixed(3)}ms/${sample.listenerOps}ops`).join(", ")}`);
} finally {
  if (browser) {
    await browser.close();
  }
  if (server) {
    await server.close();
  }
  await fs.rm(benchDir, { force: true, recursive: true });
}

function mean(values) {
  return values.reduce((sum, value) => sum + value, 0) / values.length;
}
