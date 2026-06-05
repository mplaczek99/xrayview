import { expect, type Page, test } from "@playwright/test";

function collectRuntimeErrors(page: Page): string[] {
  const runtimeErrors: string[] = [];
  page.on("pageerror", (error) => runtimeErrors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") {
      runtimeErrors.push(message.text());
    }
  });

  return runtimeErrors;
}

async function openMockStudy(page: Page) {
  await page.goto("/");
  await page.getByTestId("action-open-study").click();

  await expect(page.locator(".viewer-canvas__image")).toBeVisible();
  await expect(page.locator(".view-panel__filename")).toHaveText("1.bmp");
}

async function drawMeasurementLine(page: Page) {
  await page.getByTestId("action-tool-measure-line").click();
  const canvas = page.locator("[data-viewer-canvas]");
  await expect(canvas).toHaveClass(/viewer-canvas--measureLine/);

  const box = await canvas.boundingBox();
  expect(box).not.toBeNull();
  if (!box) {
    throw new Error("viewer canvas was not laid out");
  }

  const start = {
    x: box.x + box.width * 0.38,
    y: box.y + box.height * 0.42,
  };
  const end = {
    x: box.x + box.width * 0.57,
    y: box.y + box.height * 0.58,
  };

  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  await page.mouse.move(end.x, end.y, { steps: 6 });
  await page.mouse.up();

  const annotation = page.locator(".annotation-list__item", { hasText: "Measurement 1" });
  await expect(annotation).toBeVisible();
  return annotation;
}

test("opens and processes a BMP with current processing UI", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await page.goto("/");

  await expect(page.getByText("Open a bitewing X-ray (BMP)").first()).toBeVisible();
  await page.getByTestId("action-open-study").click();

  await expect(page.locator(".viewer-canvas__image")).toBeVisible();
  await expect(page.locator(".view-panel__filename")).toHaveText("1.bmp");

  await page.getByRole("tab", { name: "Processing" }).click();
  await page.getByTestId("action-start-process").click();

  await expect(page.locator(".run-status--success")).toContainText("Processing complete.");

  await expect(
    page.locator("[data-action=choose-output-path], [data-action=clear-output-path]"),
  ).toHaveCount(0);
  expect(runtimeErrors).toEqual([]);
});

test("shows an active animation while analysis is running", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await openMockStudy(page);
  await page.evaluate(() => {
    const stage = document.querySelector("[data-viewer-stage]");
    const viewerRemovals: string[] = [];
    const containsViewerStage = (node: Node) =>
      node === stage || (node instanceof Element && stage !== null && node.contains(stage));
    const recordViewerRemoval = (operation: string, node: Node) => {
      if (containsViewerStage(node)) {
        viewerRemovals.push(operation);
      }
    };

    (
      window as Window & {
        __xrayviewImageBeforeAnalyze?: Element | null;
        __xrayviewViewerRemovals?: string[];
      }
    ).__xrayviewImageBeforeAnalyze = document.querySelector("[data-viewer-image]");
    (
      window as Window & {
        __xrayviewViewerRemovals?: string[];
      }
    ).__xrayviewViewerRemovals = viewerRemovals;

    const originalRemoveChild = Node.prototype.removeChild;
    Node.prototype.removeChild = function <T extends Node>(child: T): T {
      recordViewerRemoval("removeChild", child);
      return originalRemoveChild.call(this, child) as T;
    };

    const originalReplaceChild = Node.prototype.replaceChild;
    Node.prototype.replaceChild = function <T extends Node>(node: Node, child: T): T {
      recordViewerRemoval("replaceChild", child);
      return originalReplaceChild.call(this, node, child) as T;
    };

    const originalReplaceChildren = Element.prototype.replaceChildren;
    Element.prototype.replaceChildren = function (...nodes: (Node | string)[]): void {
      recordViewerRemoval("replaceChildren", this);
      originalReplaceChildren.apply(this, nodes);
    };

    const originalReplaceWith = Element.prototype.replaceWith;
    Element.prototype.replaceWith = function (...nodes: (Node | string)[]): void {
      recordViewerRemoval("replaceWith", this);
      originalReplaceWith.apply(this, nodes);
    };

    const innerHtmlDescriptor = Object.getOwnPropertyDescriptor(Element.prototype, "innerHTML");
    if (innerHtmlDescriptor?.set) {
      Object.defineProperty(Element.prototype, "innerHTML", {
        ...innerHtmlDescriptor,
        set(value: string) {
          recordViewerRemoval("innerHTML", this);
          innerHtmlDescriptor.set?.call(this, value);
        },
      });
    }
  });

  await page.getByTestId("action-measure-teeth").click();

  await expect(page.getByTestId("analysis-progress")).toBeVisible();
  const pulseProbe = await page.evaluate(async () => {
    await new Promise((resolve) => setTimeout(resolve, 120));
    const pulse = document.querySelector(".analysis-progress__pulse");
    const badge = document.querySelector(".analysis-progress__badge");
    const buttonSpinner = document.querySelector(".analysis-button__spinner");
    const animation = pulse?.getAnimations()[0] ?? null;
    const buttonAnimation = buttonSpinner?.getAnimations()[0] ?? null;
    const before = Number(animation?.currentTime ?? Number.NaN);
    const buttonBefore = Number(buttonAnimation?.currentTime ?? Number.NaN);

    await new Promise((resolve) => setTimeout(resolve, 260));

    const afterPulse = document.querySelector(".analysis-progress__pulse");
    const afterBadge = document.querySelector(".analysis-progress__badge");
    const afterButtonSpinner = document.querySelector(".analysis-button__spinner");
    const afterAnimation = afterPulse?.getAnimations()[0] ?? null;
    const afterButtonAnimation = afterButtonSpinner?.getAnimations()[0] ?? null;

    return {
      animationAdvanced: Number(afterAnimation?.currentTime ?? Number.NaN) > before,
      buttonAnimationAdvanced:
        Number(afterButtonAnimation?.currentTime ?? Number.NaN) > buttonBefore,
      sameBadge: badge === afterBadge,
      sameButtonSpinner: buttonSpinner === afterButtonSpinner,
      samePulse: pulse === afterPulse,
    };
  });
  expect(pulseProbe).toEqual({
    animationAdvanced: true,
    buttonAnimationAdvanced: true,
    sameBadge: true,
    sameButtonSpinner: true,
    samePulse: true,
  });
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          document.querySelector("[data-viewer-image]") ===
          (
            window as Window & {
              __xrayviewImageBeforeAnalyze?: Element | null;
            }
          ).__xrayviewImageBeforeAnalyze,
      ),
    )
    .toBe(true);
  expect(
    await page.evaluate(
      () =>
        (
          window as Window & {
            __xrayviewViewerRemovals?: string[];
          }
        ).__xrayviewViewerRemovals ?? [],
    ),
  ).toEqual([]);
  await expect(page.locator(".analysis-progress__badge")).toBeVisible();
  await expect(page.locator(".analysis-progress__scanline")).toHaveCount(0);
  await expect(page.getByTestId("action-measure-teeth")).toContainText("Analyzing...");
  expect(runtimeErrors).toEqual([]);
});

test("draws, calibrates, clears, and deletes a line annotation", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await openMockStudy(page);

  const annotation = await drawMeasurementLine(page);
  await expect(page.locator(".annotation-layer__line--manual")).toHaveCount(1);
  await expect(annotation).toContainText(/px/);
  await expect(annotation).not.toContainText("mm");
  await expect(page.getByTestId("action-remove-annotation")).toBeEnabled();

  await page.locator("[data-calibration-length]").fill("10");
  await page.getByTestId("action-calibrate-study").click();

  await expect(annotation).toContainText("10.0 mm");
  await expect(annotation).toContainText(/px/);
  await expect(page.locator(".status-bar__text")).toContainText("Calibrated:");
  await expect(page.getByTestId("action-clear-calibration")).toBeVisible();

  await page.getByTestId("action-clear-calibration").click();
  await expect(annotation).toContainText(/px/);
  await expect(annotation).not.toContainText("mm");
  await expect(page.locator(".status-bar__text")).toContainText("Calibration cleared.");

  await page.getByTestId("action-remove-annotation").click();
  await expect(page.locator(".annotation-list__item")).toHaveCount(0);
  await expect(page.locator(".annotation-layer__line--manual")).toHaveCount(0);
  await expect(page.getByText("No manual line annotations yet.")).toBeVisible();
  expect(runtimeErrors).toEqual([]);
});

test("switches analysis overlay modes after analysis completes", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await openMockStudy(page);
  await page.getByTestId("action-measure-teeth").click();

  await expect(page.getByTestId("action-analysis-outline")).toBeVisible();
  const image = page.locator("[data-viewer-image]");
  const outlineSrc = await image.getAttribute("src");
  expect(outlineSrc).toBeTruthy();

  await page.getByTestId("action-analysis-sections").click();
  await expect(page.getByTestId("action-analysis-sections")).toHaveClass(
    /analysis-toggle__btn--active/,
  );
  const sectionsSrc = await image.getAttribute("src");
  expect(sectionsSrc).toBeTruthy();
  expect(sectionsSrc).not.toBe(outlineSrc);

  await page.getByTestId("action-analysis-outline").click();
  await expect(page.getByTestId("action-analysis-outline")).toHaveClass(
    /analysis-toggle__btn--active/,
  );
  await expect(image).toHaveAttribute("src", outlineSrc ?? "");
  expect(runtimeErrors).toEqual([]);
});

test("renders processing compare modes after a completed run", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await openMockStudy(page);
  await page.getByRole("tab", { name: "Processing" }).click();
  await page.getByTestId("action-start-process").click();
  await expect(page.locator(".run-status--success")).toContainText("Processing complete.");

  const previewImage = page.locator(".processing-tab__preview .viewer-stage__image");
  await expect(previewImage).toHaveCount(1);
  const processedSrc = await previewImage.getAttribute("src");
  expect(processedSrc).toBeTruthy();

  await page.getByRole("button", { name: "Original" }).click();
  await expect(page.locator(".compare-toggle__btn--active")).toHaveText("Original");
  await expect(previewImage).toHaveCount(1);
  const originalSrc = await previewImage.getAttribute("src");
  expect(originalSrc).toBeTruthy();
  expect(originalSrc).not.toBe(processedSrc);

  await page.getByRole("button", { name: "Split" }).click();
  await expect(page.locator(".compare-toggle__btn--active")).toHaveText("Split");
  await expect(page.locator(".compare-split")).toBeVisible();
  await expect(page.locator(".compare-split__label")).toHaveText(["Original", "Processed"]);
  await expect(page.locator(".compare-split .viewer-stage__image")).toHaveCount(2);
  expect(runtimeErrors).toEqual([]);
});

test("cancels an in-flight processing job", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await openMockStudy(page);
  await page.getByRole("tab", { name: "Processing" }).click();
  await page.getByTestId("action-start-process").click();
  await page.getByTestId("action-cancel-process").click();

  await expect(page.locator(".run-status--error")).toContainText("Processing cancelled.");
  await expect(page.locator(".status-bar__text")).toContainText("Processing cancelled.");
  await expect(page.getByTestId("action-start-process")).toBeEnabled();
  expect(runtimeErrors).toEqual([]);
});

test("shows a viewer fallback when a preview image fails to load", async ({ page }) => {
  await openMockStudy(page);

  await page.locator("[data-viewer-image]").evaluate((image) => {
    if (!(image instanceof HTMLImageElement)) {
      throw new Error("viewer image was not an HTML image");
    }
    image.src = "/missing-preview.bmp";
  });

  await expect(page.getByText("Preview Unavailable")).toBeVisible();
  await expect(
    page.getByText("The rendered preview file could not be loaded by the desktop webview."),
  ).toBeVisible();
});

test("resets the viewer after zooming", async ({ page }) => {
  const runtimeErrors = collectRuntimeErrors(page);

  await openMockStudy(page);

  const canvas = page.locator("[data-viewer-canvas]");
  const box = await canvas.boundingBox();
  expect(box).not.toBeNull();
  if (!box) {
    throw new Error("viewer canvas was not laid out");
  }

  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.wheel(0, -360);
  await expect(page.locator("[data-viewer-zoom]")).not.toHaveText("100%");

  await page.getByRole("button", { name: "Reset view" }).click();
  await expect(page.locator("[data-viewer-zoom]")).toHaveText("100%");
  expect(runtimeErrors).toEqual([]);
});
