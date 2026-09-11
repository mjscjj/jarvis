// Run against Vite. Exercise the real app shell with isolated API fixtures;
// page contents are stubbed because this regression owns navigation state.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const base = process.env.SIDEBAR_TEST_URL || 'http://127.0.0.1:5189'

async function openApp(seed = {}) {
  const context = await browser.newContext({ viewport: { width: 1440, height: 1400 } })
  const page = await context.newPage()
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  page.setDefaultTimeout(10000)
  await page.addInitScript(seed => {
    if (sessionStorage.getItem('sidebar-test-seeded')) return
    for (const [key, value] of Object.entries(seed)) localStorage.setItem(key, JSON.stringify(value))
    sessionStorage.setItem('sidebar-test-seeded', 'true')
  }, seed)
  await page.route(/\/src\/(Chat|Plugins|okr\/OKRModule)\.tsx/, route => route.fulfill({ contentType: 'application/javascript', body: 'export default function Page(){return null}' }))
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    let data
    if (path === '/api/auth/status') data = { enabled: false, status: 'authenticated' }
    else if (path === '/api/agent-identity') data = { display_name: 'Jarvis' }
    else if (path === '/api/setup/bootstrap') data = { machine_configuration_ready: true }
    else if (path === '/api/setup/status') data = {
      onboarding_required: false, runtime_id: 'sidebar-test', app_ready: true, world_model_ready: true,
      configuration: { machine_configuration_ready: true, agent_name_configured: true },
      lark: { available: true, app_id: 'test', credential_available: true, bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } },
      agent: { available: true, authenticated: true },
    }
    else if (path === '/api/app-modules') data = { items: ['okr', 'biz-okr'].map(key => ({ key, is_enabled: true })) }
    else if (path === '/api/plugin-installations') {
      await new Promise(resolve => setTimeout(resolve, 150))
      data = { items: [{ id: 'product', name: '产品管理', kind: 'collector', enabled: true }] }
    } else if (path === '/api/biz-okr/me') data = { authenticated: true, configured: true, management_access: true }
    else data = { items: [], total: 0 }
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.goto(`${base}/#/biz-okr?tab=okr-plan`)
  try {
    await page.locator('.app-menu .ant-menu-submenu-title').filter({ hasText: /^插件$/ }).waitFor()
  } catch (error) {
    console.error(JSON.stringify({ errors, body: await page.locator('body').innerText() }))
    throw error
  }
  return { page, context, errors }
}

function title(page, label) {
  return page.locator('.app-menu .ant-menu-submenu-title').filter({ hasText: new RegExp(`^${label}$`) })
}

async function expectOpen(page, label, open) {
  await title(page, label).and(page.locator(`[aria-expanded="${open}"]`)).waitFor()
}

try {
  const { page, context, errors } = await openApp()
  for (const label of ['OKR', '插件', '系统']) await expectOpen(page, label, false)
  await title(page, '插件').click()
  await title(page, 'OKR').click()
  await expectOpen(page, '插件', true)
  await expectOpen(page, 'OKR', true)
  await page.reload()
  await expectOpen(page, '插件', true)
  await expectOpen(page, 'OKR', true)
  await expectOpen(page, '系统', false)

  await page.locator('.sider-collapse-btn').click()
  await page.waitForFunction(() => document.querySelector('.app-sider')?.classList.contains('ant-layout-sider-collapsed'))
  await page.locator('.sider-collapse-btn').click()
  await expectOpen(page, '插件', true)
  await expectOpen(page, 'OKR', true)

  // Closing the group containing the active page must survive navigation and reload.
  await title(page, 'OKR').click()
  await expectOpen(page, 'OKR', false)
  await page.evaluate(() => { location.hash = '/chat' })
  await page.evaluate(() => { location.hash = '/biz-okr?tab=okr-plan' })
  await page.reload()
  await expectOpen(page, 'OKR', false)
  await expectOpen(page, '插件', true)
  await title(page, '插件').click()
  await page.reload()
  for (const label of ['OKR', '插件', '系统']) await expectOpen(page, label, false)
  assert.deepEqual(errors, [])
  await context.close()

  const legacy = await openApp({ 'jarvis.pluginsOpen': true })
  await expectOpen(legacy.page, '插件', true)
  await expectOpen(legacy.page, 'OKR', false)
  await expectOpen(legacy.page, '系统', false)
  await title(legacy.page, '插件').click()
  await legacy.page.reload()
  await expectOpen(legacy.page, '插件', false)
  assert.deepEqual(legacy.errors, [])
  await legacy.context.close()
  console.log(JSON.stringify({ result: 'passed', checks: ['all groups initially closed', 'restore selected groups after reload', 'whole sidebar collapse preserves choices', 'active route never forces a group open', 'remember all closed', 'preserve legacy plugin choice and allow overriding it'] }))
} finally {
  await browser.close()
}
