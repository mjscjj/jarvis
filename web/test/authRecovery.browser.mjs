// Isolated session-recovery regression; never contacts a real identity service.
import assert from 'node:assert/strict'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:18801'
const source = await (await fetch(`${base}/src/auth.tsx`)).text()
const reactPath = source.match(/from ["']([^"']*react\.js[^"']*)["']/)[1]
const browser = await chromium.launch({ executablePath: process.env.CHROME_EXECUTABLE, headless: true, args: ['--no-sandbox'] })
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
      if(path==='/api/auth/status') return response(++checks===1?{enabled:true,status:'authenticated',user}:{enabled:true,status:'anonymous'});
      if(path==='/api/auth/login') return new Promise(resolve=>{window.finishRecovery=()=>resolve(response({enabled:true,status:'authenticated',user}));});
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

  await page.route('**/__auth-automatic', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window); window.$RefreshReg$=()=>{}; window.$RefreshSig$=()=>t=>t; window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${reactPath}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {AuthProvider,AuthGate}=await import('/src/auth.tsx');
    const user={name:'Test',email:'test@example.test'};
    const response=data=>new Response(JSON.stringify({code:0,data:{enabled:true,...data}}),{headers:{'Content-Type':'application/json'}});
    window.loginCalls=0; window.authPolls=[]; let checks=0;
    window.fetch=async (path,options)=>{
      if(path==='/api/auth/status') {
        if (++checks===1) return new Response('',{status:503});
        return response({status:'unauthenticated'});
      }
      if(path==='/api/auth/login') {
        window.loginCalls++;
        return response({status:'pending',flow_id:'flow-1',verification_url:'https://example.test/flow-1'});
      }
      if(path==='/api/auth/login/complete') {
        const id=JSON.parse(options.body).flow_id;
        window.authPolls.push(id);
        if(window.authPolls.length===1) return new Response('',{status:502});
        if(id==='flow-1') return response({status:'pending',flow_id:'flow-2',verification_url:'https://example.test/flow-2'});
        if(window.deny) return new Response(JSON.stringify({code:403,msg:'SSO 授权已取消'}),{status:403,headers:{'Content-Type':'application/json'}});
        return response(window.newAuthorized?{status:'authenticated',user}:{status:'pending'});
      }
      throw new Error('Unexpected request: '+path);
    };
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(AuthProvider,null,React.createElement(AuthGate,{agentName:'Test'},React.createElement('div',null,'已进入工作区'))));
  </script>` }))
  await page.goto(`${base}/__auth-automatic`)
  await page.locator('a[href="https://example.test/flow-2"]').waitFor({ timeout: 15000 })
  assert.equal(await page.evaluate(() => window.loginCalls), 1)
  assert.deepEqual(await page.evaluate(() => window.authPolls.slice(0, 2)), ['flow-1', 'flow-1'])
  assert.equal(await page.getByRole('button', { name: '重新生成授权链接' }).count(), 0)
  await page.evaluate(() => { window.newAuthorized = true })
  await page.getByText('已进入工作区', { exact: true }).waitFor()
  assert.deepEqual(errors, [])
  console.log('PASS: initial outage, polling outage and expired flow recover automatically without clicks')

  await page.goto(`${base}/__auth-automatic`)
  await page.locator('a[href="https://example.test/flow-2"]').waitFor({ timeout: 15000 })
  await page.evaluate(() => { window.deny = true })
  await page.getByText('SSO 授权已取消', { exact: true }).waitFor()
  const deniedPolls = await page.evaluate(() => window.authPolls.length)
  await page.waitForTimeout(2500)
  assert.equal(await page.evaluate(() => window.authPolls.length), deniedPolls)
  assert.equal(await page.evaluate(() => window.loginCalls), 1)
  console.log('PASS: explicit authorization denial stops automatic polling')

  await page.route('**/__okr-guest-recovery', route => route.fulfill({ contentType: 'text/html; charset=utf-8', body: `<div id="root"></div><script type="module">
    import RefreshRuntime from '/@react-refresh';
    RefreshRuntime.injectIntoGlobalHook(window); window.$RefreshReg$=()=>{}; window.$RefreshSig$=()=>t=>t; window.__vite_plugin_react_preamble_installed__=true;
    const {default:React}=await import('${reactPath}');
    const {default:ReactDOM}=await import('/node_modules/.vite/deps/react-dom_client.js');
    const {AuthProvider,AuthGate,useAuth}=await import('/src/auth.tsx');
    const {default:IdentityBoundary}=await import('/src/okr/IdentityBoundary.tsx');
    const {apiFetch}=await import('/src/api.ts');
    const response=data=>new Response(JSON.stringify({code:0,data}),{headers:{'Content-Type':'application/json'}});
    window.mounts=0; window.loginCalls=0; window.recovered=false; window.finishStatus=null;
    window.fetch=async path=>{
      if(path==='/api/auth/status') return new Promise((resolve,reject)=>{window.finishStatus=fail=>{window.finishStatus=null;fail?reject(new Error('identity service unavailable')):resolve(response({enabled:true,status:'unauthenticated'}));};});
      if(path==='/api/biz-okr/me') return response({configured:true,authenticated:true,management_access:false,user:{open_id:'okr-visitor',name:'OKR visitor'}});
      if(path==='/api/expired') return new Response(JSON.stringify({code:401,msg:'session expired'}),{status:401});
      if(path==='/api/auth/login') {window.loginCalls++;return response({enabled:true,status:'pending',flow_id:'principal-flow',verification_url:'https://example.test/principal'});}
      throw new Error('Unexpected request: '+path);
    };
    window.expireSession=()=>{window.recovered=false;void apiFetch('/api/expired').then(()=>{window.recovered=true;});};
    function Draft(){const [text,setText]=React.useState('');React.useEffect(()=>{window.mounts++;},[]);return React.createElement('input',{placeholder:'OKR draft',value:text,onChange:event=>setText(event.target.value)});}
    function Surface(){const auth=useAuth();window.authLoading=auth.loading;return React.createElement(AuthGate,{agentName:'Test'},React.createElement(IdentityBoundary,null,React.createElement(Draft)));}
    ReactDOM.createRoot(document.getElementById('root')).render(React.createElement(AuthProvider,null,React.createElement(Surface)));
  </script>` }))
  for (const hash of ['/biz-okr?tab=okr-plan', '/weekly-report?tab=weekly-fill']) {
    await page.goto('about:blank')
    await page.goto(`${base}/__okr-guest-recovery#${hash}`)
    const draft = page.getByPlaceholder('OKR draft')
    await draft.fill('keep the OKR draft')
    await page.waitForFunction(() => typeof window.finishStatus === 'function')
    assert.equal(await page.evaluate(() => window.authLoading), true, 'Module renders while principal identity is unresolved')
    await page.evaluate(() => window.finishStatus(false))
    await page.waitForFunction(() => window.authLoading === false)
    for (const fail of [false, true]) {
      await page.evaluate(() => window.expireSession())
      await page.waitForFunction(() => typeof window.finishStatus === 'function')
      assert.equal(await draft.inputValue(), 'keep the OKR draft')
      assert.equal(await page.locator('.auth-loading').count(), 0)
      await page.evaluate(fail => window.finishStatus(fail), fail)
      await page.waitForFunction(() => window.recovered)
      assert.equal(await draft.inputValue(), 'keep the OKR draft')
      assert.equal(await page.evaluate(() => window.mounts), 1)
      assert.equal(await page.evaluate(() => window.loginCalls), 0)
    }
    await page.evaluate(() => { location.hash = '/chat' })
    await page.waitForFunction(() => typeof window.finishStatus === 'function')
    assert.equal(await draft.count(), 0, 'Protected pages still require principal login')
    await page.evaluate(() => window.finishStatus(false))
    await page.locator('a[href="https://example.test/principal"]').waitFor()
    assert.equal(await page.evaluate(() => window.loginCalls), 1)
  }
  assert.deepEqual(errors, [])
  console.log('PASS: OKR and shared reports retain drafts through principal 401 recovery and failure without bypassing protected-page login')
} catch (error) {
  for (const page of browser.contexts().flatMap(context => context.pages())) console.error(JSON.stringify({ body: await page.locator('body').innerText(), state: await page.evaluate(() => ({ polls: window.authPolls, loginCalls: window.loginCalls, authorized: window.newAuthorized })) }))
  throw error
} finally {
  await browser.close()
}
