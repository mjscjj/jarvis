/* Geometry and semantic checks for architecture.html.
 * Run with NODE_PATH pointing to a directory that contains puppeteer-core.
 */
const path = require('path');
const puppeteer = require('puppeteer-core');

(async () => {
  const htmlPath = path.resolve(process.argv[2] || 'architecture.html');
  const browser = await puppeteer.launch({
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage']
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1480, height: 920, deviceScaleFactor: 1 });
  await page.goto(`file://${htmlPath}`, { waitUntil: 'networkidle0' });
  await new Promise(resolve => setTimeout(resolve, 600));

  const report = await page.evaluate(() => {
    const stage = document.getElementById('stage');
    const sr = stage.getBoundingClientRect();
    const boxes = Array.from(document.querySelectorAll('.node')).map(node => {
      const r = node.getBoundingClientRect();
      return {
        id: node.dataset.id,
        left: r.left - sr.left,
        top: r.top - sr.top,
        right: r.right - sr.left,
        bottom: r.bottom - sr.top,
        width: r.width,
        height: r.height
      };
    });

    const overlaps = [];
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = boxes[i], b = boxes[j];
        const w = Math.min(a.right, b.right) - Math.max(a.left, b.left);
        const h = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
        if (w > 0.5 && h > 0.5) overlaps.push(`${a.id}<->${b.id} (${w.toFixed(1)}x${h.toFixed(1)})`);
      }
    }

    const overflow = boxes.filter(b => b.left < -0.5 || b.top < -0.5 || b.right > sr.width + 0.5 || b.bottom > sr.height + 0.5)
      .map(b => `${b.id} [${b.left.toFixed(1)},${b.top.toFixed(1)}]-[${b.right.toFixed(1)},${b.bottom.toFixed(1)}] in ${sr.width.toFixed(1)}x${sr.height.toFixed(1)}`);

    const wireIntersections = [];
    const flowKeys = Array.from(document.querySelectorAll('.flowtab')).map(button => button.dataset.flow);
    for (const key of flowKeys) {
      pickFlow(key);
      const flow = flows[key];
      const paths = Array.from(document.querySelectorAll('path.wire'));
      paths.forEach(pathEl => {
        const index = Number(pathEl.dataset.step);
        const step = flow.steps[index];
        if (!step) return;
        const length = pathEl.getTotalLength();
        for (const box of boxes) {
          if (box.id === step.from || box.id === step.to) continue;
          let hit = false;
          for (let distance = 0; distance <= length; distance += 3) {
            const point = pathEl.getPointAtLength(distance);
            if (point.x > box.left && point.x < box.right && point.y > box.top && point.y < box.bottom) {
              hit = true;
              break;
            }
          }
          if (hit) wireIntersections.push(`${key}[${index + 1}] ${step.from}->${step.to} crosses ${box.id}`);
        }
      });
    }

    return {
      stage: { width: sr.width, height: sr.height },
      boxes,
      overlaps,
      overflow,
      wireIntersections
    };
  });

  await browser.close();
  console.log(JSON.stringify(report, null, 2));
  if (report.overlaps.length || report.overflow.length || report.wireIntersections.length) process.exit(1);
})().catch(error => {
  console.error(error);
  process.exit(1);
});
