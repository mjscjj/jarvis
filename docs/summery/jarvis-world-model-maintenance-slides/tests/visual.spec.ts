import { expect, test } from "@playwright/test";
import { mkdirSync } from "node:fs";

const allScenes: Array<[string, number]> = [
  ["opening", 2], ["rag-breaks", 3], ["world-formula", 3], ["two-worlds", 3],
  ["entity-atlas", 3], ["concrete-example", 3], ["relations", 3], ["cognition-records", 3],
  ["action-model", 3], ["evidence-sources", 3], ["compile-loop", 4], ["semantic-outcomes", 3],
  ["reliability", 4], ["maintainers", 3], ["consumption", 3], ["closing", 2],
];

const baselineScenes = new Set(["opening", "two-worlds", "entity-atlas", "compile-loop", "reliability", "closing"]);

test.beforeAll(() => {
  mkdirSync("artifacts/rendered", { recursive: true });
  mkdirSync("artifacts/mobile", { recursive: true });
});

for (const [sceneId, beat] of allScenes) {
  test(`${sceneId} final frame is bounded and visually valid`, async ({ page }) => {
    await page.goto(`/?scene=${sceneId}&beat=${beat}&snapshot=1`);
    await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");

    const geometry = await page.locator(".stage").evaluate((stage) => {
      const stageRect = stage.getBoundingClientRect();
      const canvasElement = stage.querySelector<HTMLElement>(".scene-canvas")!;
      const auditElement = stage.querySelector<HTMLElement>("[data-audit]")!;
      const canvas = canvasElement.getBoundingClientRect();
      const audit = auditElement.getBoundingClientRect();
      return {
        stage: { width: stageRect.width, height: stageRect.height },
        canvas: { left: canvas.left, top: canvas.top, right: canvas.right, bottom: canvas.bottom },
        audit: { left: audit.left, top: audit.top, right: audit.right, bottom: audit.bottom },
        canvasOverflow: { x: canvasElement.scrollWidth - canvasElement.clientWidth, y: canvasElement.scrollHeight - canvasElement.clientHeight },
        auditOverflow: { x: auditElement.scrollWidth - auditElement.clientWidth, y: auditElement.scrollHeight - auditElement.clientHeight },
      };
    });

    expect(geometry.stage.width / geometry.stage.height).toBeCloseTo(16 / 9, 3);
    expect(geometry.audit.left).toBeGreaterThanOrEqual(geometry.canvas.left - 2);
    expect(geometry.audit.top).toBeGreaterThanOrEqual(geometry.canvas.top - 2);
    expect(geometry.audit.right).toBeLessThanOrEqual(geometry.canvas.right + 2);
    expect(geometry.audit.bottom).toBeLessThanOrEqual(geometry.canvas.bottom + 2);
    expect(geometry.canvasOverflow.x).toBeLessThanOrEqual(2);
    expect(geometry.canvasOverflow.y).toBeLessThanOrEqual(2);
    expect(geometry.auditOverflow.x).toBeLessThanOrEqual(2);
    expect(geometry.auditOverflow.y).toBeLessThanOrEqual(2);

    await page.locator(".stage").screenshot({ path: `artifacts/rendered/${String(allScenes.findIndex(([id]) => id === sceneId) + 1).padStart(2, "0")}-${sceneId}.png` });
    if (baselineScenes.has(sceneId)) {
      await expect(page.locator(".stage")).toHaveScreenshot(`${sceneId}.png`, {
        animations: "disabled",
        maxDiffPixelRatio: 0.006,
      });
    }
  });
}

test("mobile viewport keeps the fixed deck legible as a single surface", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/?scene=compile-loop&beat=4&snapshot=1");
  await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
  await page.screenshot({ path: "artifacts/mobile/compile-loop.png", fullPage: true });
});
