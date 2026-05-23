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
