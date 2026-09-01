import { expect, test } from "@playwright/test";
import { mkdirSync } from "node:fs";

const previews: Array<[string, number]> = [
  ["opening", 2], ["origin-loop", 3], ["active-not-chatbot", 2], ["capability-map", 3],
  ["code-loop", 3], ["ambient-feishu", 2], ["collaboration-proof", 3], ["backstage", 3],
  ["decision-problem", 2], ["system-core", 3], ["rag-vs-world", 2], ["world-anatomy", 3],
  ["fact-compile", 4], ["context-disclosure", 3], ["decision-pipeline", 4], ["clear-brain", 3],
  ["ownership", 3], ["safe-action", 4], ["generic-horizon", 3], ["closing", 2],
];

test.beforeAll(() => mkdirSync("docs/previews", { recursive: true }));

for (const [sceneId, beat] of previews) {
  test(`${sceneId} is bounded and visually stable`, async ({ page }) => {
    await page.goto(`/?scene=${sceneId}&beat=${beat}&snapshot=1`);
    await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
    const geometry = await page.locator(".stage").evaluate((stage) => {
      const canvasElement = stage.querySelector<HTMLElement>(".scene-canvas")!;
      const auditElement = stage.querySelector<HTMLElement>("[data-audit]")!;
      const canvas = canvasElement.getBoundingClientRect();
      const audit = auditElement.getBoundingClientRect();
      return {
        canvas: { left: canvas.left, top: canvas.top, right: canvas.right, bottom: canvas.bottom },
        audit: { left: audit.left, top: audit.top, right: audit.right, bottom: audit.bottom },
        canvasOverflow: { x: canvasElement.scrollWidth - canvasElement.clientWidth, y: canvasElement.scrollHeight - canvasElement.clientHeight },
        auditOverflow: { x: auditElement.scrollWidth - auditElement.clientWidth, y: auditElement.scrollHeight - auditElement.clientHeight },
      };
    });
    expect(geometry.audit.left).toBeGreaterThanOrEqual(geometry.canvas.left - 2);
    expect(geometry.audit.top).toBeGreaterThanOrEqual(geometry.canvas.top - 2);
    expect(geometry.audit.right).toBeLessThanOrEqual(geometry.canvas.right + 2);
    expect(geometry.audit.bottom).toBeLessThanOrEqual(geometry.canvas.bottom + 2);
    expect(geometry.canvasOverflow.x).toBeLessThanOrEqual(2);
    expect(geometry.canvasOverflow.y).toBeLessThanOrEqual(2);
    expect(geometry.auditOverflow.x).toBeLessThanOrEqual(2);
    expect(geometry.auditOverflow.y).toBeLessThanOrEqual(2);
    await page.locator(".stage").screenshot({ path: `docs/previews/${sceneId}.png` });
    await expect(page.locator(".stage")).toHaveScreenshot(`${sceneId}.png`, { animations: "disabled", maxDiffPixelRatio: .006 });
  });
}
