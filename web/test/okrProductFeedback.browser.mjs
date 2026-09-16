import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const playwrightModule = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const { chromium } = playwrightModule.chromium ? playwrightModule : playwrightModule.default
const base = process.env.OKR_REAL_BASE_URL?.replace(/\/$/, '')
const sessionFile = process.env.OKR_REAL_SESSION_FILE
const expectedName = process.env.OKR_REAL_EXPECTED_NAME
assert(base && sessionFile && expectedName, 'Set OKR_REAL_BASE_URL, OKR_REAL_SESSION_FILE and OKR_REAL_EXPECTED_NAME')

const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE || undefined, headless: true, args: ['--no-sandbox'] })
const context = await browser.newContext({ viewport: { width: 1450, height: 1000 } })
const session = JSON.parse(readFileSync(sessionFile, 'utf8'))
await context.addCookies([{ name: session.cookie_name || 'jarvis_okr_session', value: session.cookie, url: base, httpOnly: true, sameSite: 'Lax' }])
const page = await context.newPage()
page.setDefaultTimeout(20000)
const pageErrors = []
page.on('pageerror', error => pageErrors.push(error.message))

const runID = new Date().toISOString().replace(/[:.]/g, '-')
const title = `[反馈功能验收 ${runID}] 页面问题`
const detail = '验证建议反馈入口、反馈人头像、回复、+1 头像和解决状态。'
const reply = '补充一条验收回复。'
let feedbackID = ''

try {
  await page.goto(`${base}/#/biz-okr?tab=okr-plan&quarter=2026-Q4`)
  await page.locator('#okr-workspace-root').waitFor()
  await page.getByRole('button', { name: '打开建议反馈' }).click()
  const drawer = page.getByRole('dialog', { name: '建议反馈' })
  await drawer.getByText('正在加载反馈…').waitFor({ state: 'hidden' })
  await drawer.getByRole('button', { name: '＋ 提交新问题' }).click()
  await drawer.getByLabel('反馈标题').fill(title)
  await drawer.getByLabel('反馈详情').fill(detail)
  const createResponse = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === '/api/biz-okr/feedback')
  await drawer.getByRole('button', { name: '提交反馈', exact: true }).click()
  const createdResponse = await createResponse
  const created = await createdResponse.json()
  assert.equal(createdResponse.status(), 201, JSON.stringify(created))
  feedbackID = created.data?.id || ''
  assert(feedbackID, 'Feedback response has no id')
  assert.equal(created.data.author?.name, expectedName)
  assert(created.data.author?.avatar_url, 'Feedback author has no avatar snapshot')

  let card = drawer.locator('article').filter({ hasText: title })
  await card.getByText(detail).waitFor()
  assert.equal(await card.locator(`img[alt="${expectedName}"]`).count(), 1, 'Reporter avatar is not visible')

  const plusOneResponse = page.waitForResponse(response => response.request().method() === 'PUT' && new URL(response.url()).pathname === `/api/biz-okr/feedback/${feedbackID}/plus-one`)
  await card.getByRole('button', { name: '+1', exact: true }).click()
  assert.equal((await plusOneResponse).status(), 200)
  await card.getByRole('button', { name: '+1 · 1', exact: true }).waitFor()
  assert((await card.locator(`img[alt="${expectedName}"]`).count()) >= 2, '+1 user avatar is not visible')

  await card.getByRole('button', { name: '回复', exact: true }).click()
  await card.getByPlaceholder(`回复 ${expectedName}`).fill(reply)
  const replyResponse = page.waitForResponse(response => response.request().method() === 'POST' && new URL(response.url()).pathname === `/api/biz-okr/feedback/${feedbackID}/replies`)
  await card.getByRole('button', { name: '回复', exact: true }).last().click()
  assert.equal((await replyResponse).status(), 201)
  await card.getByText(reply).waitFor()

  const resolveResponse = page.waitForResponse(response => response.request().method() === 'PATCH' && new URL(response.url()).pathname === `/api/biz-okr/feedback/${feedbackID}/status`)
  await card.getByRole('button', { name: '标记已解决' }).click()
  assert.equal((await resolveResponse).status(), 200)
  await card.waitFor({ state: 'hidden' })

  const historyResponse = page.waitForResponse(response => response.request().method() === 'GET' && new URL(response.url()).pathname === '/api/biz-okr/feedback' && new URL(response.url()).searchParams.get('resolved') === 'true')
  await drawer.getByRole('button', { name: '已解决' }).click()
  assert.equal((await historyResponse).status(), 200)
  card = drawer.locator('article').filter({ hasText: title })
  await card.getByText('已解决', { exact: true }).waitFor()
  await card.getByText(new RegExp(`${expectedName} 于 .* 标记解决`)).waitFor()
  await page.screenshot({ path: process.env.OKR_FEEDBACK_SCREENSHOT || '/tmp/jarvis-product-feedback.png', fullPage: true })

  const reopenResponse = page.waitForResponse(response => response.request().method() === 'PATCH' && new URL(response.url()).pathname === `/api/biz-okr/feedback/${feedbackID}/status`)
  await card.getByRole('button', { name: '重新打开' }).click()
  assert.equal((await reopenResponse).status(), 200)
  await card.waitFor({ state: 'hidden' })
  await drawer.getByRole('button', { name: /待解决/ }).click()
  await drawer.locator('article').filter({ hasText: title }).waitFor()

  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  const trigger = page.getByRole('button', { name: '打开建议反馈' })
  await trigger.waitFor()
  assert(await trigger.isVisible(), 'Feedback trigger is hidden on a narrow viewport')
  await page.screenshot({ path: process.env.OKR_FEEDBACK_MOBILE_SCREENSHOT || '/tmp/jarvis-product-feedback-mobile.png', fullPage: true })
  assert.deepEqual(pageErrors, [])
  console.log(JSON.stringify({ feedback_id: feedbackID, author: expectedName, checks: ['entry', 'create', 'reporter-avatar', 'plus-one', 'plus-one-avatar', 'reply', 'resolve', 'history', 'reopen', 'mobile-entry'] }))
} finally {
  await browser.close()
}
