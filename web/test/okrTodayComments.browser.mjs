// Start `npm run test:comments:serve`, then set OKR_BROWSER_URL to its URL.
// This mounts the real drawer with mocked comments and no backend connection.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.OKR_BROWSER_URL
assert(base, 'OKR_BROWSER_URL must name the component fixture server')
const now = new Date('2026-09-16T12:00:00Z')
const yesterday = new Date(now.getTime() - 86400000)
const comment = (id, content, createdAt, resolved = false, replies = []) => ({
  id, version: 1, delete_token: `delete-${id}`, target_type: 'page',
  target_id: '2026-Q3:2026-W36', target_title: '2026-W36 OKR 页面',
  author_name: '测试用户', content, mentions: [], images: [], todo: false, resolved,
  created_at: createdAt, updated_at: createdAt, replies,
})
const reply = { ...comment('today-reply', '今天回复旧讨论', now.toISOString()), parent_id: 'old-thread' }
const comments = [
  comment('old-thread', '昨天发起的讨论', yesterday.toISOString(), false, [reply]),
  comment('today-resolved', '今天新增且已解决', new Date(now.getTime() + 1).toISOString(), true),
  comment('old-only', '只有昨天内容', new Date(yesterday.getTime() + 1000).toISOString()),
]

const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_EXECUTABLE, args: ['--no-sandbox'] })
try {
  for (const [width, timezoneId] of [[1400, 'UTC'], [375, 'Asia/Shanghai']]) {
    const context = await browser.newContext({ viewport: { width, height: 900 }, timezoneId })
    const page = await context.newPage()
    page.setDefaultTimeout(10000)
    await page.clock.install({ time: now })
    const errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/api/**', route => {
      assert.equal(route.request().method(), 'GET', 'UI test must not write data')
      assert.equal(new URL(route.request().url()).pathname, '/api/biz-okr/comments', 'unexpected API request')
      return route.fulfill({ json: { code: 0, data: { quarter: '2026-Q3', week: '2026-W36', count: 4, comments } } })
    })
    await page.goto(new URL('/test/fixtures/commentDrawer.html', base).href)
    const drawer = page.locator('aside[aria-hidden="false"]')
    const todayButton = drawer.getByRole('button', { name: '只浏览今日评论 2', exact: true })
    await todayButton.waitFor()
    assert.equal(await drawer.evaluate(element => getComputedStyle(element).position), 'fixed', 'production drawer styles must be loaded')
    const [allBox, todayBox] = await Promise.all([
      drawer.getByRole('button', { name: '逐条浏览', exact: true }).boundingBox(), todayButton.boundingBox(),
    ])
    assert(allBox && todayBox && Math.abs(allBox.y - todayBox.y) < 2, 'browse buttons must stay beside each other')
    assert(await drawer.locator('header').evaluate(element => element.scrollWidth <= element.clientWidth), 'header overflows')
    await todayButton.click()
    await drawer.getByRole('heading', { name: '今日评论' }).waitFor()
    await drawer.locator('#comment-today-reply[aria-current="true"]').waitFor()
    assert.match(await drawer.locator('header').innerText(), /第 1\/2 条今日新增评论/)
    await drawer.getByText('今天回复旧讨论', { exact: true }).waitFor()
    await drawer.getByText('昨天发起的讨论', { exact: true }).waitFor()
    assert.equal(await drawer.getByText('只有昨天内容', { exact: true }).count(), 0)
    await drawer.getByRole('button', { name: '下一个', exact: true }).click()
    await drawer.locator('#comment-today-resolved[aria-current="true"]').waitFor()
    await drawer.getByText('已浏览完今日新增评论', { exact: true }).waitFor()
    await drawer.getByRole('button', { name: '上一个', exact: true }).click()
    await drawer.locator('#comment-today-reply[aria-current="true"]').waitFor()

    // Ordinary review keeps its existing unresolved-thread behavior.
    await drawer.getByRole('button', { name: '查看全部', exact: true }).click()
    await drawer.getByRole('button', { name: '逐条浏览', exact: true }).click()
    await drawer.locator('#comment-old-thread[aria-current="true"]').waitFor()
    await drawer.getByRole('button', { name: '下一个', exact: true }).click()
    await drawer.locator('#comment-old-only[aria-current="true"]').waitFor()
    await drawer.getByRole('button', { name: '查看全部', exact: true }).click()

    // Keep the page open across its local midnight; the count must expire.
    const untilMidnight = await page.evaluate(() => {
      const current = new Date()
      return new Date(current.getFullYear(), current.getMonth(), current.getDate() + 1).getTime() - current.getTime() + 100
    })
    await page.clock.fastForward(untilMidnight)
    await drawer.getByRole('button', { name: '只浏览今日评论 0', exact: true }).click()
    await drawer.getByText('今天暂无新增评论', { exact: true }).waitFor()
    assert.equal(await drawer.locator('footer').count(), 0)
    await drawer.getByRole('button', { name: '关闭评论', exact: true }).click()
    await page.clock.setSystemTime(now)
    await page.getByRole('button', { name: '打开评论', exact: true }).click()
    await drawer.getByRole('button', { name: '只浏览今日评论 2', exact: true }).waitFor()
    assert.deepEqual(errors, [])
    if (process.env.OKR_BROWSER_SCREENSHOT) await page.screenshot({ path: `${process.env.OKR_BROWSER_SCREENSHOT}-${width}.png` })
    console.log(`PASS: ${width}px ${timezoneId}: replies, resolved comments, navigation, local midnight, reopen`)
    await context.close()
  }
} finally {
  await browser.close()
}
