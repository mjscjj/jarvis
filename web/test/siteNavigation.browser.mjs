import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.JARVIS_WEB_URL
const backend = process.env.JARVIS_READ_ONLY_API
assert(base && backend, 'Set JARVIS_WEB_URL and JARVIS_READ_ONLY_API for read-only navigation checks')
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const context = await browser.newContext({ viewport: { width: 1500, height: 1100 } })
const errors = [], checks = [], reads = new Set(), blockedWrites = [], isolatedDrafts = new Map(), sessions = new Map()
const page = await context.newPage()
page.setDefaultTimeout(15000)
page.on('pageerror', error => errors.push({ page: page.url(), error: error.message }))
await context.route('**/api/**', async route => {
  const request = route.request(), url = new URL(request.url())
  if (request.method() === 'PATCH' && /^\/api\/chat\/sessions\/[^/]+$/.test(url.pathname)) {
    const patch = request.postDataJSON()
    assert.deepEqual(Object.keys(patch), ['draft'])
    assert(sessions.has(url.pathname), 'Draft must belong to a previously loaded session')
    isolatedDrafts.set(url.pathname, patch.draft)
    return route.fulfill({ json: { code: 0, data: { ...sessions.get(url.pathname), draft: patch.draft } } })
  }
  if (request.method() !== 'GET') {
    blockedWrites.push({ method: request.method(), path: url.pathname })
    return route.fulfill({ status: 405, json: { code: 405, msg: 'Read-only regression blocks all writes' } })
  }
  reads.add(url.pathname)
  try {
    const response = await fetch(backend + url.pathname + url.search, { signal: AbortSignal.timeout(20000) })
    const body = Buffer.from(await response.arrayBuffer())
    if (!response.ok) errors.push({ page: page.url(), path: url.pathname, status: response.status })
    if (response.ok && /^\/api\/chat\/sessions\/[^/]+$/.test(url.pathname)) {
      const payload = JSON.parse(body.toString())
      sessions.set(url.pathname, payload.data)
      if (isolatedDrafts.has(url.pathname)) payload.data.draft = isolatedDrafts.get(url.pathname)
      return route.fulfill({ json: payload })
    }
    return route.fulfill({ status: response.status, contentType: response.headers.get('content-type') || 'application/json', body })
  } catch (error) {
    errors.push({ page: page.url(), path: url.pathname, error: error.message })
    return route.fulfill({ status: 502, json: { code: 502, msg: 'Read-only upstream request failed' } })
  }
})
const routes = [
  '/chat', '/today', '/work', '/review',
  ...['profile', 'projects', 'persons', 'groups', 'resources', 'key-matters', 'facts', 'world-map'].map(view => `/memory?view=${view}`),
  '/manage/clues', '/manage/automations', '/plugins',
  ...['structure', 'progress', 'relations'].map(tab => `/plugins?plugin=okr&plugin_tab=${tab}&quarter=2026-Q3`),
  '/security', '/agents',
  ...['runtime', 'scheduling', 'memory', 'extensions', 'about'].map(view => `/manage/settings?view=${view}`),
  '/manage/runtime',
]
try {
  for (const route of routes) {
    const previousErrors = errors.length
    await page.goto(`${base}/#${route}`)
    await page.reload()
    await page.waitForLoadState('networkidle')
    const main = page.locator('.ant-layout-content')
    await main.waitFor()
    await page.locator('.ant-spin-spinning').first().waitFor({ state: 'hidden' })
    assert((await main.innerText()).trim().length > 30, `Empty content at ${route}`)
    const alerts = await main.locator('.ant-alert-error').allTextContents()
    if (alerts.length) errors.push({ page: route, alerts: alerts.map(text => text.slice(0, 180)) })
    const check = { route, passed: previousErrors === errors.length, headings: await main.locator('h1,h2').allTextContents() }
    checks.push(check)
    console.log(JSON.stringify(check))
  }
  assert.deepEqual(blockedWrites, [], 'Navigation unexpectedly attempted writes; all were blocked')
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result: 'passed', pages: checks.length, readEndpoints: reads.size, writes: 0 }))
} catch (error) {
  console.error(JSON.stringify({ checks, errors, blockedWrites }))
  throw error
} finally {
  await browser.close()
}
