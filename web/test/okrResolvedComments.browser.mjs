// Run against Vite with OKR_BROWSER_URL; comment requests are mocked and never write to the backend.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.OKR_BROWSER_URL
assert(base, 'OKR_BROWSER_URL must name a Vite development server')

const now = '2026-09-14T12:00:00Z'
const comment = (id, content, resolved) => ({
  id, version: 1, delete_token: `delete-${id}`, target_type: 'page',
  target_id: '2026-Q3:2026-W36', target_title: '2026-W36 OKR 页面',
  author_name: '测试用户', content, mentions: [], images: [], todo: false, resolved,
  created_at: now, updated_at: now, replies: [],
})
const comments = [comment('active-comment', '仍需处理', false), comment('resolved-comment', '已经处理', true)]
const browser = await chromium.launch({ headless: true, executablePath: process.env.CHROME_EXECUTABLE, args: ['--no-sandbox'] })
try {
  const page = await browser.newPage({ viewport: { width: 1400, height: 900 } })
  const pageErrors = []
  page.on('pageerror', (error) => pageErrors.push(error.message))
  await page.route('**/api/setup/bootstrap', (route) => route.fulfill({ json: { code: 0, data: { machine_configuration_ready: true } } }))
  await page.route('**/api/setup/status', (route) => route.fulfill({ json: { code: 0, data: { onboarding_required: false, runtime_id: 'test', app_ready: true, world_model_ready: true, configuration: { machine_configuration_ready: true, agent_name_configured: true }, lark: { available: true, credential_available: true, bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } }, agent: { available: true, authenticated: true } } } }))
  await page.route('**/api/biz-okr/comments?**', (route) => route.fulfill({ json: { code: 0, data: { quarter: '2026-Q3', week: '2026-W36', count: comments.length, comments } } }))
  await page.route('**/api/biz-okr/comments/*', async (route) => {
    assert.equal(route.request().method(), 'PUT')
    const id = new URL(route.request().url()).pathname.split('/').at(-1)
    const item = comments.find((value) => value.id === id)
    assert(item, `unknown comment ${id}`)
    const patch = route.request().postDataJSON()
    assert.equal(patch.expected_version, item.version)
    Object.assign(item, { resolved: patch.resolved, version: item.version + 1, updated_at: new Date().toISOString() })
    await route.fulfill({ json: { code: 0, data: item } })
  })

  await page.goto(`${base}/#/biz-okr?tab=review-fill&quarter=2026-Q3&week=2026-W36`)
  page.setDefaultTimeout(10000)
  const openComments = page.locator('button').filter({ hasText: /^评论\s*\d*$/ }).first()
  try {
    await openComments.click()
  } catch (error) {
    console.error('Could not open comments at', page.url(), (await page.locator('body').innerText()).slice(0, 800))
    throw error
  }
  const drawer = page.locator('aside[aria-hidden="false"]')
  await drawer.getByText('正在读取评论…', { exact: true }).waitFor({ state: 'hidden' })
  const active = drawer.locator('#comment-active-comment')
  const resolved = drawer.locator('#comment-resolved-comment')
  await active.waitFor()
  assert.equal(await resolved.count(), 0, 'resolved thread is hidden by default')
  assert.match(await drawer.locator('header').innerText(), /1 条评论/)

  await drawer.getByRole('button', { name: '显示历史评论 1' }).click()
  await resolved.waitFor()
  await resolved.getByRole('button', { name: '重新打开' }).click()
  await drawer.getByRole('button', { name: '隐藏历史评论' }).click()
  await active.waitFor()
  await resolved.waitFor()
  assert.match(await drawer.locator('header').innerText(), /2 条评论/)

  await active.getByRole('button', { name: '标记解决' }).click()
  await active.waitFor({ state: 'hidden' })
  assert.match(await drawer.locator('header').innerText(), /1 条评论/)
  await drawer.getByRole('button', { name: '显示历史评论 1' }).click()
  await active.waitFor()
  if (process.env.OKR_BROWSER_SCREENSHOT) await page.screenshot({ path: process.env.OKR_BROWSER_SCREENSHOT })
  assert.equal(pageErrors.length, 0, pageErrors.join('\n'))
  console.log('PASS: resolved comments stay in history, reopened comments return to the default view, and active counts update')
} finally {
  await browser.close()
}
