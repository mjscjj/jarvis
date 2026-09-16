// Run against Vite. Exercise the real app shell with isolated API fixtures;
// page contents are stubbed because this regression owns navigation state.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const base = process.env.SIDEBAR_TEST_URL || 'http://127.0.0.1:5189'
const authSource = await (await fetch(`${base}/src/auth.tsx`)).text()
const reactPath = authSource.match(/from ["']([^"']*react\.js[^"']*)["']/)[1]

async function openApp(seed = {}, options = {}) {
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
  await page.addInitScript(() => {
    const fetch = window.fetch.bind(window)
    window.backgroundRequests = []
    window.fetch = (path, options) => {
      if (['/api/tasks', '/api/debug/failures', '/api/plugin-installations'].includes(String(path).split('?')[0])) {
        window.backgroundRequests.push({ path: String(path).split('?')[0], signal: options?.signal })
      }
      return fetch(path, options)
    }
  })
  if (options.clock) await page.clock.install()
  const auth = { enabled: false, status: 'authenticated', ...options.auth }
  const requests = []
  const pending = []
  const controls = { hold: false, authError: false, total: 1 }
  const okr = { authenticated: true, configured: true, management_access: true, user: { open_id: 'okr-visitor', name: 'OKR visitor' } }
  const chatSession = { id: 'cs_test', title: '测试对话', agent: 'codex', model: 'test', reasoning_effort: 'medium', sources: [], draft: {}, archived: false, messages: [], pending_attachments: [], created_at: new Date().toISOString(), updated_at: new Date().toISOString() }
  if (!options.realModule) {
    await page.route(options.realChat ? /\/src\/Plugins\.tsx/ : /\/src\/(Chat|Plugins)\.tsx/, route => route.fulfill({ contentType: 'application/javascript', body: 'export default function Page(){return null}' }))
    await page.route(/\/src\/okr\/OKRModule\.tsx/, route => route.fulfill({ contentType: 'application/javascript', body: `
      import React from '${reactPath}';
      import IdentityBoundary from '/src/okr/IdentityBoundary.tsx';
      function Draft(){const [text,setText]=React.useState('');React.useEffect(()=>{window.draftMounts=(window.draftMounts||0)+1;},[]);return React.createElement('input',{placeholder:'module draft',value:text,onChange:event=>setText(event.target.value)});}
      export default function Page(){return React.createElement(IdentityBoundary,null,React.createElement(Draft));}
    ` }))
  }
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    requests.push(path)
    if (['/api/auth/logout', '/api/biz-okr/auth/logout'].includes(path)) {
      assert.equal(route.request().method(), 'POST')
      Object.assign(auth, { status: 'unauthenticated', user: undefined })
      Object.assign(okr, { authenticated: false, user: undefined })
      await route.fulfill({ json: { code: 0, data: { ...auth, logged_out: true } } })
      return
    }
    if (/^\/api\/(okr-chat|chat)\/sessions\/cs_test$/.test(path) && route.request().method() === 'PATCH') {
      await route.fulfill({ json: { code: 0, data: { ...chatSession, ...route.request().postDataJSON() } } })
      return
    }
    assert.equal(route.request().method(), 'GET', `Unexpected mutation: ${path}`)
    if (['/api/tasks', '/api/debug/failures', '/api/plugin-installations'].includes(path) && auth.enabled && !auth.user) {
      errors.push(`Unexpected guest request: ${path}`)
      await route.fulfill({ status: 401, json: { code: 401, msg: 'principal session required' } })
      return
    }
    let data
    if (path === '/api/auth/status') {
      if (controls.authError) { await route.fulfill({ status: 503, json: { code: 503, msg: 'identity unavailable' } }); return }
      data = { ...auth }
    }
    else if (path === '/api/agent-identity') data = { display_name: 'Jarvis' }
    else if (path === '/api/setup/bootstrap') data = { machine_configuration_ready: true }
    else if (path === '/api/setup/status') data = {
      onboarding_required: false, runtime_id: 'sidebar-test', app_ready: true, world_model_ready: true,
      configuration: { machine_configuration_ready: true, agent_name_configured: true },
      lark: { available: true, app_id: 'test', application_checks: [{ event: 'im.message.receive_v1', ready: true }, { event: 'card.action.trigger', ready: true }], credential_available: true, bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } },
      agent: { available: true, authenticated: true },
    }
    else if (path === '/api/app-modules') data = { items: ['okr', 'biz-okr'].map(key => ({ key, is_enabled: true })) }
    else if (path === '/api/plugin-installations') {
      data = { items: [{ id: 'product', name: '产品管理', kind: 'collector', enabled: true }] }
    } else if (path === '/api/biz-okr/me') data = { ...okr }
    else if (/^\/api\/(okr-chat|chat)\//.test(path)) {
      if (path.endsWith('/agents')) data = { items: [{ id: 'codex', name: 'Test', available: true, default: true }] }
      else if (path.endsWith('/models')) data = { items: [{ id: 'test', name: 'Test', default: true, reasoning_efforts: ['medium'], default_reasoning_effort: 'medium' }] }
      else if (path.endsWith('/sessions')) data = { items: [chatSession] }
      else if (path.endsWith('/sessions/cs_test')) data = chatSession
      else throw new Error(`Unexpected chat request: ${path}`)
    }
    else if (path === '/api/biz-okr/plans') data = { quarter: new URL(route.request().url()).searchParams.get('quarter'), available_quarters: ['2026-Q4'], plans: [] }
    else if (path === '/api/okr/enums') data = { statuses: ['on_track'], point_kinds: ['strategy', 'product'], lights: ['green'] }
    else if (path === '/api/tasks') data = { items: [], total: controls.total }
    else if (path === '/api/debug/failures') data = { items: [{ recovered: false }] }
    else {
      errors.push(`Unexpected API: ${path}`)
      await route.fulfill({ status: 500, json: { code: 500, msg: `Unexpected API: ${path}` } })
      return
    }
    if (controls.hold && ['/api/tasks', '/api/debug/failures', '/api/plugin-installations'].includes(path)) {
      await new Promise(resolve => pending.push({ path, release: resolve }))
    }
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.goto(`${base}/#${options.hash || '/biz-okr?tab=okr-plan'}`)
  try {
    if (options.realModule) await page.getByRole('button', { name: '新建 Plan', exact: true }).waitFor()
    else await page.getByPlaceholder('module draft').waitFor()
  } catch (error) {
    console.error(JSON.stringify({ errors, body: await page.locator('body').innerText() }))
    throw error
  }
  return { page, context, errors, auth, okr, requests, pending, controls }
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

  const principalPaths = ['/api/tasks', '/api/debug/failures', '/api/plugin-installations']
  const recover = page => page.evaluate(async () => {
    const { authEvents } = await import('/src/api.ts')
    authEvents.dispatchEvent(new Event('expired'))
  })
  for (const hash of ['/biz-okr?tab=okr-plan', '/weekly-report?tab=weekly-fill']) {
    const guest = await openApp({}, { auth: { enabled: true, status: 'unauthenticated' }, hash, clock: true })
    const { page } = guest
    const draft = page.getByPlaceholder('module draft')
    await draft.fill('unsaved module content')
    const mounts = await page.evaluate(() => window.draftMounts)
    await page.clock.fastForward(65_000)
    await page.evaluate(() => {
      document.dispatchEvent(new Event('visibilitychange'))
      window.dispatchEvent(new Event('jarvis:plugins-changed'))
    })
    assert.deepEqual(guest.requests.filter(path => principalPaths.includes(path)), [])
    assert.equal(await draft.inputValue(), 'unsaved module content')
    assert.equal(await page.evaluate(() => window.draftMounts), mounts)
    assert.equal(await page.locator('.agent-activity-error, .ant-badge-status-error').count(), 0)
    if (hash.startsWith('/biz-okr')) {
      assert.equal(await page.locator('.agent-activity-icon').getAttribute('data-state'), 'inactive')
      assert.doesNotMatch(await page.locator('.agent-activity-icon').getAttribute('aria-label'), /任务|读取/)
    } else assert.equal(await page.locator('.app-sider').count(), 0)
    assert.deepEqual(guest.errors, [])
    await guest.context.close()
  }

  for (const hash of ['/biz-okr?tab=okr-plan&quarter=2026-Q4', '/weekly-report?tab=okr-plan&quarter=2026-Q4']) {
    const guest = await openApp({}, { auth: { enabled: true, status: 'unauthenticated' }, hash, realModule: true, clock: true })
    await guest.page.getByRole('button', { name: '新建 Plan', exact: true }).click()
    const draft = guest.page.getByRole('textbox', { name: 'Plan 名称', exact: true })
    await draft.fill('unsaved real Plan')
    const editor = await draft.elementHandle()
    const statusResponse = guest.page.waitForResponse(response => new URL(response.url()).pathname === '/api/auth/status')
    await recover(guest.page)
    await statusResponse
    await guest.page.clock.fastForward(65_000)
    assert.equal(await draft.inputValue(), 'unsaved real Plan')
    assert.equal(await editor.evaluate(element => element.isConnected), true)
    assert.deepEqual(guest.requests.filter(path => principalPaths.includes(path) || path.startsWith('/api/chat/')), [])
    assert.deepEqual(guest.errors, [])
    await guest.context.close()
  }

  const signedIn = await openApp({}, {
    auth: { enabled: true, status: 'authenticated', user: { name: 'Principal', username: 'principal', email: 'principal@example.test' } },
    clock: true,
  })
  const principalUser = signedIn.auth.user
  const icon = signedIn.page.locator('.agent-activity-icon')
  await signedIn.page.getByPlaceholder('module draft').fill('keep across identity changes')
  await signedIn.page.waitForFunction(() => document.querySelector('.agent-activity-icon')?.dataset.state === 'running')
  await title(signedIn.page, '系统').locator('.ant-badge-status-error').waitFor()
  await title(signedIn.page, '插件').click()
  await signedIn.page.getByText('产品管理', { exact: true }).waitFor()
  const mounts = await signedIn.page.evaluate(() => window.draftMounts)
  signedIn.controls.hold = true
  await signedIn.page.clock.fastForward(61_000)
  await signedIn.page.evaluate(() => window.dispatchEvent(new Event('jarvis:plugins-changed')))
  await signedIn.page.waitForFunction(paths => paths.every(path => window.backgroundRequests.filter(request => request.path === path).length >= 2), principalPaths)
  Object.assign(signedIn.auth, { status: 'unauthenticated', user: undefined })
  await recover(signedIn.page)
  await signedIn.page.waitForFunction(() => document.querySelector('.agent-activity-icon')?.dataset.state === 'inactive')
  assert.deepEqual(await signedIn.page.evaluate(paths => paths.map(path => {
    const requests = window.backgroundRequests.filter(request => request.path === path)
    return requests[requests.length - 1].signal.aborted
  }), principalPaths), [true, true, true], 'Losing principal identity aborts all background reads')
  assert.deepEqual(signedIn.pending.map(request => request.path).sort(), [...principalPaths].sort())
  signedIn.controls.hold = false
  for (const request of signedIn.pending.splice(0)) request.release()
  await signedIn.page.clock.fastForward(65_000)
  const stopped = signedIn.requests.filter(path => principalPaths.includes(path))
  await signedIn.page.evaluate(() => {
    document.dispatchEvent(new Event('visibilitychange'))
    window.dispatchEvent(new Event('jarvis:plugins-changed'))
  })
  await signedIn.page.clock.fastForward(65_000)
  assert.deepEqual(signedIn.requests.filter(path => principalPaths.includes(path)), stopped)
  assert.equal(await icon.getAttribute('data-state'), 'inactive')
  assert.equal(await signedIn.page.locator('.agent-activity-error, .ant-badge-status-error').count(), 0)
  assert.equal(await signedIn.page.getByText('产品管理', { exact: true }).count(), 0, 'Aborted plugin response cannot restore old navigation')
  assert.equal(await signedIn.page.getByPlaceholder('module draft').inputValue(), 'keep across identity changes')
  assert.equal(await signedIn.page.evaluate(() => window.draftMounts), mounts)

  signedIn.controls.total = 2
  Object.assign(signedIn.auth, { status: 'authenticated', user: principalUser })
  const resumed = signedIn.page.waitForRequest(request => new URL(request.url()).pathname === '/api/plugin-installations')
  await recover(signedIn.page)
  await resumed
  await signedIn.page.getByRole('button', { name: /^Jarvis：正在执行 2 个任务，/ }).waitFor()
  await title(signedIn.page, '系统').locator('.ant-badge-status-error').waitFor()
  await signedIn.page.getByText('产品管理', { exact: true }).waitFor()
  signedIn.controls.authError = true
  await recover(signedIn.page)
  await signedIn.page.waitForFunction(() => document.querySelector('.agent-activity-icon')?.dataset.state === 'inactive')
  assert.equal(await signedIn.page.getByPlaceholder('module draft').inputValue(), 'keep across identity changes')
  assert.equal(await signedIn.page.evaluate(() => window.draftMounts), mounts)
  assert.deepEqual(signedIn.errors, [])
  await signedIn.context.close()

  for (const username of ['chujiejie.1', 'lixiaolin']) {
    const app = await openApp({}, { realChat: true, auth: { enabled: true, status: 'authenticated', user: { username, email: `${username}@example.test` } } })
    const dock = app.page.getByRole('textbox', { name: '底部对话输入' })
    await dock.waitFor()
    await app.page.waitForFunction(() => !document.querySelector('[aria-label="底部对话输入"]').disabled)
    assert(app.requests.includes('/api/chat/sessions'))
    assert(!app.requests.some(path => path.startsWith('/api/okr-chat/')))
    assert.equal(await app.page.getByText('使用字节身份登录普通对话').count(), 0)
    const switchIdentity = async (username) => {
      Object.assign(app.auth, { status: username ? 'authenticated' : 'unauthenticated', user: username ? { username, email: `${username}@example.test` } : undefined })
      app.okr.user = { open_id: username || 'ordinary', union_id: username || 'ordinary', name: username || '普通用户' }
      await app.page.evaluate(user => window.dispatchEvent(new CustomEvent('jarvis:okr-auth-changed', { detail: { authenticated: true, user: { openId: user.open_id, unionId: user.union_id } } })), app.okr.user)
    }
    await dock.fill('上一账号未发送的草稿')
    await switchIdentity(undefined)
    await app.page.waitForResponse(response => response.url().includes('/api/okr-chat/sessions/cs_test'))
    await app.page.waitForFunction(() => document.querySelector('[aria-label="底部对话输入"]')?.value === '')
    await switchIdentity(username)
    await app.page.waitForResponse(response => response.url().includes('/api/chat/sessions/cs_test'))
    await dock.waitFor()
    if (username === 'chujiejie.1') {
      await app.page.getByRole('button', { name: '退出', exact: true }).click()
    } else {
      await app.page.getByRole('button', { name: `${username}，打开账号菜单` }).click()
      await app.page.locator('.account-menu button').filter({ hasText: '退出登录' }).click()
    }
    await app.page.getByRole('heading', { name: '登录 OKR' }).waitFor()
    assert.equal(await dock.count(), 0)
    assert(!app.requests.includes('/api/auth/login'))
    await app.page.getByRole('button', { name: '使用飞书登录', exact: true }).waitFor()
    await switchIdentity(username)
    app.okr.authenticated = true
    await app.page.evaluate(() => location.hash = '/chat')
    await app.page.locator('.chat-workspace').waitFor()
    assert(!app.requests.includes('/api/auth/login'))
    assert.deepEqual(app.errors, [])
    await app.context.close()
  }
  console.log('PASS: both principals use ordinary chat; account switches reset drafts; either logout clears both surfaces; OKR re-login restores main access')

  const unprotected = await openApp({}, { clock: true })
  await unprotected.page.waitForFunction(() => document.querySelector('.agent-activity-icon')?.dataset.state === 'running')
  await title(unprotected.page, '系统').locator('.ant-badge-status-error').waitFor()
  const counts = principalPaths.map(path => unprotected.requests.filter(request => request === path).length)
  await unprotected.page.clock.fastForward(61_000)
  await unprotected.page.evaluate(() => window.dispatchEvent(new Event('jarvis:plugins-changed')))
  await unprotected.page.waitForFunction(paths => paths.every(path => window.backgroundRequests.filter(request => request.path === path).length >= 2), principalPaths)
  for (const [index, path] of principalPaths.entries()) assert(unprotected.requests.filter(request => request === path).length > counts[index])
  assert.deepEqual(unprotected.errors, [])
  await unprotected.context.close()
  console.log(JSON.stringify({ result: 'passed', checks: ['OKR visitor and shared report skip principal reads', 'no misleading guest activity state', 'principal identity enables background data', 'identity loss cancels reads and timers', 'stale responses ignored', 'identity recovery restarts reads', 'identity failure preserves module draft', 'authentication-disabled installations keep polling'] }))
} catch (error) {
  for (const context of browser.contexts()) for (const page of context.pages()) console.error(await page.locator('body').innerText())
  throw error
} finally {
  await browser.close()
}
