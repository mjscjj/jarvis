// Isolated browser regression: every API call is a fixture and the Tauri IPC is
// stubbed, so no real update is ever checked, downloaded or installed.
// Start Vite, then run with PLAYWRIGHT_MODULE and CHROME_EXECUTABLE if needed.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:18801'
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 900 } })
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', error => errors.push(error.message))
const session = { id: 's1', title: '测试对话', agent: 'trae', model: 'test', reasoning_effort: 'medium', sources: [], draft: {}, archived: false, messages: [], pending_attachments: [], created_at: new Date().toISOString(), updated_at: new Date().toISOString() }
const checkFailure = '更新源不可用：dns 解析失败'
const installFailure = '签名校验失败：manifest signature mismatch'

let active = page

try {
  const routeApi = async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()
    if (path === '/api/chat/sessions/s1' && method === 'PATCH') Object.assign(session, route.request().postDataJSON())
    else assert.equal(method, 'GET', `Unexpected mutation: ${path}`)
    let data
    if (path === '/api/agent-identity') data = { display_name: 'Jarvis' }
    else if (path === '/api/auth/status') data = { status: 'authenticated', user: { username: 'Test', email: 'test@example.test' } }
    else if (path === '/api/setup/bootstrap') data = { machine_configuration_ready: true }
    else if (path === '/api/setup/status') data = {
      app_ready: true, completed: true, world_model_ready: true,
      configuration: { machine_configuration_ready: true, agent_name_configured: true },
      lark: { available: true, credential_available: true, bot: { verified: true }, user: { verified: true } },
      agent: { available: true, authenticated: true },
    }
    else if (['/api/plugin-installations', '/api/debug/failures', '/api/projects'].includes(path)) data = { items: [] }
    else if (path === '/api/chat/sessions') data = { items: [session] }
    else if (path === '/api/chat/sessions/s1') data = session
    else if (path === '/api/chat/agents') data = { items: [{ id: 'trae', name: 'TRAE', available: true, default: true }] }
    else if (path === '/api/chat/agents/trae/models') data = { items: [{ id: 'test', name: 'Test', default: true }] }
    else if (path === '/api/tasks') data = { items: [], total: 0, page: 1, page_size: Number(url.searchParams.get('page_size')) }
    else {
      errors.push(`Unexpected API: ${path}`)
      await route.fulfill({ status: 500, json: { code: 500, msg: `Unexpected API: ${path}` } })
      return
    }
    await route.fulfill({ json: { code: 0, data } })
  }
  await page.route('**/api/**', routeApi)

  // Every Tauri call funnels through __TAURI_INTERNALS__.invoke, so the whole
  // updater surface is reachable from one stub. The plan lives in localStorage
  // to survive the reloads that exercise the startup check.
  await page.addInitScript(() => {
    if (!localStorage.getItem('__tauriPlan')) {
      localStorage.setItem('__tauriPlan', JSON.stringify({ version: '0.1.5', next: '0.2.0', check: 'latest', hold: true }))
    }
    let rid = 0
    let nextCallbackId = 0
    const callbacks = new Map()
    window.__tauriCalls = []
    window.__TAURI_INTERNALS__ = {
      transformCallback: (callback, once) => {
        const id = ++nextCallbackId
        callbacks.set(id, { callback, once })
        return id
      },
      unregisterCallback: id => callbacks.delete(id),
      invoke: async cmd => {
        window.__tauriCalls.push(cmd)
        const plan = JSON.parse(localStorage.getItem('__tauriPlan'))
        if (cmd === 'plugin:app|version') return plan.version
        if (cmd === 'plugin:resources|close' || cmd === 'plugin:process|restart') return null
        if (cmd === 'plugin:updater|check') {
          if (plan.hold) await new Promise(resolve => { window.__tauriRelease = resolve })
          if (plan.check === 'error') throw new Error(plan.checkError)
          if (plan.check === 'latest') return null
          return { rid: ++rid, currentVersion: plan.version, version: plan.next, body: '修复若干问题', rawJson: {} }
        }
        if (cmd === 'plugin:updater|download_and_install') {
          if (plan.installError) throw new Error(plan.installError)
          return null
        }
        throw new Error(`Unexpected Tauri command: ${cmd}`)
      },
    }
  })
  const setPlan = patch => page.evaluate(patch => {
    const plan = JSON.parse(localStorage.getItem('__tauriPlan') || '{}')
    localStorage.setItem('__tauriPlan', JSON.stringify({ ...plan, ...patch }))
  }, patch)
  const checkCalls = () => page.evaluate(() => window.__tauriCalls.filter(cmd => cmd === 'plugin:updater|check').length)
  const installCalls = () => page.evaluate(() => window.__tauriCalls.filter(cmd => cmd === 'plugin:updater|download_and_install').length)
  const release = () => page.evaluate(() => window.__tauriRelease())
  const checkButton = page.getByRole('button', { name: '检查更新' })
  const dialog = page.getByRole('dialog')
  const noDialog = async () => {
    await page.waitForTimeout(700)
    assert.equal(await dialog.count(), 0, 'The unattended probe must not open a window')
  }

  await page.goto(base)

  // The brand icon opens Jarvis controls; version information is one explicit action.
  await page.locator('.agent-activity-icon').click()
  await page.getByRole('button', { name: '关于与更新' }).click()
  await page.getByRole('heading', { name: '关于 Jarvis' }).waitFor()
  assert.match(page.url(), /#\/manage\/settings\?view=about$/)
  await page.getByText('0.1.5', { exact: true }).waitFor()
  await page.getByText('正在检查更新…').waitFor()
  assert.equal(await page.locator('button.ant-btn-loading').count(), 1, 'Checking keeps the button busy')
  await release()
  await page.getByText('已是最新版本').waitFor()
  await page.getByText(/检查时间 \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/).waitFor()

  // A repeated manual check must really hit the updater, not just redraw.
  await setPlan({ hold: false })
  const beforeRepeat = await checkCalls()
  await checkButton.click()
  await page.waitForFunction(count => window.__tauriCalls.filter(cmd => cmd === 'plugin:updater|check').length > count, beforeRepeat)
  await page.getByText(/检查时间 \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}/).waitFor()

  // A failed check used to reach console.error only.
  await setPlan({ check: 'error', checkError: checkFailure })
  await checkButton.click()
  await page.getByText('检查更新失败').waitFor()
  await page.getByText(checkFailure).waitFor()
  await noDialog()

  // A manual check reports the new version in place; the window opens on demand.
  await setPlan({ check: 'update' })
  await checkButton.click()
  await page.getByText('发现新版本 0.2.0').waitFor()
  await noDialog()
  await page.getByRole('button', { name: '查看并升级' }).click()
  await dialog.getByText('有新版本 0.2.0').waitFor()

  await setPlan({ installError: installFailure })
  const beforeInstall = await installCalls()
  await dialog.getByRole('button', { name: '立即升级' }).click()
  await dialog.getByText('升级失败').waitFor()
  await dialog.getByText(installFailure).waitFor()
  assert.equal(await installCalls(), beforeInstall + 1)

  // Dismissing the window keeps the failure visible on 关于 instead of losing it.
  await dialog.getByRole('button', { name: '稍后再说' }).click()
  await page.waitForFunction(() => !document.querySelector('.ant-modal-wrap'))
  await page.getByText('升级失败').waitFor()
  await page.getByRole('button', { name: '重新打开升级窗口' }).waitFor()

  // A fresh check must not reopen the window carrying the previous failure.
  await setPlan({ installError: '' })
  await checkButton.click()
  await page.getByText('发现新版本 0.2.0').waitFor()
  await page.getByRole('button', { name: '查看并升级' }).click()
  await dialog.getByText('有新版本 0.2.0').waitFor()
  assert.equal(await dialog.getByText('升级失败').count(), 0, 'Reopening must not replay the last failure')
  assert.equal(await dialog.getByText(installFailure).count(), 0)
  assert.equal(await dialog.getByRole('button', { name: '重试' }).count(), 0)
  await dialog.getByRole('button', { name: '稍后再说' }).click()
  await page.waitForFunction(() => !document.querySelector('.ant-modal-wrap'))

  // Startup: a failed probe stays silent, an available version prompts.
  await setPlan({ check: 'error' })
  await page.reload()
  await noDialog()
  await page.locator('.agent-activity-icon').click()
  await page.getByText('检查更新失败').waitFor()
  await page.getByText(checkFailure).waitFor()

  await setPlan({ check: 'update' })
  await page.reload()
  await page.getByRole('dialog').getByText('有新版本 0.2.0').waitFor()

  // Same UI opened straight in a browser: no Tauri shell, so no version and no
  // check entry, and the unattended probe never runs.
  const browserPage = await browser.newPage({ viewport: { width: 1280, height: 900 } })
  active = browserPage
  browserPage.on('pageerror', error => errors.push(error.message))
  browserPage.setDefaultTimeout(8000)
  await browserPage.route('**/api/**', routeApi)
  await browserPage.goto(`${base}#/manage/settings?view=about`)
  await browserPage.getByText('当前通过浏览器访问本机服务').waitFor()
  assert.equal(await browserPage.getByRole('button', { name: '检查更新' }).count(), 0)
  assert.equal(await browserPage.getByText('0.1.5', { exact: true }).count(), 0)
  assert.equal(await browserPage.getByRole('dialog').count(), 0)

  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result: 'passed', checks: ['icon menu opens 设置 → 关于', 'current version', 'checking state', 'already latest with timestamp', 'repeated check reaches updater', 'check failure shows raw error', 'available reports in place', 'prompt on demand', 'install failure shows raw error', 'failure survives dismiss', 'reopened prompt is clean', 'silent startup failure', 'startup prompt', 'browser fallback'] }))
} catch (error) {
  console.error(JSON.stringify({ errors, body: (await active.locator('body').innerText()).slice(0, 2200) }))
  throw error
} finally {
  await browser.close()
}
