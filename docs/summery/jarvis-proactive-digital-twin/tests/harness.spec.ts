import { expect, test } from "@playwright/test";

type RegistryScene = {
  id: string;
  title: string;
  section: string;
  beats: number;
  profile: string;
};

async function registry(page: Parameters<typeof test>[0] extends never ? never : any): Promise<RegistryScene[]> {
  await page.goto("/?scene=opening&beat=0&snapshot=1");
  return page.evaluate(() => window.__DECK_REGISTRY__);
}

test("registry and every stable scene/beat route render without runtime errors", async ({ page }) => {
  const runtimeErrors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") runtimeErrors.push(message.text());
  });
  page.on("pageerror", (error) => runtimeErrors.push(error.message));

  const scenes = await registry(page);
  expect(scenes).toHaveLength(18);

  for (const scene of scenes) {
    for (let beat = 0; beat < scene.beats; beat += 1) {
      await page.goto(`/?scene=${scene.id}&beat=${beat}&snapshot=1`);
      const stage = page.locator(".stage");
      await expect(stage).toHaveAttribute("data-scene-id", scene.id);
      await expect(stage).toHaveAttribute("data-beat", String(beat));
      await expect(stage).toHaveAttribute("data-profile", scene.profile);
      await expect(stage.locator("[data-audit]")).toHaveCount(1);
      await expect(page.locator(".stage-viewport")).toHaveAttribute("data-deck-ready", "true");
    }
  }

  expect(runtimeErrors).toEqual([]);
});

test("invalid scene ids fail loudly", async ({ page }) => {
  await page.goto("/?scene=does-not-exist&snapshot=1");
  await expect(page.locator("[data-route-error]"))
    .toHaveAttribute("data-route-error", "does-not-exist");
  await expect(page.getByText("找不到场景：does-not-exist")).toBeVisible();
});

test("keyboard advances beats, crosses scenes, and reverses to the prior final beat", async ({ page }) => {
  await page.goto("/?scene=opening&beat=0&snapshot=1");
  const stage = page.locator(".stage");

  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-scene-id", "opening");
  await expect(stage).toHaveAttribute("data-beat", "1");

  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-scene-id", "input-shift");
  await expect(stage).toHaveAttribute("data-beat", "0");

  await page.keyboard.press("ArrowLeft");
  await expect(stage).toHaveAttribute("data-scene-id", "opening");
  await expect(stage).toHaveAttribute("data-beat", "1");
});

test("stage click advances while navigator controls isolate interaction", async ({ page }) => {
  await page.goto("/?scene=opening&beat=0&snapshot=1");
  const stage = page.locator(".stage");

  await page.mouse.click(800, 150);
  await expect(stage).toHaveAttribute("data-beat", "1");

  const target = page.getByRole("button", { name: "跳转到：六千条消息之后" });
  await target.click();
  await expect(stage).toHaveAttribute("data-scene-id", "evidence-funnel");
  await expect(stage).toHaveAttribute("data-beat", "0");

  await target.focus();
  await page.keyboard.press("ArrowRight");
  await expect(stage).toHaveAttribute("data-beat", "0");
});

test("pointer swipe moves between beats and suppresses the synthetic click", async ({ page }) => {
  await page.goto("/?scene=input-shift&beat=0&snapshot=1");
  const stage = page.locator(".stage");
  const viewport = page.locator(".stage-viewport");

  await viewport.dispatchEvent("pointerdown", { pointerId: 1, pointerType: "touch", clientX: 1200, clientY: 500 });
  await viewport.dispatchEvent("pointerup", { pointerId: 1, pointerType: "touch", clientX: 900, clientY: 500 });
  await viewport.dispatchEvent("click", { clientX: 900, clientY: 500 });
  await expect(stage).toHaveAttribute("data-beat", "1");
});

test("print route contains every scene at its final beat", async ({ page }) => {
  await page.goto("/?print=1&snapshot=1");
  const scenes = await page.evaluate(() => window.__DECK_REGISTRY__);
  const printed = page.locator("[data-print-deck] .print-page > .print-transform > .stage");
  await expect(printed).toHaveCount(scenes.length);
  for (let index = 0; index < scenes.length; index += 1) {
    await expect(printed.nth(index)).toHaveAttribute("data-scene-id", scenes[index].id);
    await expect(printed.nth(index)).toHaveAttribute("data-beat", String(scenes[index].beats - 1));
  }
});

test("mobile viewport preserves the 16:9 stage and readable route state", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/?scene=pipeline&beat=3&snapshot=1");
  const frame = await page.locator(".stage-frame").boundingBox();
  expect(frame).not.toBeNull();
  expect(frame!.width / frame!.height).toBeCloseTo(16 / 9, 2);
  await expect(page.locator(".stage")).toHaveAttribute("data-scene-id", "pipeline");
});
