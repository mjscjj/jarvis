// Run only against an isolated instance: this test creates two local comments.
import assert from 'node:assert/strict'
const {chromium}=await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base=process.env.OKR_BROWSER_URL
assert(base,'OKR_BROWSER_URL must name an isolated test instance')
const browser=await chromium.launch({headless:true,args:['--no-sandbox']})
try {
 const page=await browser.newPage({viewport:{width:1400,height:900}})
 const errors=[]
 page.on('pageerror',e=>errors.push(e.message))
 await page.route('**/api/setup/bootstrap',r=>r.fulfill({json:{code:0,data:{machine_configuration_ready:true}}}))
 await page.route('**/api/setup/status',r=>r.fulfill({json:{code:0,data:{onboarding_required:false,runtime_id:'test',app_ready:true,world_model_ready:true,configuration:{machine_configuration_ready:true,agent_name_configured:true},lark:{available:true,credential_available:true,bot:{status:'ready',verified:true},user:{status:'ready',verified:true}},agent:{available:true,authenticated:true}}}}))
 const url=base+'/#/biz-okr?tab=review-fill&quarter=2026-Q3&week=2026-W36'
 page.setDefaultTimeout(10000)
 await page.goto(url)
 console.log('Loaded')
 await page.locator('button').filter({hasText:/^评论\s*\d*$/}).waitFor()
 await page.locator('button').filter({hasText:/^评论\s*\d*$/}).click()
 console.log('Opened drawer')
 const drawer=page.locator('aside[aria-hidden="false"]')
 await drawer.getByText('正在读取评论…',{exact:true}).waitFor({state:'hidden'})
 async function submit(content){
  await drawer.getByPlaceholder('对本周页面发表评论，输入 @ 选择提醒人…').fill(content)
  const response=page.waitForResponse(r=>r.request().method()==='POST'&&new URL(r.url()).pathname==='/api/biz-okr/comments')
  const start=Date.now()
  await drawer.getByRole('button',{name:'发布评论',exact:true}).click()
  console.log('Clicked submit')
  const result=await response
  assert.equal(result.status(),200,await result.text())
  const body=await result.json()
  const node=page.locator('#comment-'+body.data.id)
  await node.waitFor()
  await drawer.getByText('评论已发布',{exact:true}).waitFor()
  await page.waitForTimeout(350)
  const box=await node.boundingBox()
  assert(box && box.y>=0 && box.y<900,JSON.stringify(box))
  assert((await node.innerText()).includes(content))
  console.log('Saved and visible',Date.now()-start,'ms')
  return node
 }
 await submit('隔离验证：提交后立即可见 '+Date.now())
 // Hold every list response while publishing. The drawer must recover the
 // existing threads as well as keep the new one when those responses arrive.
 let release
 const gate=new Promise(resolve=>{release=resolve})
 let intercepted
 const interceptedPromise=new Promise(resolve=>{intercepted=resolve})
 let oldCount=0
 await page.route('**/api/biz-okr/comments?**',async route=>{
  if(route.request().method()!=='GET')return route.continue()
  const old=await route.fetch()
  oldCount=(await old.json()).data.comments.length
  intercepted()
  await gate
  await route.fulfill({response:old})
 })
 await page.reload()
 await page.locator('button').filter({hasText:/^评论\s*\d*$/}).click()
 await interceptedPromise
 await drawer.getByText('正在读取评论…',{exact:true}).waitFor()
 assert(oldCount>0,'fixture needs existing comments')
 const node=await submit('隔离验证：旧列表不可覆盖新评论 '+Date.now())
 assert.equal(await drawer.locator('article[id^="comment-"]').count(),1)
 release()
 await page.waitForFunction(count=>document.querySelectorAll('aside[aria-hidden="false"] article[id^="comment-"]').length>=count,oldCount+1)
 await node.waitFor()
 assert.equal(errors.length,0,errors.join('\n'))
 await page.screenshot({path:process.env.OKR_BROWSER_SCREENSHOT || '/tmp/okr-comment-feedback.png'})
 console.log('PASS: new comment and',oldCount,'older threads remain visible after delayed GET; no browser errors')
} finally {await browser.close()}
