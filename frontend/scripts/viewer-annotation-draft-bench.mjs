import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { createServer } from "vite";

const args = new Map();
for (let index = 2; index < process.argv.length; index += 2) {
  args.set(process.argv[index], process.argv[index + 1]);
}

const annotationCount = Number(args.get("--annotations") ?? 300);
const moveCount = Number(args.get("--moves") ?? 240);
const sampleCount = Number(args.get("--samples") ?? 7);
const mode = args.get("--mode") === "edit" ? "edit" : "draw";

const server = await createServer({
  configFile: false,
  root: fileURLToPath(new URL("..", import.meta.url)),
  server: {
    host: "127.0.0.1",
    port: 0,
  },
  appType: "spa",
});

await server.listen();
const address = server.httpServer?.address();
if (!address || typeof address === "string") {
  throw new Error("Vite did not expose a loopback server address.");
}

const browser = await chromium.launch();
const samples = [];
const previewUrl = "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==";

try {
  for (let sample = 0; sample < sampleCount; sample += 1) {
    const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
    await page.goto(`http://127.0.0.1:${address.port}/`);
    samples.push(
      await page.evaluate(
        async ({ annotationCount, mode, moveCount, previewUrl }) => {
          document.body.innerHTML = `
            <div data-viewer-stage>
              <div
                data-viewer-canvas
                style="position:relative;width:1200px;height:820px;overflow:hidden;touch-action:none"
              >
                <img data-viewer-image alt="" src="${previewUrl}" />
                <div data-annotation-layer></div>
                <span data-viewer-draft-distance hidden></span>
                <span data-viewer-zoom></span>
              </div>
            </div>
          `;

          const image = document.querySelector("[data-viewer-image]");
          if (!image) {
            throw new Error("Benchmark image missing.");
          }

          const { ViewerController } = await import("/src/features/viewer/ViewerController.ts");
          const controller = new ViewerController();
          const annotations = {
            rectangles: [],
            polylines: [],
            lines: Array.from({ length: annotationCount }, (_, index) => {
              const column = index % 30;
              const row = Math.floor(index / 30);
              const x = 24 + column * 38;
              const y = 32 + row * 24;
              return {
                id: `line-${index}`,
                label: `Line ${index}`,
                source: "manual",
                editable: true,
                start: { x, y },
                end: { x: x + 26, y: y + 12 },
                measurement: {
                  pixelLength: 28.635642126552707,
                  calibratedLengthMm: null,
                },
              };
            }),
          };
          const model = {
            previewUrl,
            viewportResetKey: "bench",
            imageSize: { width: 1200, height: 820 },
            tool: "measureLine",
            annotations,
            selectedAnnotationId: mode === "edit" ? "line-0" : null,
            measurementScale: null,
            analysisOverlayMode: "outline",
          };

          const counters = {
            canvasRectReads: 0,
            draftAttributeWrites: 0,
            svgElementsCreated: 0,
            replaceChildrenCalls: 0,
          };
          const originalCreateElementNS = Document.prototype.createElementNS;
          const originalReplaceChildren = Element.prototype.replaceChildren;
          const originalSetAttribute = Element.prototype.setAttribute;
          const originalGetBoundingClientRect = Element.prototype.getBoundingClientRect;
          Document.prototype.createElementNS = function patchedCreateElementNS(namespace, tagName) {
            if (namespace === "http://www.w3.org/2000/svg") {
              counters.svgElementsCreated += 1;
            }
            return originalCreateElementNS.call(this, namespace, tagName);
          };
          Element.prototype.replaceChildren = function patchedReplaceChildren(...nodes) {
            counters.replaceChildrenCalls += 1;
            return originalReplaceChildren.apply(this, nodes);
          };
          Element.prototype.setAttribute = function patchedSetAttribute(name, value) {
            if (
              this.closest?.(".annotation-draft") &&
              ["x1", "y1", "x2", "y2", "x", "y", "cx", "cy"].includes(name)
            ) {
              counters.draftAttributeWrites += 1;
            }
            return originalSetAttribute.call(this, name, value);
          };
          Element.prototype.getBoundingClientRect = function patchedGetBoundingClientRect() {
            if (this === document.querySelector("[data-viewer-canvas]")) {
              counters.canvasRectReads += 1;
            }
            return originalGetBoundingClientRect.call(this);
          };

          try {
            controller.mount(document, model);
            image.dispatchEvent(new Event("load"));
            const canvas = document.querySelector("[data-viewer-canvas]");
            if (!canvas) {
              throw new Error("Benchmark canvas missing.");
            }

            const pointerDownTarget =
              mode === "edit"
                ? document.querySelector(
                    "[data-annotation-handle][data-annotation-id='line-0'][data-endpoint='end']",
                  )
                : canvas;
            if (!pointerDownTarget) {
              throw new Error("Benchmark pointer target missing.");
            }
            pointerDownTarget.dispatchEvent(
              new PointerEvent("pointerdown", {
                bubbles: true,
                cancelable: true,
                button: 0,
                pointerId: 1,
                pointerType: "mouse",
                clientX: mode === "edit" ? 50 : 70,
                clientY: mode === "edit" ? 44 : 70,
              }),
            );

            counters.svgElementsCreated = 0;
            counters.replaceChildrenCalls = 0;
            counters.canvasRectReads = 0;
            counters.draftAttributeWrites = 0;
            const startedAt = performance.now();
            for (let index = 0; index < moveCount; index += 1) {
              canvas.dispatchEvent(
                new PointerEvent("pointermove", {
                  bubbles: true,
                  cancelable: true,
                  button: 0,
                  buttons: 1,
                  pointerId: 1,
                  pointerType: "mouse",
                  clientX: 72 + index * 2,
                  clientY: 76 + (index % 80),
                }),
              );
            }
            await new Promise((resolve) => requestAnimationFrame(() => resolve()));
            await new Promise((resolve) => requestAnimationFrame(() => resolve()));
            const elapsedMs = performance.now() - startedAt;
            return {
              elapsedMs,
              canvasRectReads: counters.canvasRectReads,
              draftAttributeWrites: counters.draftAttributeWrites,
              svgElementsCreated: counters.svgElementsCreated,
              replaceChildrenCalls: counters.replaceChildrenCalls,
              annotationNodes: document.querySelectorAll(".annotation-layer *").length,
            };
          } finally {
            Document.prototype.createElementNS = originalCreateElementNS;
            Element.prototype.replaceChildren = originalReplaceChildren;
            Element.prototype.setAttribute = originalSetAttribute;
            Element.prototype.getBoundingClientRect = originalGetBoundingClientRect;
            controller.detach();
          }
        },
        { annotationCount, mode, moveCount, previewUrl },
      ),
    );
    await page.close();
  }
} finally {
  await browser.close();
  await server.close();
}

const elapsedValues = samples.map((sample) => sample.elapsedMs);
const total = elapsedValues.reduce((sum, value) => sum + value, 0);
const average = total / elapsedValues.length;
const minimum = Math.min(...elapsedValues);
const maximum = Math.max(...elapsedValues);

console.log(
  JSON.stringify(
    {
      annotationCount,
      mode,
      moveCount,
      sampleCount,
      averageMs: average,
      minimumMs: minimum,
      maximumMs: maximum,
      samples,
    },
    null,
    2,
  ),
);
