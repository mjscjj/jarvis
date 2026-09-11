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
let pending = []
const ready = () => ({ onboarding_required: true, runtime_id: 'runtime', app_ready: true, completed: true, world_model_ready: true,
  configuration: { machine_configuration_ready: true, agent_name_configured: true },
  lark: { available: true, app_id: 'app_test', credential_available: true, bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } },
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
      loginCalls++; status = ready(); hold = true
      await route.fulfill({ json: { code: 0, data: { id: 'test-flow', status: 'success' } } })
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
  status = ready()
  await page.getByRole('button', { name: '重新检查', exact: true }).click()
  await workspace.waitFor()
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result: 'passed', checks: ['installed app before full check', 'background expiry notice', 'draft survives checks and repair', 'authorization rechecks credentials', 'network error and retry', 'first install waits for full check'], statusCalls }))
} finally { release(); await browser.close() }
