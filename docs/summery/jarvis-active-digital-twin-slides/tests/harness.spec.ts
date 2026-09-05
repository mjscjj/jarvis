import { expect, test, type Page } from "@playwright/test";

type RegistryScene = { id: string; title: string; section: string; beats: number; profile: string; theme: string };
const expectedIds = [
  "opening", "origin-loop", "active-not-chatbot", "capability-map", "code-loop",
  "ambient-feishu", "collaboration-proof", "backstage", "decision-problem", "system-core",
  "rag-vs-world", "world-anatomy", "fact-compile", "context-disclosure", "decision-pipeline",
  "clear-brain", "ownership", "safe-action", "generic-horizon", "closing",
];
const expectedBeats = [3, 4, 3, 4, 4, 3, 4, 4, 3, 4, 3, 4, 5, 4, 5, 4, 4, 5, 4, 3];

async function registry(page: Page): Promise<RegistryScene[]> {
  await page.goto("/?scene=system-core&beat=0&snapshot=1");
  await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
  return page.evaluate(() => window.__DECK_REGISTRY__);
}

test("registry exposes the complete twenty-scene article deck", async ({ page }) => {
  const scenes = await registry(page);
  expect(scenes.map(({ id }) => id)).toEqual(expectedIds);
  expect(scenes.map(({ beats }) => beats)).toEqual(expectedBeats);
  expect(scenes.every(({ theme }) => theme === "whiteboard")).toBe(true);
});

test("every scene and beat renders without runtime or asset errors", async ({ page }) => {
  const errors: string[] = [];
  page.on("console", (message) => { if (message.type() === "error") errors.push(message.text()); });
  page.on("pageerror", (error) => errors.push(error.message));
  page.on("requestfailed", (request) => errors.push(`${request.url()} :: ${request.failure()?.errorText}`));

  for (const scene of await registry(page)) {
    for (let beat = 0; beat < scene.beats; beat += 1) {
      await page.goto(`/?scene=${scene.id}&beat=${beat}&snapshot=1`);
      const stage = page.locator(".stage");
      await expect(stage).toHaveAttribute("data-scene-id", scene.id);
      await expect(stage).toHaveAttribute("data-beat", String(beat));
      await expect(stage).toHaveAttribute("data-profile", scene.profile);
      await expect(stage.locator("[data-audit]")).toHaveCount(1);
    }
  }
  expect(errors).toEqual([]);
});

test("invalid ids fail loudly and beat routes clamp", async ({ page }) => {
  await page.goto("/?scene=missing&snapshot=1");
  await expect(page.locator("[data-route-error]")).toHaveAttribute("data-route-error", "missing");
  await page.goto("/?scene=decision-pipeline&beat=99&snapshot=1");
  await expect(page.locator(".stage")).toHaveAttribute("data-beat", "4");
});

test("keyboard navigation crosses preview boundaries one frame at a time", async ({ page }) => {
  await page.goto("/?scene=system-core&beat=2&snapshot=1");
  const stage = page.locator(".stage");
  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-beat", "3");
  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-scene-id", "rag-vs-world");
  await expect(stage).toHaveAttribute("data-beat", "0");
  await page.keyboard.press("ArrowLeft");
  await expect(stage).toHaveAttribute("data-scene-id", "system-core");
  await expect(stage).toHaveAttribute("data-beat", "3");
});

test("record interaction stays local to the world model scene", async ({ page }) => {
  await page.goto("/?scene=world-anatomy&beat=3&snapshot=1");
  const stage = page.locator(".stage");
  const fact = page.getByRole("button", { name: "查看认知记录：Fact" });
  await fact.hover();
  expect(await fact.evaluate((element) => getComputedStyle(element).transform)).not.toBe("none");
  await fact.click();
  await expect(fact).toHaveClass(/active/);
  await expect(stage).toHaveAttribute("data-beat", "3");
  await fact.focus();
  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-beat", "3");
});

test("empty click and swipe advance exactly one frame", async ({ page }) => {
  await page.goto("/?scene=decision-pipeline&beat=0&snapshot=1");
  const stage = page.locator(".stage");
  const viewport = page.locator(".stage-viewport");
  const box = await stage.boundingBox();
  expect(box).not.toBeNull();
  await page.mouse.click(box!.x + box!.width * .5, box!.y + box!.height * .91);
  await expect(stage).toHaveAttribute("data-beat", "1");
  await viewport.dispatchEvent("pointerdown", { pointerId: 1, pointerType: "touch", clientX: 1200, clientY: 500 });
  await viewport.dispatchEvent("pointerup", { pointerId: 1, pointerType: "touch", clientX: 900, clientY: 500 });
  await viewport.dispatchEvent("click", { clientX: 900, clientY: 500 });
  await expect(stage).toHaveAttribute("data-beat", "2");
});

test("fixed 16:9 stage remains visible on mobile", async ({ page }) => {
  await page.goto("/?scene=world-anatomy&beat=3&snapshot=1");
  expect(await page.locator(".stage").evaluate((stage) => ({ width: getComputedStyle(stage).width, height: getComputedStyle(stage).height }))).toEqual({ width: "1920px", height: "1080px" });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  const frame = await page.locator(".stage-frame").boundingBox();
  expect(frame).not.toBeNull();
  expect(frame!.width / frame!.height).toBeCloseTo(16 / 9, 2);
  expect(frame!.x).toBeGreaterThanOrEqual(-1);
  expect(frame!.x + frame!.width).toBeLessThanOrEqual(391);
});

test("fonts load and print mode contains all settled previews", async ({ page }) => {
  await page.goto("/?scene=system-core&beat=3&snapshot=1");
  await page.evaluate(() => document.fonts.load('500 32px "Noto Serif SC"', "状态与决策"));
  expect(await page.evaluate(() => ({
    sans: document.fonts.check('32px "Noto Sans SC"'),
    serif: document.fonts.check('500 32px "Noto Serif SC"'),
    mono: document.fonts.check('20px "JetBrains Mono Variable"'),
  }))).toEqual({ sans: true, serif: true, mono: true });
  await page.goto("/?print=1&snapshot=1");
  const printed = page.locator("[data-print-deck] .print-page .stage");
  await expect(printed).toHaveCount(expectedIds.length);
  for (let index = 0; index < expectedIds.length; index += 1) await expect(printed.nth(index)).toHaveAttribute("data-scene-id", expectedIds[index]);
});
