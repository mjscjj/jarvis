// Isolated session-recovery regression; never contacts a real identity service.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:5189'
const source = await (await fetch(`${base}/src/auth.tsx`)).text()
const reactPath = source.match(/from ["']([^"']*react\.js[^"']*)["']/)[1]
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true })
try {
  const page = await browser.newPage()
  page.setDefaultTimeout(8000)
  const errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.route('**/__auth-recovery', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window); window.$RefreshReg$=()=>{}; window.$RefreshSig$=()=>t=>t; window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${reactPath}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {AuthProvider,AuthGate}=await import('/src/auth.tsx');
    const {apiFetch}=await import('/src/api.ts');
    let checks=0, expired=false;
    const user={name:'Test',email:'test@example.test'};
    const response=data=>new Response(JSON.stringify({code:0,data}),{headers:{'Content-Type':'application/json'}});
    window.fetch=async path=>{
      if(path==='/api/auth/status') return response(++checks===1?{status:'authenticated',user}:{status:'anonymous'});
      if(path==='/api/auth/login') return new Promise(resolve=>{window.finishRecovery=()=>resolve(response({status:'authenticated',user}));});
      if(path==='/api/expired') {if(!expired){expired=true;return new Response('',{status:401});}return response({ok:true});}
      throw new Error('Unexpected request: '+path);
    };
    window.expireSession=()=>{void apiFetch('/api/expired').then(()=>{window.recovered=true;});};
    window.mounts=0;
    function Draft(){const [text,setText]=React.useState('');React.useEffect(()=>{window.mounts++;},[]);return React.createElement('input',{placeholder:'draft',value:text,onChange:event=>setText(event.target.value)});}
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(AuthProvider,null,React.createElement(AuthGate,{agentName:'Test'},React.createElement(Draft))));
  </script>` }))
  await page.goto(`${base}/__auth-recovery`)
  const input = page.getByPlaceholder('draft')
  await input.fill('keep this draft').catch(async error => {
    throw new Error(`${error.message}\n${errors.join('\n')}\n${await page.locator('body').innerText()}`)
  })
  await page.evaluate(() => window.expireSession())
  await page.waitForFunction(() => typeof window.finishRecovery === 'function')
  assert.equal(await input.inputValue(), 'keep this draft')
  await input.fill('still editable during recovery')
  await page.evaluate(() => window.finishRecovery())
  await page.waitForFunction(() => window.recovered)
  assert.equal(await input.inputValue(), 'still editable during recovery')
  assert.equal(await page.evaluate(() => window.mounts), 1)
  assert.deepEqual(errors, [])
  console.log('PASS: 401 recovery preserves the mounted editor and its draft')

  await page.route('**/__auth-regenerate', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window); window.$RefreshReg$=()=>{}; window.$RefreshSig$=()=>t=>t; window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${reactPath}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {AuthProvider,AuthGate}=await import('/src/auth.tsx');
    const user={name:'Test',email:'test@example.test'};
    const response=data=>new Response(JSON.stringify({code:0,data}),{headers:{'Content-Type':'application/json'}});
    window.loginCalls=0; window.authPolls=[];
    window.fetch=async (path,options)=>{
      if(path==='/api/auth/status') return response({status:'unauthenticated'});
      if(path==='/api/auth/login') {
        const id='flow-'+(++window.loginCalls);
        const result=()=>response({status:'pending',flow_id:id,verification_url:'https://example.test/'+id});
        return window.loginCalls===1?result():new Promise(resolve=>{window.finishLogin=()=>resolve(result());});
      }
      if(path==='/api/auth/login/complete') {
        window.authPolls.push({body:options.body,authorized:window.newAuthorized});
        if(JSON.parse(options.body).flow_id==='flow-1') return new Promise(resolve=>{window.finishOldPoll=()=>resolve(response({status:'authenticated',user}));});
        return response(window.newAuthorized?{status:'authenticated',user}:{status:'pending'});
      }
      throw new Error('Unexpected request: '+path);
    };
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(AuthProvider,null,React.createElement(AuthGate,{agentName:'Test'},React.createElement('div',null,'已进入工作区'))));
  </script>` }))
  await page.goto(`${base}/__auth-regenerate`)
  await page.waitForFunction(() => typeof window.finishOldPoll === 'function')
  await page.getByRole('button', { name: '重新生成授权链接', exact: true }).click()
  await page.waitForFunction(() => typeof window.finishLogin === 'function')
  await page.evaluate(() => { window.finishOldPoll(); window.finishLogin() })
  await page.locator('a[href="https://example.test/flow-2"]').waitFor()
  assert.equal(await page.getByText('已进入工作区', { exact: true }).count(), 0)
  assert.equal(await page.evaluate(() => window.loginCalls), 2)
  await page.evaluate(() => { window.newAuthorized = true })
  await page.getByText('已进入工作区', { exact: true }).waitFor()
  assert.deepEqual(errors, [])
  console.log('PASS: regeneration ignores stale SSO completion and completes the new flow')
} catch (error) {
  for (const page of browser.contexts().flatMap(context => context.pages())) console.error(JSON.stringify({ body: await page.locator('body').innerText(), state: await page.evaluate(() => ({ polls: window.authPolls, loginCalls: window.loginCalls, authorized: window.newAuthorized })) }))
  throw error
} finally {
  await browser.close()
}
