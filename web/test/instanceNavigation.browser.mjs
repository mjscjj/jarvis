// Run against the deployed main origin. Anonymous entry tests use real APIs;
// signed-in navigation uses fixtures and never writes business data.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.INSTANCE_NAVIGATION_URL
assert(base, 'Set INSTANCE_NAVIGATION_URL to the main origin')
const origin = new URL(base).origin
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const results = []

async function openPage() {
  const context = await browser.newContext()
  const page = await context.newPage()
  page.setDefaultTimeout(20000)
  const requests = [], errors = []
  page.on('request', request => {
    const url = new URL(request.url())
    if (url.origin === origin && url.pathname.includes('/api/')) requests.push(url.pathname)
  })
  page.on('pageerror', error => { errors.push(error.message); console.error('Page error:', error.message) })
  return { context, page, requests, errors }
}

async function fixtureAPIs(page, principal, preferred = false) {
  const session = { id: 'cs_navigation_test', title: 'Navigation fixture', agent: 'codex', model: 'test', reasoning_effort: 'medium', sources: [], draft: {}, messages: [], pending_attachments: [], archived: false }
  await page.route('**/api/**', async route => {
    const inDev = new URL(route.request().url()).pathname.startsWith('/dev/')
    const path = new URL(route.request().url()).pathname.replace(/^\/dev/, '')
    let data = { items: [], total: 0 }
    if (path === '/api/auth/status') {
      const allowed = principal || (preferred && !inDev)
      data = { enabled: true, prefer_main_workbench: preferred && inDev, status: allowed ? 'authenticated' : 'unauthenticated', ...(allowed ? { user: { username: 'fixture', name: 'Fixture' } } : {}) }
    }
    else if (path === '/api/agent-identity') data = { display_name: 'Test' }
    else if (path === '/api/setup/bootstrap') data = { machine_configuration_ready: true }
    else if (path === '/api/setup/status') data = { onboarding_required: false, app_ready: true, world_model_ready: true, configuration: { machine_configuration_ready: true, agent_name_configured: true }, lark: { available: true, app_id: 'test', application_checks: [], credential_available: true, bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } }, agent: { available: true, authenticated: true } }
    else if (path === '/api/app-modules') data = { items: ['okr', 'biz-okr'].map(key => ({ key, is_enabled: true })) }
    else if (path === '/api/biz-okr/me') data = { authenticated: true, configured: true, management_access: true, user: { open_id: 'fixture', union_id: 'fixture', name: 'Fixture' } }
    else if (/^\/api\/(okr-chat|chat)\//.test(path)) {
      if (path.endsWith('/agents')) data = { items: [{ id: 'codex', name: 'Test', available: true, default: true }] }
      else if (path.endsWith('/models')) data = { items: [{ id: 'test', name: 'Test', default: true, reasoning_efforts: ['medium'] }] }
      else if (path.endsWith('/sessions')) data = { items: [session] }
      else data = session
    } else if (path === '/api/biz-okr/plans') data = { quarter: '2026-Q4', available_quarters: ['2026-Q4'], plans: [] }
    else if (path === '/api/okr/enums') data = { statuses: ['on_track'], point_kinds: ['strategy'], lights: ['green'] }
    await route.fulfill({ json: { code: 0, data } })
  })
}

try {
  for (const key of (process.env.INSTANCE_NAVIGATION_FIXTURES_ONLY ? [] : ['weekly-report', 'biz-okr', 'okr'])) {
    const { context, page, requests, errors } = await openPage()
    const suffix = `?source=navigation-test#/${key}?tab=okr-plan&quarter=2026-Q4&plan_id=entry-test&comment_id=comment-test`
    await page.goto(`${origin}/${suffix}`)
    await page.waitForURL(url => url.pathname === '/dev/', { waitUntil: 'domcontentloaded' })
    // The existing page router may canonicalize aliases and parameter order.
    const target = new URL(page.url())
    assert.equal(target.search, '?source=navigation-test')
    const expected = new URL(`${origin}/dev/${suffix}`)
    const actualParams = new URLSearchParams(target.hash.split('?')[1])
    for (const [key, value] of new URLSearchParams(expected.hash.split('?')[1])) assert.equal(actualParams.get(key), value)
    await page.waitForFunction(() => document.querySelector('#root')?.childElementCount > 0)
    await page.getByRole('button', { name: '使用飞书登录', exact: true }).waitFor()
    assert(!requests.some(path => path.includes('/api/setup/')), 'Shared modules must not start personal installation or world modeling')
    assert.equal(await page.locator('.world-model-setup').count(), 0)
    assert.equal(requests.filter(path => path.startsWith('/api/')).length, 0, 'Main APIs must not load before the initial redirect')
    assert.deepEqual(errors, [])
    results.push({ case: `old-${key}`, mode: 'real-anonymous', passed: true, pathname: new URL(page.url()).pathname })
    await context.close()
  }

  for (const principal of [true, false]) {
    const { context, page, requests, errors } = await openPage()
    await fixtureAPIs(page, principal)
    await page.goto(`${origin}/dev/#/biz-okr?tab=okr-plan&quarter=2026-Q4`)
    await page.getByRole('button', { name: '新建 Plan', exact: true }).waitFor()
    await page.getByRole('link', { name: '返回主工作台', exact: true }).waitFor()
    const chatPrefix = principal ? '/dev/api/chat/' : '/dev/api/okr-chat/'
    await page.waitForRequest(request => new URL(request.url()).pathname.startsWith(chatPrefix), { timeout: 1000 }).catch(() => {})
    assert(requests.some(path => path.startsWith(chatPrefix)), `Missing chat request: ${chatPrefix}`)
    assert(requests.every(path => path.startsWith('/dev/api/')), 'Dev page requested a main API')
    if (principal) {
      await page.getByRole('link', { name: '返回主工作台', exact: true }).click()
      await page.waitForURL(`${origin}/#/chat`)
      await page.locator('.app-menu').waitFor()
      await page.locator('.app-menu .ant-menu-submenu-title').filter({ hasText: /^OKR$/ }).click()
      await page.getByRole('menuitem', { name: 'Biz OKR Plan', exact: true }).click()
      await page.waitForURL(url => url.pathname === '/dev/', { waitUntil: 'domcontentloaded' })
      await page.getByRole('button', { name: '新建 Plan', exact: true }).waitFor()
      await page.getByRole('link', { name: '返回主工作台', exact: true }).click()
      await page.waitForURL(`${origin}/#/chat`)
      await page.locator('.app-menu').waitFor()
      requests.length = 0
      await page.evaluate(() => { window.location.hash = '/biz-okr?tab=okr-plan&quarter=2026-Q4' })
      await page.waitForURL(url => url.pathname === '/dev/' && url.hash.includes('/biz-okr'), { waitUntil: 'domcontentloaded' })
      await page.getByRole('button', { name: '新建 Plan', exact: true }).waitFor()
      assert(!requests.some(path => path.startsWith('/api/biz-okr/')), 'Internal navigation loaded main OKR data')
      await page.reload()
      await page.getByRole('button', { name: '新建 Plan', exact: true }).waitFor()
      assert.equal(new URL(page.url()).pathname, '/dev/')
      await page.evaluate(() => { window.location.hash = '/manage/automations' })
      assert.equal(new URL(page.url()).pathname, '/dev/')
    }
    assert.deepEqual(errors, [])
    results.push({ case: principal ? 'principal-return-and-internal-entry' : 'okr-guest-chat-and-return-link', mode: 'mock-api', passed: true })
    await context.close()
  }
  // Preference comes from a verified server identity, independently of dev
  // principal permissions. No real account is impersonated by these fixtures.
  for (const principal of [false, true]) {
    const { context, page, requests, errors } = await openPage()
    await fixtureAPIs(page, principal, true)
    await page.goto(`${origin}/dev/#/biz-okr?tab=okr-plan&quarter=2026-Q4`)
    await page.getByRole('button', { name: '新建 Plan', exact: true }).waitFor()
    await page.waitForFunction(() => !document.querySelector('a[href="/#/chat"]'))
    assert.equal(await page.getByRole('link', { name: '返回主工作台', exact: true }).count(), 0)
    const expand = page.getByRole('button', { name: '展开底部对话输入', exact: true })
    if (await expand.isVisible()) await expand.click()
    await page.getByRole('button', { name: '切换会话', exact: true }).click()
    await page.getByRole('button', { name: /全部会话与历史/ }).click()
    await page.getByRole('dialog').waitFor()
    assert(new URL(page.url()).hash.includes('/biz-okr'), 'OKR history left the module')
    assert(requests.some(path => path.startsWith(principal ? '/dev/api/chat/' : '/dev/api/okr-chat/')))
    assert(!requests.some(path => path.startsWith('/api/')), 'OKR history requested main APIs')
    await page.locator('.ant-modal-close').click()
    await page.locator('.app-menu').getByText('任务', { exact: true }).click()
    await page.waitForURL(`${origin}/#/work`)
    // Back returns to OKR; automatic direct-entry correction uses replace.
    await page.goBack()
    await page.waitForURL(url => url.pathname === '/dev/' && url.hash.includes('/biz-okr'), { waitUntil: 'domcontentloaded' })
    await page.goto(`${origin}/dev/#/chat`)
    await page.waitForURL(`${origin}/#/chat`)
    assert(!requests.includes('/dev/api/auth/login'), 'Main user was asked for dev principal login')
    if (principal) {
      await page.goto(`${origin}/dev/#/chat?session=cs_navigation_test`)
      await page.locator('.chat-workspace').waitFor()
      assert.equal(new URL(page.url()).pathname, '/dev/', 'Explicit dev session was sent to main')
    }
    assert.deepEqual(errors, [])
    results.push({ case: principal ? 'preferred-main-with-dev-principal' : 'preferred-main-with-okr-login-only', mode: 'mock-api', passed: true })
    await context.close()
  }
  console.log(JSON.stringify({ origin, results }, null, 2))
} catch (error) {
  console.error(JSON.stringify({ results, pages: await Promise.all(browser.contexts().flatMap(c => c.pages().map(async p => ({ url: p.url(), body: (await p.locator('body').innerText()).slice(0,1800) })))) }))
  throw error
} finally {
  await browser.close()
}
