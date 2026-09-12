// Run against Vite with PLAYWRIGHT_MODULE / CHROME_EXECUTABLE when needed.
// Mounts the real app; every API call is intercepted, with no Agent or data mutations.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:18801'
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2 })
page.setDefaultTimeout(8000)
const errors = []
page.on('pageerror', error => errors.push(error.message))
let tasks = []
let fail = false
let hold = false
let release
let activityCalls = 0
const session = { id: 's1', title: '测试对话', agent: 'trae', model: 'test', reasoning_effort: 'medium', sources: [], draft: {}, archived: false, messages: [], pending_attachments: [], created_at: new Date().toISOString(), updated_at: new Date().toISOString() }

try {
  await page.route('**/api/**', async route => {
    const url = new URL(route.request().url())
    const path = url.pathname
    const method = route.request().method()
    if (path === '/api/chat/sessions/s1' && method === 'PATCH') {
      Object.assign(session, route.request().postDataJSON())
    } else assert.equal(method, 'GET', `Unexpected mutation: ${path}`)
    let data
    if (path === '/api/agent-identity') data = { display_name: 'Jarvis' }
    else if (path === '/api/auth/status') data = { enabled: false, status: 'authenticated', user: { name: 'Test', email: 'test@example.test' } }
    else if (path === '/api/app-modules') data = { items: [] }
    else if (path === '/api/biz-okr/me') data = { authenticated: false, configured: false, management_access: false }
    else if (path === '/api/setup/bootstrap') data = { machine_configuration_ready: true }
    else if (path === '/api/setup/status') data = {
      app_ready: true, completed: true, world_model_ready: true,
      configuration: { machine_configuration_ready: true, agent_name_configured: true },
      lark: { available: true, application_checks: [{ event: 'im.message.receive_v1', ready: true }, { event: 'card.action.trigger', ready: true }], credential_available: true, bot: { verified: true }, user: { verified: true } },
      agent: { available: true, authenticated: true },
    }
    else if (['/api/plugin-installations', '/api/debug/failures', '/api/projects'].includes(path)) data = { items: [] }
    else if (path === '/api/chat/sessions') data = { items: [session] }
    else if (path === '/api/chat/sessions/s1') data = session
    else if (path === '/api/chat/agents') data = { items: [{ id: 'trae', name: 'TRAE', available: true, default: true }] }
    else if (path === '/api/chat/agents/trae/models') data = { items: [{ id: 'test', name: 'Test', default: true }] }
    else if (path === '/api/tasks') {
      const size = Number(url.searchParams.get('page_size'))
      if (size === 1) {
        activityCalls++
        assert.equal(url.searchParams.get('status'), 'executing')
        assert.equal(url.searchParams.get('page'), '1')
        if (hold) await new Promise(resolve => { release = resolve })
        if (fail) { await route.fulfill({ status: 500, json: { code: 500, msg: '测试连接失败' } }); return }
      }
      const filtered = tasks.filter(task => url.searchParams.get('status').split(',').includes(task.status))
      data = { items: filtered.slice(0, size), total: filtered.length, page: 1, page_size: size }
    } else {
      errors.push(`Unexpected API: ${path}`)
      await route.fulfill({ status: 500, json: { code: 500, msg: `Unexpected API: ${path}` } })
      return
    }
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.goto(base)
  const icon = page.locator('.agent-activity-icon')
  const waitState = state => page.waitForFunction(state => document.querySelector('.agent-activity-icon')?.dataset.state === state, state)
  const refresh = () => page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')))
  const visibility = hidden => page.evaluate(hidden => {
    Object.defineProperty(document, 'hidden', { configurable: true, value: hidden })
    document.dispatchEvent(new Event('visibilitychange'))
  }, hidden)
  const setTasks = async next => { tasks = next; await refresh() }
  await waitState('idle')
  const idleBox = await icon.boundingBox()
  assert.equal(idleBox.width, 44)
  assert.equal(idleBox.height, 44)
  assert.equal(await icon.locator('img').evaluate(el => getComputedStyle(el).filter), 'none')

  // Poll discovers execution without visiting the task page.
  tasks = [{ id: 1, status: 'executing', source_type: 'manual' }]
  await waitState('running')
  assert.match(await icon.getAttribute('aria-label'), /正在执行 1 个任务/)
  await page.waitForFunction(() => getComputedStyle(document.querySelector('.agent-activity-particles')).opacity === '1')
  assert.deepEqual(await icon.boundingBox(), idleBox, 'Animation must not shift layout')
  const orbit = page.locator('.agent-activity-swarm').first()
  const transform = await orbit.evaluate(el => getComputedStyle(el).transform)
  await page.waitForTimeout(250)
  assert.notEqual(await orbit.evaluate(el => getComputedStyle(el).transform), transform)
  if (process.env.ACTIVITY_TEST_SCREENSHOT) await page.screenshot({ path: process.env.ACTIVITY_TEST_SCREENSHOT })

  await setTasks([{ id: 1, status: 'executing' }, { id: 2, status: 'executing', source_type: 'scheduled_task' }, { id: 3, status: 'pending' }])
  await page.getByRole('button', { name: 'Jarvis：正在执行 2 个任务，点击查看版本与更新' }).waitFor()
  assert.equal(await orbit.evaluate(el => getComputedStyle(el).animationDuration), '8s')
  await icon.hover()
  // Becoming interactive must not drop the execution state the icon reports.
  await page.getByRole('tooltip', { name: '正在执行 2 个任务 · 点击查看版本与更新' }).waitFor()
  await page.getByRole('button', { name: '修改机器人名称' }).click()
  await page.getByRole('textbox', { name: '机器人名称' }).fill('测试名称')
  await page.getByRole('textbox', { name: '机器人名称' }).press('Escape')
  assert.deepEqual(await icon.boundingBox(), idleBox)
  await page.locator('.sider-collapse-btn').click()
  await page.waitForTimeout(400)
  const collapsed = await icon.boundingBox()
  assert.equal(collapsed.width, 44)
  const sider = await page.locator('.app-sider').boundingBox()
  assert.ok(collapsed.x - 7 >= sider.x && collapsed.x + 51 <= sider.x + sider.width, 'Particles fit collapsed sidebar')
  await page.locator('.sider-collapse-btn').click()

  await page.emulateMedia({ reducedMotion: 'reduce' })
  assert.equal(await orbit.evaluate(el => getComputedStyle(el).animationName), 'none')
  assert.equal(await page.locator('.agent-activity-spark').first().evaluate(el => getComputedStyle(el).animationName), 'none')
  assert.equal(await icon.locator('img').evaluate(el => getComputedStyle(el).transform), 'none')
  await page.emulateMedia({ reducedMotion: 'no-preference' })

  for (const status of ['done', 'failed', 'waiting', 'needs_human', 'pending']) {
    await setTasks([{ id: 1, status, source_type: 'scheduled_task' }])
    await waitState('idle')
    assert.match(await icon.getAttribute('aria-label'), /暂无执行中的任务/)
  }
  await page.waitForFunction(() => getComputedStyle(document.querySelector('.agent-activity-particles')).opacity === '0')
  await setTasks([{ id: 1, status: 'executing', source_type: 'scheduled_task' }])
  await waitState('running')
  fail = true
  await refresh()
  await waitState('error')
  await page.locator('.agent-activity-error').waitFor()
  assert.match(await icon.getAttribute('aria-label'), /任务状态读取失败/)
  assert.equal(await orbit.evaluate(el => getComputedStyle(el).animationPlayState), 'paused')
  fail = false
  await waitState('running')

  await visibility(true)
  const pausedCalls = activityCalls
  await page.waitForTimeout(3300)
  assert.equal(activityCalls, pausedCalls, 'Hidden page must stop polling')
  await visibility(false)
  await page.waitForFunction(() => document.querySelector('.agent-activity-icon')?.dataset.state === 'running')
  await page.waitForTimeout(100)
  assert.equal(activityCalls, pausedCalls + 1, 'Visible page refreshes immediately')

  // Late failure from an aborted request must not overwrite a fresh result.
  hold = true
  await refresh()
  const deadline = Date.now() + 8000
  while (!release && Date.now() < deadline) await new Promise(resolve => setTimeout(resolve, 20))
  assert.ok(release, 'Held request must arrive')
  await visibility(true)
  hold = false
  fail = true
  release()
  await page.waitForTimeout(100)
  fail = false
  await visibility(false)
  await waitState('running')
  assert.equal(await page.locator('.agent-activity-error').count(), 0)

  tasks = []
  await page.locator('.app-sider').getByText('任务', { exact: true }).click()
  await waitState('idle')
  tasks = [{ id: 1, status: 'executing', source_type: 'scheduled_task' }]
  await waitState('running')
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result: 'passed', activityCalls, checks: ['real App shell', 'polling across pages', 'manual and scheduled execution', 'total across pagination', 'non-executing statuses', 'animated orbit', 'stable layout', 'interactive button semantics', 'name editing', 'collapsed glow bounds', 'reduced motion', 'API failure and recovery', 'visibility pause/resume', 'aborted response ignored'] }))
} catch (error) {
  console.error(JSON.stringify({ errors, body: (await page.locator('body').innerText()).slice(0, 2200) }))
  throw error
} finally {
  await browser.close()
}
