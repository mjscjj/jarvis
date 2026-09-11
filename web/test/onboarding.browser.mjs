// Isolated fixtures: no real login, configuration writes, or Agent turns.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:5189'
const source = await (await fetch(`${base}/src/Onboarding.tsx`)).text()
const reactPath = source.match(/from ["']([^"']*react\.js[^"']*)["']/)[1]
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', error => errors.push(error.message))
let installed = true
let hold = true
let httpError = false
let statusCalls = 0
let loginCalls = 0
let finalizeCalls = 0
let pending = []
let pendingLogin = false
let holdLogin = false
let finishLogin
let holdCancel = false
let finishCancel
let cancelCalls = 0
let currentFlow
const flowStates = new Map()
const ready = () => ({ runtime_id: 'runtime', app_ready: true, completed: true, world_model_ready: true,
  configuration: { machine_configuration_ready: true, agent_name_configured: true },
  lark: { available: true, app_id: 'cli_test', app_name: '测试飞书助手', application_checks: [{ event: 'im.message.receive_v1', ready: true }, { event: 'card.action.trigger', ready: true }], credential_available: true, bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } },
  agent: { available: true, authenticated: true } })
let status = ready()
const release = () => { hold = false; for (const resolve of pending) resolve(); pending = [] }
try {
  await page.route('**/api/**', async route => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/setup/bootstrap') {
      await route.fulfill({ json: { code: 0, data: { machine_configuration_ready: installed } } })
    } else if (path === '/api/setup/status') {
      statusCalls++
      if (hold) await new Promise(resolve => pending.push(resolve))
      await route.fulfill(httpError
        ? { status: 500, json: { code: 500, msg: '连接检查暂时失败' } }
        : { json: { code: 0, data: status } })
    } else if (path === '/api/setup/lark/login') {
      loginCalls++;
      if (pendingLogin) {
        const id = `flow-${loginCalls}`
        if (holdLogin) await new Promise(resolve => { finishLogin = resolve })
        currentFlow = id
        flowStates.set(id, 'pending')
        await route.fulfill({ json: { code: 0, data: { id, status: 'pending', verification_url: `https://example.test/${id}` } } })
        return
      }
      if (installed) status = ready()
      else { status.lark.user.verified = true; status.lark.user.status = 'ready' }
      hold = true
      await route.fulfill({ json: { code: 0, data: { id: 'test-flow', status: 'success' } } })
    } else if (path.startsWith('/api/setup/flows/')) {
      const id = path.split('/')[4]
      if (path.endsWith('/cancel')) {
        cancelCalls++
        if (holdCancel) await new Promise(resolve => { finishCancel = resolve })
        flowStates.set(id, 'failed')
      }
      await route.fulfill({ json: { code: 0, data: { id, status: flowStates.get(id), verification_url: `https://example.test/${id}` } } })
    } else if (path === '/api/setup/lark/permissions') {
      await route.fulfill({ json: { code: 0, data: { scopes: { tenant: ['im:message:readonly'], user: ['minutes:minutes.artifacts:read'] } } } })
    } else if (path === '/api/setup/finalize') {
      finalizeCalls++
      const input = route.request().postDataJSON()
      assert.equal(input.app_secret, finalizeCalls === 1 ? 'wrong-secret' : 'correct-secret')
      if (finalizeCalls === 1) await route.fulfill({ status: 400, json: { code: 40042, msg: 'App ID 与 App Secret 验证未通过' } })
      else {
        status = ready(); status.runtime_id = 'after-restart'
        await route.fulfill({ json: { code: 0, data: { ...status, runtime_id: 'runtime', app_ready: false } } })
      }
    } else throw new Error(`Unexpected API: ${path}`)
  })
  await page.route('**/__onboarding-test', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<!doctype html><meta charset="utf-8"><div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window); window.$RefreshReg$=()=>{}; window.$RefreshSig$=()=>t=>t; window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${reactPath}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {OnboardingGate}=await import('/src/Onboarding.tsx');
    await import('/src/styles.css');
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(React.StrictMode,null,
      React.createElement(OnboardingGate,null,React.createElement('main',{'aria-label':'工作区'},React.createElement('input',{'aria-label':'工作草稿'})))));
  </script>` }))
  await page.goto(`${base}/__onboarding-test`)
  const workspace = page.getByRole('main', { name: '工作区' })
  const draft = page.getByRole('textbox', { name: '工作草稿' })
  await workspace.waitFor()
  assert.equal(hold, true, 'workspace must open before the full check returns')
  await draft.fill('不能丢失的草稿')
  status.app_ready = false; status.lark.user.verified = false; status.lark.error = '飞书授权已失效'
  release()
  await page.locator('.setup-connection-notice').waitFor()
  assert.equal(await draft.inputValue(), '不能丢失的草稿')
  await page.getByRole('button', { name: '处理连接', exact: true }).click()
  await page.getByRole('button', { name: '授权飞书账号', exact: true }).click()
  await page.waitForFunction(() => document.body.textContent.includes('正在打开连接页面'))
  assert.equal(loginCalls, 1)
  assert.ok(statusCalls >= 2, 'successful authorization must run a full check again')
  assert.equal(await draft.inputValue(), '不能丢失的草稿')
  release()
  await page.locator('.setup-connection-notice').waitFor({ state: 'detached' })
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  assert.equal(await draft.inputValue(), '不能丢失的草稿')

  // A failed background request is visible without blocking the installed app.
  httpError = true
  await page.reload()
  await workspace.waitFor()
  await page.locator('.setup-connection-notice').getByText('连接检查：连接检查暂时失败').waitFor()
  httpError = false
  await page.getByRole('button', { name: '重新检查', exact: true }).click()
  await page.locator('.setup-connection-notice').waitFor({ state: 'detached' })

  // A first installation still waits for real connection validation.
  installed = false; hold = true
  status = ready(); status.app_ready = false; status.configuration.machine_configuration_ready = false; status.lark.user.verified = false
  await page.reload()
  await page.getByText('正在检查已有配置…', { exact: true }).waitFor()
  assert.equal(await workspace.count(), 0)
  release()
  await page.getByRole('button', { name: '授权飞书账号', exact: true }).waitFor()
  assert.equal(await workspace.count(), 0)
  const identity = page.getByRole('region', { name: '当前飞书应用' })
  await identity.getByText('正在连接的飞书助手：测试飞书助手').waitFor()
  assert.match(await identity.innerText(), /cli_test/)
  assert.equal(await identity.getByRole('link').getAttribute('href'), 'https://open.feishu.cn/app/cli_test')
  assert.equal(await page.locator('#setup-secret').count(), 0)

  // Missing events are repaired in the console, without asking for a secret.
  status.lark.application_checks[1] = { event: 'card.action.trigger', ready: false, error: 'console_event_published missing; event not published' }
  await page.getByRole('button', { name: '重新检查', exact: true }).click()
  await page.getByText('未就绪：card.action.trigger', { exact: true }).waitFor()
  assert.equal(await page.locator('#setup-secret').count(), 0)
  await page.getByText('查看具体检查结果', { exact: true }).click()
  await page.getByText('console_event_published missing; event not published', { exact: true }).waitFor()
  await page.getByRole('button', { name: '查看完整权限配置', exact: true }).click()
  assert.match(await page.getByRole('textbox', { name: '完整权限配置' }).inputValue(), /minutes:minutes.artifacts:read/)

  status.lark.application_checks[1].ready = true
  status.lark.application_checks[1].error = undefined
  status.lark.credential_available = false
  await page.getByRole('button', { name: '重新检查', exact: true }).click()
  await page.getByRole('button', { name: '授权飞书账号', exact: true }).click()
  await page.waitForFunction(() => document.body.textContent.includes('正在打开连接页面'))
  release()
  const secret = page.getByLabel('请填写「测试飞书助手」的应用密钥（App Secret）', { exact: true })
  await secret.waitFor()
  await secret.fill('wrong-secret')
  await page.getByRole('button', { name: '验证并开始使用', exact: true }).click()
  await page.getByText('App ID 与 App Secret 验证未通过', { exact: true }).waitFor()
  assert.equal(await secret.inputValue(), 'wrong-secret')
  assert.equal(await workspace.count(), 0)
  assert.match(await identity.innerText(), /cli_test/)
  await secret.fill('correct-secret')
  await page.getByRole('button', { name: '重试并继续', exact: true }).click()
  await workspace.waitFor()
  assert.equal(finalizeCalls, 2)

  // Regeneration is one begin request; the backend owns replacing the old flow.
  installed = false; pendingLogin = true
  status = ready(); status.app_ready = false; status.configuration.machine_configuration_ready = false; status.lark.user.verified = false
  await page.reload()
  const authorize = page.getByRole('button', { name: '授权飞书账号', exact: true })
  await authorize.click()
  const connection = page.locator('a[href^="https://example.test/flow-"]')
  await connection.waitFor()
  const originalFlow = currentFlow
  const beforeRegenerate = loginCalls
  holdLogin = true
  const beginRequest = page.waitForRequest('**/api/setup/lark/login')
  await page.getByRole('button', { name: '重新生成连接', exact: true }).evaluate(button => { button.click(); button.click() })
  await beginRequest
  assert.equal(await authorize.isDisabled(), true)
  assert.equal(loginCalls, beforeRegenerate + 1)
  assert.equal(cancelCalls, 0, 'regeneration must not also send cancellation')
  holdLogin = false; finishLogin()
  await connection.waitFor()
  assert.notEqual(currentFlow, originalFlow)
  assert.equal(await connection.getAttribute('href'), `https://example.test/${currentFlow}`)

  // An old successful poll waiting on /status cannot clear its replacement.
  const oldFlow = currentFlow
  hold = true
  const oldStatusRequest = page.waitForRequest('**/api/setup/status')
  flowStates.set(oldFlow, 'success')
  await oldStatusRequest
  await page.getByRole('button', { name: '重新生成连接', exact: true }).click()
  await connection.waitFor()
  assert.notEqual(currentFlow, oldFlow)
  const newFlow = currentFlow
  const oldStatusResponse = page.waitForResponse('**/api/setup/status')
  release()
  await oldStatusResponse
  // Observe a subsequent poll: a cleared replacement would stop polling.
  await page.waitForRequest(`**/api/setup/flows/${newFlow}`)
  assert.equal(await connection.getAttribute('href'), `https://example.test/${newFlow}`)

  // Explicit cancellation keeps the begin button locked until it completes.
  holdCancel = true
  const cancelRequest = page.waitForRequest(`**/api/setup/flows/${newFlow}/cancel`)
  await page.getByRole('button', { name: '返回', exact: true }).click()
  await cancelRequest
  assert.equal(await authorize.isDisabled(), true)
  holdCancel = false; finishCancel()
  await page.getByText('正在取消连接…', { exact: true }).waitFor({ state: 'detached' })
  assert.equal(await authorize.isDisabled(), false)
  assert.equal(cancelCalls, 1)
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result: 'passed', checks: ['installed app before full check', 'background expiry notice', 'draft survives checks and repair', 'authorization rechecks credentials', 'network error and retry', 'first install waits for full check', 'application identity before secret', 'event repair without secret', 'complete permission config visible', 'OAuth before chat secret', 'failed secret retained and retry succeeds', 'single regeneration request', 'stale poll cannot clear replacement', 'cancel locks new authorization'], statusCalls }))
} catch (error) {
  console.error(JSON.stringify({ errors, body: await page.locator('body').innerText(), links: await page.locator('a').evaluateAll(links => links.map(link => ({ text: link.textContent, role: link.getAttribute('role'), href: link.getAttribute('href') }))) }))
  throw error
} finally { release(); finishLogin?.(); finishCancel?.(); await browser.close() }
