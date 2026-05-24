import { expect, test } from "@playwright/test";

test("opens and processes a BMP without legacy output UI", async ({ page }) => {
  const runtimeErrors: string[] = [];
  page.on("pageerror", (error) => runtimeErrors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") {
      runtimeErrors.push(message.text());
    }
  });

  await page.goto("/");

  await expect(page.getByText("Open a bitewing X-ray (BMP)").first()).toBeVisible();
  await page.getByTestId("action-open-study").click();

  await expect(page.locator(".viewer-canvas__image")).toBeVisible();
  await expect(page.locator(".view-panel__filename")).toHaveText("1.bmp");

  await page.getByRole("tab", { name: "Processing" }).click();
  await page.getByTestId("action-start-process").click();

  await expect(page.locator(".run-status--success")).toContainText("Processing complete.");

  const legacyPattern = new RegExp(
    ["di" + "com", "ti" + "ff", "\\.d" + "cm\\b", "\\.t" + "if\\b"].join("|"),
    "i",
  );
  const bodyText = await page.locator("body").textContent();
  expect(bodyText ?? "").not.toMatch(legacyPattern);
  await expect(
    page.locator("[data-action=choose-output-path], [data-action=clear-output-path]"),
  ).toHaveCount(0);
  expect(runtimeErrors).toEqual([]);
});

test("shows an active animation while analysis is running", async ({ page }) => {
  const runtimeErrors: string[] = [];
  page.on("pageerror", (error) => runtimeErrors.push(error.message));
  page.on("console", (message) => {
    if (message.type() === "error") {
      runtimeErrors.push(message.text());
    }
  });

  await page.goto("/");
  await page.getByTestId("action-open-study").click();
  await expect(page.locator(".viewer-canvas__image")).toBeVisible();
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
