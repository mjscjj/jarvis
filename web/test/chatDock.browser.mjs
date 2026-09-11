// Isolated browser regression: all API calls are fixtures, never real Agent turns.
// Start Vite, then run with PLAYWRIGHT_MODULE and CHROME_EXECUTABLE if not installed locally.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:18801'
const source = await (await fetch(`${base}/src/Chat.tsx`)).text()
const modulePath = (name) => {
  const result = source.match(new RegExp(`from ["']([^"']*${name}[^"']*)["']`))
  assert.ok(result, `Vite module import: ${name}`)
  return result[1]
}
const reactPath = modulePath('react\\.js')
const identityPath = modulePath('agentIdentity')
const contextPath = modulePath('pageContext')
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errors = []
const calls = []
page.on('pageerror', error => errors.push(error.message))
page.setDefaultTimeout(8000)
const makeSession = (id, title, extra = {}) => ({ id, title, agent: 'trae', model: 'test-model', reasoning_effort: 'medium', sources: [{ kind: 'workspace', label: '自动参考工作上下文' }], draft: {}, archived: false, created_at: new Date().toISOString(), updated_at: new Date().toISOString(), messages: [{ id: `m-${id}`, role: 'assistant', text: '先确认交互方案，再跟进接口联调。', created_at: new Date().toISOString() }], pending_attachments: [], ...extra })
const sessions = new Map([['s1', makeSession('s1', '今日工作安排')], ['s2', makeSession('s2', '项目进展')], ['s3', makeSession('s3', '归档的讨论', { archived: true })]])
const models = [{ id: 'test-model', name: 'Test Model', default: true, reasoning_efforts: ['medium', 'high'], default_reasoning_effort: 'medium', input_modalities: ['text', 'image'] }, { id: 'second-model', name: 'Second Model', default: false, reasoning_efforts: ['low', 'high'], default_reasoning_effort: 'high', input_modalities: ['text'] }]
let failModelPatch = false
try {
  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const body = request.headers()['content-type']?.includes('application/json') ? request.postDataJSON() : undefined
    calls.push({ path: url.pathname, method, body })
    let data
    if (url.pathname === '/api/agent-identity') data = { display_name: 'Jarvis' }
    else if (url.pathname === '/api/chat/agents') data = { items: [{ id: 'trae', name: 'TRAE', available: true, default: true }, { id: 'codex', name: 'Codex', available: true, default: false }, { id: 'cursor', name: 'Cursor', available: false, default: false }] }
    else if (url.pathname.endsWith('/models')) data = { items: models }
    else if (url.pathname === '/api/chat/sessions') {
      if (method === 'POST') { data = makeSession(`s${sessions.size + 1}`, body.title, { ...body, messages: [] }); sessions.set(data.id, data) }
      else data = { items: [...sessions.values()].filter(item => item.archived === (url.searchParams.get('archived') === 'true')) }
    } else {
      const match = url.pathname.match(/^\/api\/chat\/sessions\/([^/]+)(.*)$/)
      assert.ok(match, `Unexpected API: ${url.pathname}`)
      const session = sessions.get(match[1])
      assert.ok(session, `Missing fixture session: ${match[1]}`)
      const tail = match[2]
      if (!tail) {
        if (method === 'PATCH') {
          if (failModelPatch && body.model) { await route.fulfill({ status: 500, json: { code: 500, msg: '模型保存失败（测试）' } }); return }
          if (body.draft?.text === '慢保存的旧草稿') await new Promise(resolve => setTimeout(resolve, 650))
          Object.assign(session, body)
        }
        data = session
      } else if (tail === '/attachments') {
        data = { id: 'file1', name: 'note.txt', mime_type: 'text/plain', size_bytes: 5, created_at: new Date().toISOString() }
        session.pending_attachments.push(data)
      } else if (tail.startsWith('/attachments/')) { session.pending_attachments = []; data = { deleted: true } }
      else if (tail === '/cancel') { session.running = false; data = { canceled: true } }
      else assert.fail(`Unexpected API: ${url.pathname}`)
    }
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.addInitScript(() => {
    const originalFetch = window.fetch
    window.__chatSends = []
    window.fetch = (url, options) => {
      if (/\/api\/chat\/sessions\/[^/]+\/messages$/.test(String(url))) {
        window.__chatSends.push(JSON.parse(options.body))
        const encoder = new TextEncoder()
        return Promise.resolve(new Response(new ReadableStream({ start(controller) {
          window.__chatAccept = () => controller.enqueue(encoder.encode('event: accepted\ndata: {}\n\n'))
          window.__chatReject = () => { controller.enqueue(encoder.encode('event: error\ndata: {"message":"chat state conflict: this chat session is already generating a reply"}\n\n')); controller.close() }
          window.__chatDelta = text => controller.enqueue(encoder.encode(`event: delta\ndata: ${JSON.stringify({ text })}\n\n`))
          window.__chatFinish = () => { controller.enqueue(encoder.encode('event: done\ndata: {}\n\n')); controller.close() }
          if (!window.__deferAcceptance) { window.__chatAccept(); window.__chatDelta('正在检查当前进展。') }
        } }), { headers: { 'Content-Type': 'text/event-stream' } }))
      }
      return originalFetch(url, options)
    }
  })
  await page.route('**/__chat-test', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<!doctype html><meta charset="utf-8"><div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window); window.$RefreshReg$=()=>{}; window.$RefreshSig$=()=>t=>t; window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${reactPath}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {default:Chat}=await import('/src/Chat.tsx');
    const {AgentIdentityProvider}=await import('${identityPath}');
    const {PageContextProvider,usePageContext}=await import('${contextPath}');
    await import('/src/styles.css');
    function Harness(){const {context,navigate}=usePageContext();return React.createElement('div',{className:'app-shell',style:{'--sider-width':'184px'}},
      React.createElement('nav',{},React.createElement('button',{onClick:()=>navigate('chat')},'对话入口'),React.createElement('button',{onClick:()=>navigate('tasks',{mode:'delegated',page:'2',state:'open'})},'工作台入口')),
      React.createElement('div',{className:'app-content '+(context.active_key==='chat'?'is-chat-page':'')},React.createElement(Chat,{compact:context.active_key!=='chat'})));}
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(React.StrictMode,null,React.createElement(AgentIdentityProvider,null,React.createElement(PageContextProvider,{initialKey:'tasks'},React.createElement(Harness)))));
  </script>` }))
  await page.goto(`${base}/__chat-test#/work?mode=delegated&page=2&state=open`)
  const input = page.getByRole('textbox', { name: '底部对话输入' })
  await input.waitFor()
  await page.waitForFunction(() => !document.querySelector('[aria-label="底部对话输入"]').disabled)
  assert.equal(new URL(page.url()).hash, '#/work?mode=delegated&page=2&state=open')
  const initialHeight = await page.locator('.chat-dock-composer').evaluate(el => el.getBoundingClientRect().height)
  assert.ok(initialHeight <= 82, `Composer too tall: ${initialHeight}`)
  assert.equal(await page.locator('.chat-dock-composer').evaluate(el => getComputedStyle(el).backgroundColor), 'rgb(255, 255, 255)')
  await page.getByRole('button', { name: '切换 Agent：TRAE', exact: true }).click()
  assert.equal(await page.getByRole('button', { name: 'Cursor 不可用', exact: true }).isDisabled(), true)
  await page.getByRole('button', { name: 'Codex', exact: true }).click()
  await page.getByRole('button', { name: /^取\s*消$/ }).click()
  assert.equal(sessions.get('s1').agent, 'trae')
  assert.equal(await page.getByRole('button', { name: '切换 Agent：TRAE', exact: true }).isVisible(), true)
  await input.fill('跨页面保留的草稿')
  await page.getByRole('button', { name: '对话入口', exact: true }).click()
  await page.getByPlaceholder('问一个问题，或告诉我你想推进什么…').waitFor()
  assert.equal(await page.getByPlaceholder('问一个问题，或告诉我你想推进什么…').inputValue(), '跨页面保留的草稿')
  assert.equal(await page.locator('.chat-dock').count(), 0)
  assert.ok(page.url().endsWith('#/chat?session=s1'))
  await page.getByRole('button', { name: '工作台入口', exact: true }).click()
  assert.equal(await input.inputValue(), '跨页面保留的草稿')

  await input.fill('点击当前会话也保留')
  await page.getByRole('button', { name: '切换会话', exact: true }).click()
  assert.equal(await page.locator('.chat-dock-session[aria-current="true"]').count(), 1)
  assert.notEqual(await page.locator('.chat-dock-session[aria-current="true"]').evaluate(el => getComputedStyle(el).backgroundColor), await page.locator('.chat-dock-session:not([aria-current="true"])').first().evaluate(el => getComputedStyle(el).backgroundColor))
  await page.locator('.chat-dock-session').filter({ hasText: '今日工作安排' }).click()
  assert.equal(await input.inputValue(), '点击当前会话也保留')
  await input.fill('慢保存的旧草稿')
  await page.waitForTimeout(550) // Let the deliberately delayed autosave start.

  // Switch before the debounce expires: the previous session's draft must survive.
  await input.fill('马上切换也不丢失')
  await page.getByRole('button', { name: '切换会话', exact: true }).click()
  await page.locator('.chat-dock-session').filter({ hasText: '项目进展' }).click()
  await page.waitForFunction(() => document.querySelector('.chat-dock-session-trigger')?.textContent.includes('项目进展'))
  assert.equal(sessions.get('s1').draft.text, '马上切换也不丢失')
  assert.equal(await input.inputValue(), '')
  assert.ok(page.url().includes('mode=delegated'))

  await page.getByRole('button', { name: '切换模型：Test Model', exact: true }).click()
  await page.getByRole('combobox', { name: '底部对话模型' }).click()
  await page.locator('.ant-select-item-option').filter({ hasText: 'Second Model' }).click()
  await page.getByRole('button', { name: '切换模型：Second Model', exact: true }).waitFor()
  assert.equal(sessions.get('s2').model, 'second-model')
  assert.equal(sessions.get('s2').reasoning_effort, 'high')
  await page.getByRole('button', { name: '更多对话设置', exact: true }).click()
  await page.getByRole('combobox', { name: '底部对话推理强度' }).click()
  await page.locator('.ant-select-item-option').filter({ hasText: /^low$/ }).click()
  await page.waitForFunction(() => !document.querySelector('[aria-label="底部对话输入"]').disabled)
  assert.equal(sessions.get('s2').reasoning_effort, 'low')
  await page.getByRole('checkbox', { name: '世界模型', exact: true }).click()
  await page.waitForFunction(() => [...document.querySelectorAll('.chat-source-picker input')][2]?.checked)
  await page.waitForFunction(() => !document.querySelector('[aria-label="底部对话输入"]').disabled)
  assert.ok(sessions.get('s2').sources.some(item => item.kind === 'world'))
  await page.getByRole('button', { name: '更多对话设置', exact: true }).click()

  await page.locator('.chat-dock input[type=file]').setInputFiles({ name: 'note.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') })
  await page.getByRole('button', { name: '移除 note.txt', exact: true }).waitFor()
  await page.getByRole('button', { name: '移除 note.txt', exact: true }).click()
  await page.getByRole('button', { name: '移除 note.txt', exact: true }).waitFor({ state: 'detached' })

  await input.fill('检查工作台上下文')
  await input.press('Enter')
  await page.getByRole('button', { name: '停止回复', exact: true }).waitFor()
  assert.equal(await page.getByRole('button', { name: '切换 Agent：TRAE', exact: true }).isDisabled(), true)
  const firstSend = (await page.evaluate(() => window.__chatSends))[0]
  assert.equal(Object.hasOwn(firstSend, 'page_context'), false, 'ambient page context must not be sent')
  assert.ok(firstSend.sources.some(source => source.kind === 'world'), 'explicit sources must survive')
  assert.ok(firstSend.sources.every(source => !source.label.includes('自动参考')))
  await page.getByRole('button', { name: '对话入口', exact: true }).click()
  await page.getByText('正在检查当前进展。', { exact: true }).waitFor()
  await page.getByRole('button', { name: '工作台入口', exact: true }).click()
  await page.getByRole('button', { name: '停止回复', exact: true }).waitFor()
  await page.getByRole('button', { name: '停止回复', exact: true }).click()
  sessions.get('s2').messages.push({ id: 'reply-final', role: 'assistant', text: '检查完成，已保留当前上下文。', created_at: new Date().toISOString() })
  await page.evaluate(() => window.__chatFinish())
  await page.getByRole('button', { name: '发送消息', exact: true }).waitFor()
  assert.ok(calls.some(call => call.path === '/api/chat/sessions/s2/cancel'))
  await page.getByRole('button', { name: '查看最新回复全文', exact: true }).click()
  await page.locator('.chat-dock-reply-body').getByText('检查完成，已保留当前上下文。').waitFor()
  await page.getByRole('button', { name: '关闭回复全文', exact: true }).click()

  failModelPatch = true
  await page.getByRole('button', { name: '切换模型：Second Model', exact: true }).click()
  await page.getByRole('combobox', { name: '底部对话模型' }).click()
  await page.locator('.ant-select-item-option').filter({ hasText: 'Test Model' }).click()
  await page.getByText('模型保存失败（测试）', { exact: true }).waitFor()
  failModelPatch = false
  await page.locator('.chat-dock-error .ant-alert-close-icon').click()

  for (const width of [1280, 768, 767, 390, 320]) {
    await page.setViewportSize({ width, height: 800 })
    const geometry = await page.locator('.chat-dock-composer').evaluate(el => ({ left: el.getBoundingClientRect().left, right: el.getBoundingClientRect().right, scroll: el.scrollWidth, width: el.clientWidth }))
    assert.ok(geometry.left >= 0 && geometry.right <= width, JSON.stringify({ width, geometry }))
    assert.ok(geometry.scroll <= geometry.width + 1, JSON.stringify({ width, geometry }))
    assert.equal(await page.getByRole('button', { name: '切换 Agent：TRAE', exact: true }).isVisible(), true)
  }
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.getByRole('button', { name: '收起底部对话输入', exact: true }).click()
  await page.getByRole('button', { name: '展开底部对话输入', exact: true }).click()
  await input.waitFor()
  await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === '底部对话输入')
  await input.fill('第一行')
  await input.press('Shift+Enter')
  assert.equal(await input.inputValue(), '第一行\n')
  // An archived deep link isn't in the recent list but must still load directly.
  await page.evaluate(() => { window.location.hash = '/chat?session=s3' })
  await page.locator('.chat-session-heading strong').filter({ hasText: '归档的讨论' }).waitFor()
  await page.getByRole('button', { name: '工作台入口', exact: true }).click()
  await page.getByRole('button', { name: '切换会话', exact: true }).click()
  await page.locator('.chat-dock-sessions .ant-btn').filter({ hasText: '新对话' }).click()
  await page.waitForFunction(() => document.querySelector('.chat-dock-session-trigger')?.textContent.includes('新对话'))
  assert.deepEqual(calls.filter(call => call.path === '/api/chat/sessions' && call.method === 'POST').at(-1).body.sources, [], 'new sessions must not select background sources automatically')
  await page.getByRole('button', { name: '切换 Agent：TRAE', exact: true }).click()
  await page.getByRole('button', { name: 'Codex', exact: true }).click()
  await page.waitForFunction(() => !document.querySelector('[aria-label="底部对话输入"]').disabled)
  assert.equal(sessions.get('s4').agent, 'codex')
  await page.getByRole('button', { name: '切换 Agent：Codex', exact: true }).waitFor()
  await page.evaluate(() => { window.__deferAcceptance = true })
  await input.fill('冲突时保留输入')
  await page.locator('.chat-dock input[type=file]').setInputFiles({ name: 'note.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') })
  await page.getByRole('button', { name: '移除 note.txt', exact: true }).waitFor()
  await input.press('Enter')
  await page.waitForFunction(() => document.querySelector('[aria-label="底部对话输入"]').disabled)
  assert.equal(await input.inputValue(), '冲突时保留输入')
  assert.equal(sessions.get('s4').draft.text, '冲突时保留输入')
  await page.waitForFunction(() => window.__chatSends.at(-1)?.message === '冲突时保留输入')
  sessions.get('s4').running = true
  await page.evaluate(() => window.__chatReject())
  await page.getByText('chat state conflict: this chat session is already generating a reply', { exact: true }).waitFor()
  await page.waitForFunction(() => !document.querySelector('[aria-label="底部对话输入"]').disabled)
  assert.equal(await input.inputValue(), '冲突时保留输入')
  assert.equal(await page.getByRole('button', { name: '移除 note.txt', exact: true }).count(), 1)
  assert.equal(sessions.get('s4').messages.length, 0)
  await page.getByRole('button', { name: '停止回复', exact: true }).waitFor()
  await page.getByRole('button', { name: '停止回复', exact: true }).click()
  await page.getByRole('button', { name: '发送消息', exact: true }).waitFor()
  assert.equal(await input.inputValue(), '冲突时保留输入')

  // A reloaded full chat must recover the backend state and poll completion.
  sessions.get('s4').running = true
  await page.goto(`${base}/__chat-test#/chat?session=s4`)
  await page.reload()
  await page.getByText('上一轮仍在回复，完成后自动更新…', { exact: true }).waitFor()
  assert.equal(await page.getByPlaceholder('问一个问题，或告诉我你想推进什么…').inputValue(), '冲突时保留输入')
  sessions.get('s4').messages.push({ id: 'recovered', role: 'assistant', text: '后台回复完成', created_at: new Date().toISOString() })
  sessions.get('s4').running = false
  await page.getByText('后台回复完成', { exact: true }).waitFor()
  await page.getByRole('button', { name: '工作台入口', exact: true }).click()
  await input.waitFor()
  await input.fill('接收后清空')
  await input.press('Enter')
  await page.waitForFunction(() => document.querySelector('[aria-label="底部对话输入"]').value === '')
  assert.equal(await page.getByRole('button', { name: '移除 note.txt', exact: true }).count(), 0)
  await page.evaluate(() => window.__chatFinish())
  await page.getByRole('button', { name: '发送消息', exact: true }).waitFor()
  if (process.env.CHAT_TEST_SCREENSHOT) await page.screenshot({ path: process.env.CHAT_TEST_SCREENSHOT })
  sessions.get('s4').messages.push({ id: 'failed-in-background', role: 'assistant', text: '', status: 'interrupted', error: '模型授权已过期（测试）', created_at: new Date().toISOString() })
  await page.goto(`${base}/__chat-test#/chat?session=s4`)
  await page.reload()
  await page.getByText('模型授权已过期（测试）', { exact: true }).waitFor()
  await page.getByText('回复已中断，以上为已保存内容。', { exact: true }).waitFor()
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({ result: 'passed', composerHeight: initialHeight, checks: ['draft across pages', 'draft on fast session switch', 'serialized autosaves', 'same-session draft', 'model and effort', 'sources', 'file upload/remove', 'stream across pages', 'stop', 'reply detail', 'visible API errors', '320–1280px layout', 'collapse/expand focus', 'archived deep link', 'new session', 'visible Agent selection', 'unavailable Agent disabled', 'Agent switch cancellation', 'light theme', 'selected session styling', 'Shift+Enter newline', 'rejected input and attachments retained', 'remote reply stop', 'refresh and completion polling', 'clear only after acceptance'], apiCalls: calls.length }))
} catch (error) {
  console.error(JSON.stringify({ errors, body: (await page.locator('body').innerText()).slice(0, 2000) }))
  if (process.env.CHAT_TEST_SCREENSHOT) await page.screenshot({ path: process.env.CHAT_TEST_SCREENSHOT })
  throw error
} finally { await browser.close() }
