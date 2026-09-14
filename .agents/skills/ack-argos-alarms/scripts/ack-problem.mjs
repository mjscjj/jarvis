#!/usr/bin/env node

import { spawn } from 'node:child_process'
import fs from 'node:fs'
import net from 'node:net'
import os from 'node:os'
import path from 'node:path'

const ALLOWED_HOSTS = new Set([
  'cloud-i18n.bytedance.net',
  'cloud.tiktok-row.net',
])
const HELP = `Usage: node ack-problem.mjs --url <Argos problem URL> [--url <URL> ...] [--ack]

Without --ack, inspect each Problem without changing it.
With --ack, click the unique enabled ACK button and verify Argos activity logs.

Options:
  --url <url>       Argos collaboration-space Problem URL; repeatable
  --ack             Perform the ACK write
  --profile <name>  Read bytedcli sessions from this profile
  --timeout <ms>    Per-page timeout, default 30000
  --chrome <path>   Chrome/Chromium executable override
  -h, --help        Show this help
`

function fail(message) {
  throw new Error(message)
}

function parseArgs(argv) {
  const options = { urls: [], ack: false, timeout: 30000, profile: process.env.BYTEDCLI_PROFILE || '' }
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index]
    if (arg === '-h' || arg === '--help') return { help: true }
    if (arg === '--ack') { options.ack = true; continue }
    if (['--url', '--profile', '--timeout', '--chrome'].includes(arg)) {
      const value = argv[++index]
      if (!value) fail(`${arg} requires a value`)
      if (arg === '--url') options.urls.push(value)
      if (arg === '--profile') options.profile = value
      if (arg === '--timeout') options.timeout = Number(value)
      if (arg === '--chrome') options.chrome = value
      continue
    }
    fail(`unknown argument: ${arg}`)
  }
  if (options.urls.length === 0) fail('at least one --url is required')
  if (!Number.isInteger(options.timeout) || options.timeout < 1000 || options.timeout > 60000) {
    fail('--timeout must be an integer between 1000 and 60000')
  }
  if (options.profile && !/^[A-Za-z0-9._-]+$/.test(options.profile)) fail('invalid --profile')
  return options
}

function parseProblemUrl(raw) {
  const url = new URL(raw)
  if (url.protocol !== 'https:' || !ALLOWED_HOSTS.has(url.hostname)) fail(`unsupported Argos host: ${url.hostname}`)
  if (!/^\/argos\/alarm\/space\/[0-9a-f]+$/.test(url.pathname)) fail(`unsupported Argos path: ${url.pathname}`)
  const problemId = url.searchParams.get('anomalyId')
  if (!problemId || !/^[0-9a-f]+$/.test(problemId)) fail('Argos URL must contain a valid anomalyId')
  if (url.hostname === 'cloud-i18n.bytedance.net') url.hostname = 'cloud.tiktok-row.net'
  return { url: url.toString(), problemId }
}

function dataDir(profile) {
  const root = path.join(os.homedir(), '.local', 'share', 'bytedcli')
  return profile ? path.join(root, 'profiles', profile, 'data') : path.join(root, 'data')
}

function loadCookies(profile) {
  const root = dataDir(profile)
  const files = ['bytecloud_session.json', 'sso_session.tiktok.json']
  const cookies = []
  for (const filename of files) {
    const file = path.join(root, filename)
    if (!fs.existsSync(file)) fail(`missing bytedcli session: ${file}; run bytedcli --site i18n-tt auth login --session --auto --yes`)
    const parsed = JSON.parse(fs.readFileSync(file, 'utf8'))
    if (!Array.isArray(parsed.cookies)) fail(`invalid bytedcli session: ${file}`)
    cookies.push(...parsed.cookies)
  }
  const unique = new Map()
  for (const cookie of cookies) unique.set(`${cookie.name}|${cookie.domain}|${cookie.path}`, cookie)
  return [...unique.values()].filter((cookie) => !cookie.expiresAt || cookie.expiresAt > Date.now()).map((cookie) => ({
    name: cookie.name,
    value: cookie.value,
    domain: cookie.domain.replace(/^\./, ''),
    path: cookie.path || '/',
    secure: Boolean(cookie.secure),
    httpOnly: Boolean(cookie.httpOnly),
    ...(cookie.expiresAt ? { expires: cookie.expiresAt / 1000 } : {}),
  }))
}

function executableCandidates(override) {
  const candidates = [
    override,
    process.env.BYTEDCLI_BROWSER_EXECUTABLE_PATH,
    process.env.CHROME_PATH,
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
    '/usr/bin/google-chrome',
    '/usr/bin/google-chrome-stable',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
  ].filter(Boolean)
  const cache = path.join(os.homedir(), '.cache', 'ms-playwright')
  if (fs.existsSync(cache)) {
    const entries = fs.readdirSync(cache)
    const preferred = entries.filter((entry) => /^chromium-[0-9]+$/.test(entry)).sort().reverse()
    const fallback = entries.filter((entry) => !preferred.includes(entry)).sort().reverse()
    for (const entry of [...preferred, ...fallback]) {
      candidates.push(path.join(cache, entry, 'chrome-linux64', 'chrome'))
      candidates.push(path.join(cache, entry, 'chrome-linux', 'chrome'))
      candidates.push(path.join(cache, entry, 'chrome-linux', 'headless_shell'))
    }
  }
  return [...new Set(candidates)]
}

function findChrome(override) {
  const executable = executableCandidates(override).find((candidate) => {
    try { fs.accessSync(candidate, fs.constants.X_OK); return true } catch { return false }
  })
  if (!executable) fail('Chrome/Chromium not found; set BYTEDCLI_BROWSER_EXECUTABLE_PATH or pass --chrome')
  return executable
}

async function freePort() {
  const server = net.createServer()
  await new Promise((resolve, reject) => { server.once('error', reject); server.listen(0, '127.0.0.1', resolve) })
  const port = server.address().port
  await new Promise((resolve) => server.close(resolve))
  return port
}

async function waitFor(predicate, timeout, label) {
  const deadline = Date.now() + timeout
  let lastError
  while (Date.now() < deadline) {
    try {
      const value = await predicate()
      if (value) return value
    } catch (error) { lastError = error }
    await new Promise((resolve) => setTimeout(resolve, 250))
  }
  fail(`${label} timed out${lastError ? `: ${lastError.message}` : ''}`)
}

class Cdp {
  constructor(socket) {
    this.socket = socket
    this.nextId = 0
    this.pending = new Map()
    socket.addEventListener('message', (event) => {
      const message = JSON.parse(event.data)
      if (!message.id || !this.pending.has(message.id)) return
      const { resolve, reject } = this.pending.get(message.id)
      this.pending.delete(message.id)
      if (message.error) reject(new Error(JSON.stringify(message.error)))
      else resolve(message.result)
    })
  }

  call(method, params = {}, sessionId) {
    return new Promise((resolve, reject) => {
      const id = ++this.nextId
      this.pending.set(id, { resolve, reject })
      this.socket.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }))
    })
  }
}

async function launchBrowser(chrome, timeout) {
  const port = await freePort()
  const userDataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'argos-ack-'))
  const args = [
    '--headless', '--disable-gpu',
    `--remote-debugging-port=${port}`, `--user-data-dir=${userDataDir}`, 'about:blank',
  ]
  if (typeof process.getuid === 'function' && process.getuid() === 0) args.unshift('--no-sandbox')
  const child = spawn(chrome, args, { stdio: 'ignore' })
  const version = await waitFor(async () => {
    const response = await fetch(`http://127.0.0.1:${port}/json/version`)
    return response.ok ? response.json() : null
  }, Math.min(timeout, 10000), 'Chrome DevTools startup')
  const socket = new WebSocket(version.webSocketDebuggerUrl)
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true })
    socket.addEventListener('error', reject, { once: true })
  })
  return { child, cdp: new Cdp(socket), socket, userDataDir }
}

async function createPage(cdp, cookies) {
  const target = await cdp.call('Target.createTarget', { url: 'about:blank' })
  const attached = await cdp.call('Target.attachToTarget', { targetId: target.targetId, flatten: true })
  const sessionId = attached.sessionId
  await cdp.call('Network.enable', {}, sessionId)
  await cdp.call('Page.enable', {}, sessionId)
  await cdp.call('Runtime.enable', {}, sessionId)
  await cdp.call('Page.setLifecycleEventsEnabled', { enabled: true }, sessionId)
  await cdp.call('Network.setCookies', { cookies }, sessionId)
  return { targetId: target.targetId, sessionId }
}

async function evaluate(cdp, sessionId, expression) {
  const result = await cdp.call('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true }, sessionId)
  if (result.exceptionDetails) {
    const exception = result.exceptionDetails.exception?.description || result.exceptionDetails.exception?.value
    fail(exception || result.exceptionDetails.text || 'browser evaluation failed')
  }
  return result.result.value
}

const PAGE_STATE = `(() => {
  const drawer = document.querySelector('.arco-volc3-drawer-content');
  const text = drawer?.innerText || '';
  const buttons = drawer ? [...drawer.querySelectorAll('button')] : [];
  const enabledAckButtons = buttons.filter((button) => ['Ack', '确认', '确认问题'].includes(button.innerText.trim()) && !button.disabled);
  const header = drawer?.querySelector('.top-0.bg-white')?.innerText || text.slice(0, 1000);
  const alreadyAcked = header.split('\\n').some((line) => ['Acked', '已确认'].includes(line.trim())) || text.includes('Acked Problem') || text.includes('确认了报警');
  return {
    href: location.href, title: document.title, body: document.body?.innerText.slice(0, 500) || '',
    drawer: Boolean(drawer), drawerText: text.slice(0, 4000),
    enabledAckCount: enabledAckButtons.length, alreadyAcked,
    activityAcked: text.includes('Acked Problem') || text.includes('确认了报警'),
  };
})()`

async function inspectProblem(cdp, sessionId, target, timeout, execute) {
  await cdp.call('Page.navigate', { url: target.url }, sessionId)
  let observed
  let before
  try {
    before = await waitFor(async () => {
      const state = await evaluate(cdp, sessionId, PAGE_STATE)
      observed = state
      if (new URL(state.href).hostname.includes('sso.')) fail('Argos browser session is not authenticated; refresh it with bytedcli auth login --session')
      return state.drawer && (state.alreadyAcked || state.enabledAckCount === 1) ? state : null
    }, timeout, `load Argos Problem ${target.problemId}`)
  } catch (error) {
    if (observed && !error.message.includes('last page:')) {
      fail(`${error.message}; last page: ${observed.title} ${observed.href}; body: ${observed.body}`)
    }
    throw error
  }
  const actualId = new URL(before.href).searchParams.get('anomalyId')
  if (actualId !== target.problemId) fail(`loaded Problem ${actualId || '<none>'}, expected ${target.problemId}`)
  if (before.alreadyAcked) return { problem_id: target.problemId, status: 'already_acked', url: before.href }
  if (!execute) return { problem_id: target.problemId, status: 'unacked', url: before.href }
  if (before.enabledAckCount !== 1) fail(`Problem ${target.problemId} has ${before.enabledAckCount} enabled ACK buttons; expected exactly one`)
  const clicked = await evaluate(cdp, sessionId, `(() => {
    const drawer = document.querySelector('.arco-volc3-drawer-content');
    const buttons = [...drawer.querySelectorAll('button')].filter((button) => ['Ack', '确认', '确认问题'].includes(button.innerText.trim()) && !button.disabled);
    if (buttons.length !== 1) return false;
    buttons[0].click();
    return true;
  })()` )
  if (!clicked) fail(`Problem ${target.problemId} ACK button disappeared before click`)
  const after = await waitFor(async () => {
    const state = await evaluate(cdp, sessionId, PAGE_STATE)
    return state.activityAcked ? state : null
  }, timeout, `verify Argos Problem ${target.problemId} ACK`)
  return { problem_id: target.problemId, status: 'acked', url: after.href }
}

async function main() {
  const options = parseArgs(process.argv.slice(2))
  if (options.help) { process.stdout.write(HELP); return }
  const targetsById = new Map(options.urls.map(parseProblemUrl).map((item) => [item.problemId, item]))
  const cookies = loadCookies(options.profile)
  const browser = await launchBrowser(findChrome(options.chrome), options.timeout)
  let page
  try {
    page = await createPage(browser.cdp, cookies)
    const results = []
    for (const target of targetsById.values()) {
      results.push(await inspectProblem(browser.cdp, page.sessionId, target, options.timeout, options.ack))
    }
    process.stdout.write(`${JSON.stringify({ ok: true, mode: options.ack ? 'ack' : 'inspect', results }, null, 2)}\n`)
  } finally {
    if (page) await browser.cdp.call('Target.closeTarget', { targetId: page.targetId }).catch(() => {})
    browser.socket.close()
    browser.child.kill('SIGTERM')
    await new Promise((resolve) => {
      const timer = setTimeout(resolve, 2000)
      browser.child.once('exit', () => { clearTimeout(timer); resolve() })
    })
    try { fs.rmSync(browser.userDataDir, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 }) } catch {}
  }
}

main().catch((error) => {
  process.stderr.write(`${JSON.stringify({ ok: false, error: error.message })}\n`)
  process.exitCode = 1
})
