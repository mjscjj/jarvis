import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { chromium } from "playwright";
import { PDFDocument } from "pdf-lib";

const deckUrl = process.env.DECK_URL ?? "http://127.0.0.1:4187";
const outputPath = resolve("jarvis-world-model-maintenance.pdf");
const browser = await chromium.launch();

try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 720 } });
  await page.goto(`${deckUrl}/?print=1&snapshot=1`, { waitUntil: "networkidle" });
  await page.emulateMedia({ media: "print" });
  await page.pdf({
    path: outputPath,
    printBackground: true,
    preferCSSPageSize: true,
    margin: { top: "0", right: "0", bottom: "0", left: "0" },
  });

  const pdf = await PDFDocument.load(await readFile(outputPath));
  const pageCount = pdf.getPageCount();
  if (pageCount !== 16) throw new Error(`Expected 16 PDF pages, got ${pageCount}`);
  console.log(`Exported ${pageCount} pages to ${outputPath}`);
} finally {
  await browser.close();
}
