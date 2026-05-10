import fs from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { chromium } from "playwright";
import { createServer } from "vite";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const frontendRoot = path.resolve(__dirname, "..");
const benchDirName = ".annotation-edit-drag-bench";
const benchDir = path.join(frontendRoot, benchDirName);
const indexPath = path.join(benchDir, "index.html");
const mainPath = path.join(benchDir, "main.tsx");

const samples = Number.parseInt(process.env.XRAYVIEW_ANNOTATION_EDIT_BENCH_SAMPLES ?? "7", 10);
const moves = Number.parseInt(process.env.XRAYVIEW_ANNOTATION_EDIT_BENCH_MOVES ?? "400", 10);
const lineCount = Number.parseInt(process.env.XRAYVIEW_ANNOTATION_EDIT_BENCH_LINES ?? "5000", 10);

await fs.rm(benchDir, { force: true, recursive: true });
await fs.mkdir(benchDir, { recursive: true });
await fs.writeFile(
  indexPath,
  '<!doctype html><html><head><meta charset="utf-8"><title>Annotation Edit Drag Bench</title></head><body><div id="root"></div><script type="module" src="./main.tsx"></script></body></html>\n',
);
await fs.writeFile(
  mainPath,
  String.raw`import React, { useMemo, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import { act } from "react-dom/test-utils";
import type { AnnotationBundle, LineAnnotation } from "../src/lib/generated/contracts";
import { usePointerInteractions } from "../src/features/viewer/usePointerInteractions";
import { createViewport, getViewerTransform } from "../src/features/viewer/viewport";
import type { ViewerViewport } from "../src/features/viewer/viewport";

declare global {
  interface Window {
    __runAnnotationEditDragBench: (options: { lineCount: number; moves: number; samples: number }) => Promise<{
      lineCount: number;
      moves: number;
      samples: Array<{
        elapsedMs: number;
        freshLineArrayRenders: number;
        originalLineArrayRenders: number;
        renderCount: number;
      }>;
    }>;
  }
}

globalThis.IS_REACT_ACT_ENVIRONMENT = true;

const frame = { width: 1200, height: 900 };
const imageSize = { width: 1000, height: 800 };
const targetAnnotationId = "line-0";

let annotations: AnnotationBundle = createAnnotations(1);
let latestInteractions: ReturnType<typeof usePointerInteractions> | null = null;
const renderStats = {
  freshLineArrayRenders: 0,
  originalLineArrayRenders: 0,
  renderCount: 0,
};

function createAnnotations(lineCount: number): AnnotationBundle {
  const lines: LineAnnotation[] = Array.from({ length: lineCount }, (_, index) => ({
    id: "line-" + index,
    label: "Line " + index,
    source: "manual",
    start: { x: 20 + (index % 900), y: 30 + (index % 700) },
    end: { x: 80 + (index % 900), y: 90 + (index % 700) },
    editable: true,
    confidence: null,
    measurement: null,
  }));

  return {
    lines,
    rectangles: [],
    polylines: [],
  };
}

function resetRenderStats() {
  renderStats.freshLineArrayRenders = 0;
  renderStats.originalLineArrayRenders = 0;
  renderStats.renderCount = 0;
}

function Harness() {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [viewport, setViewport] = useState<ViewerViewport>(createViewport);
  const transform = useMemo(
    () => getViewerTransform(frame, imageSize, viewport),
    [viewport],
  );
  const pointerInteractions = usePointerInteractions({
    containerRef,
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

  latestInteractions = pointerInteractions;
  renderStats.renderCount += 1;
  if (pointerInteractions.displayedAnnotations.lines === annotations.lines) {
    renderStats.originalLineArrayRenders += 1;
  } else {
    renderStats.freshLineArrayRenders += 1;
  }

  return (
    <div
      ref={containerRef}
      data-annotation-edit-bench-target="true"
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
    pointerId: 11,
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

window.__runAnnotationEditDragBench = async ({ lineCount, moves, samples }) => {
  const container = document.querySelector<HTMLElement>("[data-annotation-edit-bench-target=true]");
  if (!container) {
    throw new Error("missing annotation edit benchmark container");
  }

  annotations = createAnnotations(lineCount);
  const results = [];
  for (let sample = 0; sample < samples + 1; sample += 1) {
    await act(async () => {
      latestInteractions?.resetPointerInteractions();
    });
    resetRenderStats();
    await act(async () => {
      latestInteractions?.beginHandleDrag(targetAnnotationId, "end");
    });

    const start = performance.now();
    for (let move = 0; move < moves; move += 1) {
      await dispatchPointerEvent(
        container,
        "pointermove",
        100 + (move % 700),
        120 + (move % 500),
      );
    }
    await dispatchPointerEvent(container, "pointerup", 800, 620);
    const elapsedMs = performance.now() - start;

    if (sample > 0) {
      results.push({
        elapsedMs,
        freshLineArrayRenders: renderStats.freshLineArrayRenders,
        originalLineArrayRenders: renderStats.originalLineArrayRenders,
        renderCount: renderStats.renderCount,
      });
    }
  }

  return {
    lineCount,
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
  await page.waitForFunction(() => typeof window.__runAnnotationEditDragBench === "function");
  const result = await page.evaluate(
    ({ lineCount, moves, samples }) =>
      window.__runAnnotationEditDragBench({ lineCount, moves, samples }),
    { lineCount, moves, samples },
  );

  const elapsed = result.samples.map((sample) => sample.elapsedMs);
  const freshLineArrayRenders = result.samples.map((sample) => sample.freshLineArrayRenders);
  const renderCounts = result.samples.map((sample) => sample.renderCount);
  const meanElapsed = mean(elapsed);
  const minElapsed = Math.min(...elapsed);
  const meanFreshLineArrayRenders = mean(freshLineArrayRenders);
  const meanRenderCount = mean(renderCounts);

  console.log(`Annotation edit drag benchmark (${result.lineCount} lines, ${result.moves} moves, ${result.samples.length} samples)`);
  console.log(`mean: ${meanElapsed.toFixed(3)} ms`);
  console.log(`min: ${minElapsed.toFixed(3)} ms`);
  console.log(`mean fresh line-array renders: ${meanFreshLineArrayRenders.toFixed(1)}`);
  console.log(`mean renders: ${meanRenderCount.toFixed(1)}`);
  console.log(`samples: ${result.samples.map((sample) => `${sample.elapsedMs.toFixed(3)}ms/${sample.freshLineArrayRenders}fresh/${sample.renderCount}renders`).join(", ")}`);
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
