const puppeteer = require('/Users/bytedance/.local/lib/node_modules/@bytedance-dev/bytedcli/node_modules/puppeteer-core');

(async () => {
  const browser = await puppeteer.launch({
    executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    headless: 'new',
    args: ['--no-sandbox', '--disable-setuid-sandbox', '--disable-dev-shm-usage'],
  });
  const page = await browser.newPage();
  await page.setViewport({ width: 1440, height: 900 });
  await page.goto('http://127.0.0.1:18801', { waitUntil: 'networkidle0', timeout: 30000 });
  await page.screenshot({ path: 'screenshot-overview.png', fullPage: true });

  // Click 待办
  const buttons = await page.$$('.nav-item');
  for (const btn of buttons) {
    const text = await page.evaluate((el) => el.textContent, btn);
    if (text && text.includes('待办')) {
      await btn.click();
      await page.waitForTimeout(1500);
      await page.screenshot({ path: 'screenshot-todos.png', fullPage: true });
      break;
    }
  }

  // Click 任务
  const buttons2 = await page.$$('.nav-item');
  for (const btn of buttons2) {
    const text = await page.evaluate((el) => el.textContent, btn);
    if (text && text.includes('任务')) {
      await btn.click();
      await page.waitForTimeout(1500);
      await page.screenshot({ path: 'screenshot-tasks.png', fullPage: true });
      break;
    }
  }

  await browser.close();
  console.log('done');
})();
