import { expect, test, type Page } from "@playwright/test";

type RegistryScene = {
  id: string;
  title: string;
  section: string;
  beats: number;
  profile: string;
  theme: string;
};

const expectedSceneIds = [
  "opening",
  "rag-breaks",
  "world-formula",
  "two-worlds",
  "entity-atlas",
  "concrete-example",
  "relations",
  "cognition-records",
  "action-model",
  "evidence-sources",
  "compile-loop",
  "semantic-outcomes",
  "reliability",
  "maintainers",
  "consumption",
  "closing",
];

async function readRegistry(page: Page): Promise<RegistryScene[]> {
  await page.goto("/?scene=opening&beat=0&snapshot=1");
  await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
  return page.evaluate(() => window.__DECK_REGISTRY__);
}

test("registry exposes the confirmed 16-scene whiteboard narrative", async ({ page }) => {
  const scenes = await readRegistry(page);
  expect(scenes.map(({ id }) => id)).toEqual(expectedSceneIds);
  expect(scenes.every(({ theme }) => theme === "whiteboard")).toBe(true);
  expect(scenes.every(({ beats }) => beats >= 3 && beats <= 5)).toBe(true);
});

test("every stable scene and beat route renders without runtime or asset errors", async ({ page }) => {
  const runtimeErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") runtimeErrors.push(message.text());
  });
  page.on("pageerror", (error) => runtimeErrors.push(error.message));
  page.on("requestfailed", (request) => runtimeErrors.push(`${request.method()} ${request.url()} :: ${request.failure()?.errorText}`));

  const scenes = await readRegistry(page);
  for (const scene of scenes) {
    for (let beat = 0; beat < scene.beats; beat += 1) {
      await page.goto(`/?scene=${scene.id}&beat=${beat}&snapshot=1`);
      const stage = page.locator(".stage");
      await expect(stage).toHaveAttribute("data-scene-id", scene.id);
      await expect(stage).toHaveAttribute("data-beat", String(beat));
      await expect(stage).toHaveAttribute("data-profile", scene.profile);
      await expect(stage).toHaveAttribute("data-theme", scene.theme);
      await expect(stage.locator("[data-audit]")).toHaveCount(1);
      await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
    }
  }

  expect(runtimeErrors).toEqual([]);
});

test("invalid scene ids fail loudly and oversized beats clamp", async ({ page }) => {
  await page.goto("/?scene=does-not-exist&snapshot=1");
  await expect(page.locator("[data-route-error]")).toHaveAttribute("data-route-error", "does-not-exist");
  await expect(page.getByText("找不到场景：does-not-exist")).toBeVisible();

  await page.goto("/?scene=compile-loop&beat=99&snapshot=1");
  await expect(page.locator(".stage")).toHaveAttribute("data-beat", "4");
});

test("keyboard navigation advances one frame and crosses scene boundaries", async ({ page }) => {
  await page.goto("/?scene=opening&beat=1&snapshot=1");
  const stage = page.locator(".stage");

  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-scene-id", "opening");
  await expect(stage).toHaveAttribute("data-beat", "2");

  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-scene-id", "rag-breaks");
  await expect(stage).toHaveAttribute("data-beat", "0");

  await page.keyboard.press("ArrowLeft");
  await expect(stage).toHaveAttribute("data-scene-id", "opening");
  await expect(stage).toHaveAttribute("data-beat", "2");
});

test("local entity interaction does not leak into deck navigation", async ({ page }) => {
  await page.goto("/?scene=entity-atlas&beat=3&snapshot=1");
  const stage = page.locator(".stage");
  const person = page.getByRole("button", { name: "查看实体：Person / 人" });

  await person.hover();
  const hoverTransform = await person.evaluate((element) => getComputedStyle(element).transform);
  expect(hoverTransform).not.toBe("none");
  await person.click();
  await expect(person).toHaveClass(/active/);
  await expect(stage).toHaveAttribute("data-beat", "3");

  await person.focus();
  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-beat", "3");
});

test("empty stage click and pointer swipe each advance one frame", async ({ page }) => {
  await page.goto("/?scene=two-worlds&beat=0&snapshot=1");
  const stage = page.locator(".stage");
  const viewport = page.locator(".stage-viewport");

  const stageBox = await stage.boundingBox();
  expect(stageBox).not.toBeNull();
  await page.mouse.click(stageBox!.x + stageBox!.width * 0.5, stageBox!.y + stageBox!.height * 0.91);
  await expect(stage).toHaveAttribute("data-beat", "1");

  await viewport.dispatchEvent("pointerdown", { pointerId: 1, pointerType: "touch", clientX: 1200, clientY: 500 });
  await viewport.dispatchEvent("pointerup", { pointerId: 1, pointerType: "touch", clientX: 900, clientY: 500 });
  await viewport.dispatchEvent("click", { clientX: 900, clientY: 500 });
  await expect(stage).toHaveAttribute("data-beat", "2");
});

test("fixed stage scales as one 16:9 surface and stays visible on mobile", async ({ page }) => {
  await page.goto("/?scene=compile-loop&beat=4&snapshot=1");
  const baseSize = await page.locator(".stage").evaluate((stage) => ({
    width: getComputedStyle(stage).width,
    height: getComputedStyle(stage).height,
  }));
  expect(baseSize).toEqual({ width: "1920px", height: "1080px" });

  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
  const frame = await page.locator(".stage-frame").boundingBox();
  expect(frame).not.toBeNull();
  expect(frame!.width / frame!.height).toBeCloseTo(16 / 9, 2);
  expect(frame!.width).toBeGreaterThan(380);
  expect(frame!.x).toBeGreaterThanOrEqual(-1);
  expect(frame!.x + frame!.width).toBeLessThanOrEqual(391);
});

test("delivery fonts load and print mode contains all 16 final frames", async ({ page }) => {
  await page.goto("/?scene=closing&beat=2&snapshot=1");
  await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
  await page.evaluate(() => document.fonts.load('500 32px "Noto Serif SC"', "世界模型"));
  const fonts = await page.evaluate(() => ({
    sans: document.fonts.check('32px "Noto Sans SC"'),
    serif: document.fonts.check('500 32px "Noto Serif SC"'),
    mono: document.fonts.check('20px "JetBrains Mono Variable"'),
  }));
  expect(fonts).toEqual({ sans: true, serif: true, mono: true });

  await page.goto("/?print=1&snapshot=1");
  const printed = page.locator("[data-print-deck] .print-page > .print-transform > .stage");
  await expect(printed).toHaveCount(16);
  for (let index = 0; index < expectedSceneIds.length; index += 1) {
    await expect(printed.nth(index)).toHaveAttribute("data-scene-id", expectedSceneIds[index]);
    const scene = (await page.evaluate(() => window.__DECK_REGISTRY__))[index];
    await expect(printed.nth(index)).toHaveAttribute("data-beat", String(scene.beats - 1));
  }
});

test("architecture boundaries are stated directly", async ({ page }) => {
  await page.goto("/?scene=relations&beat=3&snapshot=1");
  await expect(page.getByText("没有独立 RelationFact 表", { exact: false })).toBeVisible();

  await page.goto("/?scene=maintainers&beat=3&snapshot=1");
  await expect(page.getByText("自动主维护者", { exact: false })).toBeVisible();
  await expect(page.getByText("辅助补写者", { exact: false })).toBeVisible();
});
