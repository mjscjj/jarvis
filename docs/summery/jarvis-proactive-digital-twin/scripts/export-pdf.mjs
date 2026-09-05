import { chromium } from "@playwright/test";
import { PDFDocument } from "pdf-lib";
import { preview } from "vite";
import { mkdir, writeFile } from "node:fs/promises";
import { resolve } from "node:path";

const root = resolve(import.meta.dirname, "..");
const outputDir = resolve(root, "artifacts");
const outputPath = resolve(outputDir, "jarvis-proactive-digital-twin.pdf");

await mkdir(outputDir, { recursive: true });

const server = await preview({
  root,
  preview: { host: "127.0.0.1", port: 4177, strictPort: true },
});

const browser = await chromium.launch();
try {
  const page = await browser.newPage({ viewport: { width: 1920, height: 1080 } });
  const runtimeErrors = [];
  page.on("console", (message) => {
    if (message.type() === "error") runtimeErrors.push(message.text());
  });
  page.on("pageerror", (error) => runtimeErrors.push(error.message));

  await page.goto("http://127.0.0.1:4177/?scene=opening&beat=0&snapshot=1", { waitUntil: "networkidle" });
  await page.evaluate(() => document.fonts.ready);
  const scenes = await page.evaluate(() => window.__DECK_REGISTRY__);
  if (scenes.length !== 18) throw new Error(`expected 18 scenes, got ${scenes.length}`);

  const pdf = await PDFDocument.create();
  for (const scene of scenes) {
    const beat = scene.beats - 1;
    await page.goto(`http://127.0.0.1:4177/?scene=${scene.id}&beat=${beat}&snapshot=1`, { waitUntil: "networkidle" });
    await page.evaluate(() => document.fonts.ready);
    const stage = page.locator(".stage");
    const actualId = await stage.getAttribute("data-scene-id");
    const actualBeat = await stage.getAttribute("data-beat");
    if (actualId !== scene.id || actualBeat !== String(beat)) {
      throw new Error(`unstable frame for ${scene.id}: got ${actualId}/${actualBeat}`);
    }
    const png = await stage.screenshot({ animations: "disabled", type: "png" });
    const image = await pdf.embedPng(png);
    const pdfPage = pdf.addPage([960, 540]);
    pdfPage.drawImage(image, { x: 0, y: 0, width: 960, height: 540 });
  }

  if (runtimeErrors.length > 0) throw new Error(`runtime errors during PDF export:\n${runtimeErrors.join("\n")}`);
  await writeFile(outputPath, await pdf.save({ useObjectStreams: false }));
  process.stdout.write(`${outputPath}\n`);
} finally {
  await browser.close();
  await new Promise((resolveClose, rejectClose) => {
    server.httpServer.close((error) => error ? rejectClose(error) : resolveClose());
  });
}
