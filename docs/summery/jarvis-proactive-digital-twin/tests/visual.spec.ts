import { expect, test } from "@playwright/test";
import { mkdirSync } from "node:fs";

const baselineScenes = ["opening", "evidence-funnel", "pipeline", "approval", "proof", "closing"];

test.beforeAll(() => mkdirSync("artifacts/rendered", { recursive: true }));

test("final beats stay inside the stage and emit a full render contact sheet", async ({ page }) => {
  await page.goto("/?scene=opening&beat=0&snapshot=1");
  const scenes = await page.evaluate(() => window.__DECK_REGISTRY__);

  for (const [index, scene] of scenes.entries()) {
    await page.goto(`/?scene=${scene.id}&beat=${scene.beats - 1}&snapshot=1`);
    await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");

    const geometry = await page.locator(".stage").evaluate((stage) => {
      const stageRect = stage.getBoundingClientRect();
      const canvas = stage.querySelector(".scene-canvas")!.getBoundingClientRect();
      const audit = stage.querySelector("[data-audit]")!.getBoundingClientRect();
      return {
        stage: { width: stageRect.width, height: stageRect.height },
        canvas: { left: canvas.left, top: canvas.top, right: canvas.right, bottom: canvas.bottom },
        audit: { left: audit.left, top: audit.top, right: audit.right, bottom: audit.bottom },
      };
    });

    expect(geometry.stage.width / geometry.stage.height).toBeCloseTo(16 / 9, 3);
    expect(geometry.audit.left).toBeGreaterThanOrEqual(geometry.canvas.left - 2);
    expect(geometry.audit.top).toBeGreaterThanOrEqual(geometry.canvas.top - 2);
    expect(geometry.audit.right).toBeLessThanOrEqual(geometry.canvas.right + 2);
    expect(geometry.audit.bottom).toBeLessThanOrEqual(geometry.canvas.bottom + 2);

    const filename = `${String(index + 1).padStart(2, "0")}-${scene.id}.png`;
    await page.locator(".stage").screenshot({ path: `artifacts/rendered/${filename}` });
  }
});

for (const sceneId of baselineScenes) {
  test(`${sceneId} visual baseline`, async ({ page }) => {
    await page.goto(`/?scene=${sceneId}&beat=99&snapshot=1`);
    await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
    await expect(page.locator(".stage")).toHaveScreenshot(`${sceneId}.png`, {
      animations: "disabled",
      maxDiffPixelRatio: 0.006,
    });
  });
}
