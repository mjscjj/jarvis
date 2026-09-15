// Uses fixtures only: verifies that the OKR UI never requests ordinary history.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:18801'
const source = await (await fetch(`${base}/src/Chat.tsx`)).text()
const modulePath = name => source.match(new RegExp(`from ["']([^"']*${name}[^"']*)["']`))[1]
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
const page = await browser.newPage()
await page.setViewportSize({width: 1440, height: 900})
const calls = [], errors = []
page.on('pageerror', e => errors.push(e.message))
const session = {id:'cs_okr', title:'OKR 专用记录', agent:'codex', model:'test', reasoning_effort:'medium', sources:[], draft:{}, archived:false, created_at:new Date().toISOString(), updated_at:new Date().toISOString(), messages:[{id:'cm_a',role:'assistant',text:'这是 OKR 结果',created_at:new Date().toISOString(),attachments:[{id:'ca_okr',name:'okr.txt',mime_type:'text/plain',size_bytes:1}]}], pending_attachments:[]}
try {
  await page.route('**/api/**', async route => {
    const r = route.request(), path = new URL(r.url()).pathname
    calls.push(path)
    let data
    if (path === '/api/agent-identity') data = {display_name:'Jarvis'}
    else if (path === '/api/okr-chat/agents') data = {items:[{id:'codex',name:'OKR Docker',available:true,default:true}]}
    else if (path === '/api/okr-chat/agents/codex/models') data = {items:[{id:'test',name:'Test',default:true,reasoning_efforts:['medium'],default_reasoning_effort:'medium'}]}
    else if (path === '/api/okr-chat/sessions') data = {items:[session]}
    else if (path === '/api/okr-chat/sessions/cs_okr') { if(r.method()==='PATCH') Object.assign(session,r.postDataJSON()); data=session }
    else { errors.push(`unexpected API ${path}`); await route.fulfill({status:403,json:{code:403,msg:'forbidden'}}); return }
    await route.fulfill({json:{code:0,data}})
  })
  await page.route('**/__okr-chat-test', route => route.fulfill({contentType:'text/html',body:`<div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh'; RefreshRuntime.injectIntoGlobalHook(window);window.$RefreshReg$=()=>{};window.$RefreshSig$=()=>t=>t;window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${modulePath('react\\.js')}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {default:Chat}=await import('/src/Chat.tsx');
    const {AgentIdentityProvider}=await import('${modulePath('agentIdentity')}');
    const {PageContextProvider}=await import('${modulePath('pageContext')}');
    await import('/src/styles.css');
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(AgentIdentityProvider,null,React.createElement(PageContextProvider,{initialKey:'biz-okr'},React.createElement(Chat,{compact:true,isolated:true}))));
  </script>`}))
  await page.goto(`${base}/__okr-chat-test#/biz-okr?session=ordinary-id`)
  const input = page.getByRole('textbox',{name:'底部对话输入'})
  await input.waitFor()
  await page.waitForFunction(()=>!document.querySelector('[aria-label="底部对话输入"]').disabled)
  await input.fill('OKR 草稿不会跳到普通会话')
  await page.getByRole('button',{name:'查看最新回复全文'}).click()
  await page.getByRole('button',{name:'在对话页继续'}).click()
  await page.getByRole('dialog').waitFor()
  const composer = await page.locator('.ant-modal .chat-composer-area').boundingBox()
  assert.ok(composer && composer.y + composer.height <= 900, `modal composer is outside viewport: ${JSON.stringify(composer)}`)
  await page.setViewportSize({width: 390, height: 844})
  const mobileComposer = await page.locator('.ant-modal .chat-composer-area').boundingBox()
  assert.ok(mobileComposer && mobileComposer.y + mobileComposer.height <= 844, `mobile modal composer is outside viewport: ${JSON.stringify(mobileComposer)}`)
  assert.equal(await page.getByPlaceholder('问一个问题，或告诉我你想推进什么…').inputValue(),'OKR 草稿不会跳到普通会话')
  assert.ok(new URL(page.url()).hash.includes('biz-okr'))
  assert.equal(await page.locator('a[href="/api/okr-chat/attachments/ca_okr/content"]').count(),1)
  session.messages[0].text = '长回复滚动测试。\n\n'.repeat(300)
  await page.reload()
  await page.getByRole('button',{name:'查看最新回复全文'}).click()
  await page.getByRole('button',{name:'在对话页继续'}).click()
  await page.locator('.ant-modal-container').waitFor()
  await page.waitForFunction(() => !document.querySelector('.ant-modal')?.className.includes('ant-zoom'))
  for (const viewport of [{width:1440,height:900},{width:1280,height:500},{width:390,height:844},{width:390,height:500}]) {
    await page.setViewportSize(viewport)
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    const geometry = await page.locator('.ant-modal-container').evaluate(el => {
      const modal = el.getBoundingClientRect()
      const composer = el.querySelector('.chat-composer-area').getBoundingClientRect()
      const list = el.querySelector('.chat-message-list')
      return {modal:modal.toJSON(),composer:composer.toJSON(),listHeight:list.clientHeight,listScrollHeight:list.scrollHeight}
    })
    assert.ok(geometry.composer.bottom <= geometry.modal.bottom && geometry.composer.bottom <= viewport.height, JSON.stringify({viewport,geometry}))
    assert.ok(geometry.composer.top >= geometry.modal.top && geometry.listHeight > 0 && geometry.listScrollHeight > geometry.listHeight, JSON.stringify({viewport,geometry}))
  }
  assert.equal(calls.some(p=>p.startsWith('/api/chat/')),false)
  assert.equal(calls.some(p=>p.includes('ordinary-id')),false)
  assert.deepEqual(errors,[])
  console.log(JSON.stringify({result:'passed',checks:['isolated API','ordinary deep-link ignored','history stays in OKR','draft retained on expansion','isolated attachment URL'],calls:calls.length}))
} finally { await browser.close() }
