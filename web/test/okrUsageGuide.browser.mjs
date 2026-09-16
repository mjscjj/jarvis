import assert from 'node:assert/strict'

const playwright = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const { chromium } = playwright.default ?? playwright
const base = process.env.OKR_BROWSER_URL
assert(base, 'OKR_BROWSER_URL must name the component fixture server')

const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_EXECUTABLE, args: ['--no-sandbox'] })
try {
  for (const width of [1400, 375]) {
    const context = await browser.newContext({ viewport: { width, height: 900 } })
    const page = await context.newPage()
    const errors = []
    page.on('pageerror', (error) => errors.push(error.message))
    await page.goto(new URL('/test/fixtures/usageGuide.html', base).href)
    const guideButton = page.getByRole('button', { name: '使用说明', exact: true })
    const feedbackButton = page.getByRole('button', { name: '建议反馈', exact: true })
    const [guideBox, feedbackBox] = await Promise.all([guideButton.boundingBox(), feedbackButton.boundingBox()])
    assert(guideBox && feedbackBox && guideBox.x < feedbackBox.x, 'usage guide must sit left of feedback')
    assert.equal(guideBox.height, feedbackBox.height, 'usage guide and feedback must use the same height')
    assert.equal(await guideButton.evaluate((element) => getComputedStyle(element).fontSize), await feedbackButton.evaluate((element) => getComputedStyle(element).fontSize), 'usage guide and feedback must use the same font size')
    await guideButton.click()
    const drawer = page.getByRole('dialog')
    await drawer.getByRole('heading', { name: '制定下季度 OKR Plan' }).waitFor()
    await drawer.getByText('怎么移动策略 / 产品具体 KR').waitFor()
    assert.equal(await drawer.locator('video').count(), 0, 'usage guide must not render recordings')
    await drawer.getByRole('button', { name: '使用场景', exact: true }).waitFor()
    for (const label of ['填写本周周报', '填写 OKR Review', '制定下季度 OKR', '查看与我相关的评论', '主持会议']) {
      await drawer.getByRole('button', { name: label, exact: true }).waitFor()
    }
    await drawer.getByRole('button', { name: '主持会议', exact: true }).click()
    await drawer.getByRole('heading', { name: '主持会议' }).waitFor()
    await drawer.getByText('先 Review、后 Plan', { exact: false }).first().waitFor()
    await drawer.getByText('下一条', { exact: false }).first().waitFor()
    await drawer.getByText('标记 Todo', { exact: false }).first().waitFor()
    assert.equal((await drawer.innerText()).includes('根据议程选择业务方向或负责人'), false)
    await drawer.getByRole('button', { name: '查看与我相关的评论', exact: true }).click()
    await drawer.getByRole('heading', { name: '查看与我相关的评论' }).waitFor()
    await drawer.getByRole('button', { name: '高频问题', exact: true }).click()
    await drawer.getByRole('heading', { name: '怎么删除 Plan 页面的 KR、产品具体 KR或策略具体 KR？' }).waitFor()
    await drawer.getByRole('button', { name: '页面介绍', exact: true }).click()
    await drawer.getByRole('cell', { name: 'Review 会议', exact: true }).waitFor()
    assert(await drawer.evaluate((element) => element.scrollWidth <= element.clientWidth), `${width}px drawer overflows`)
    assert.deepEqual(errors, [])
    console.log(`PASS: usage guide at ${width}px`)
    await context.close()
  }
} finally {
  await browser.close()
}
