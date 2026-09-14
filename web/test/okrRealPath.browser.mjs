// Live OKR business-path acceptance test. Uses either an existing browser session
// or a short test session issued from a live-verified Feishu grant. The latter
// does not test device login. No API routing or fixture identities are used.
import assert from 'node:assert/strict'
import { readFileSync, unlinkSync } from 'node:fs'
const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')

const base = process.env.OKR_REAL_BASE_URL?.replace(/\/$/, '')
const profile = process.env.OKR_REAL_PROFILE_DIR
const sessionFile = process.env.OKR_REAL_SESSION_FILE
const expectedOpenID = process.env.OKR_REAL_EXPECTED_OPEN_ID
const expectedName = process.env.OKR_REAL_EXPECTED_NAME || '储节节'
const quarter = process.env.OKR_REAL_QUARTER || '2026-Q3'
const week = process.env.OKR_REAL_WEEK || '2026-W36'
const weeklyWeek = process.env.OKR_REAL_WEEKLY_WEEK || '2026-W35'
const planQuarter = process.env.OKR_REAL_PLAN_QUARTER || '2026-Q4'
const cases = (process.env.OKR_REAL_CASES || 'navigation,people,review-comment,weekly-comment,plan-comment,review-export,weekly-export,self-mention').split(',').map(value => value.trim())
const runID = `okr-real-${new Date().toISOString().replace(/[:.]/g, '-')}`
assert(base && /^https?:\/\//.test(base), 'Set OKR_REAL_BASE_URL to the actual deployed OKR origin')
assert(profile, 'Set OKR_REAL_PROFILE_DIR to a dedicated browser profile')
assert(expectedOpenID, 'Set OKR_REAL_EXPECTED_OPEN_ID from a verified OKR identity; display name alone is insufficient')
const validCases = ['navigation', 'people', 'review-comment', 'weekly-comment', 'plan-comment', 'review-export', 'weekly-export', 'self-mention']
assert(cases.length > 0 && cases.every(value => validCases.includes(value)), `OKR_REAL_CASES must use: ${validCases.join(', ')}`)

const browser = await chromium.launchPersistentContext(profile, {
  executablePath: process.env.CHROME_EXECUTABLE || undefined,
  headless: process.env.OKR_REAL_HEADLESS !== '0',
  viewport: { width: 1450, height: 1000 },
  args: ['--no-sandbox'],
})
let sessionToken = ''
if (sessionFile) {
  const session = JSON.parse(readFileSync(sessionFile, 'utf8'))
  assert.equal(session.open_id, expectedOpenID, 'Test session belongs to a different Feishu account')
  sessionToken = session.cookie
  assert(sessionToken, 'Test session file has no cookie')
  await browser.addCookies([{
    name: 'jarvis_okr_session', value: sessionToken,
    url: base, httpOnly: true, sameSite: 'Lax',
  }])
}
const page = browser.pages()[0] || await browser.newPage()
page.setDefaultTimeout(15000)
const evidence = { run_id: runID, origin: base, session_source: sessionFile ? 'live_verified_grant_test_session' : 'existing_browser_login', login_flow_tested: !sessionFile, quarter, review_week: week, weekly_week: weeklyWeek, plan_quarter: planQuarter, cases, identity: null, results: [], artifacts: [] }
const createdComments = []
const browserErrors = []
page.on('pageerror', error => browserErrors.push(error.message))
page.on('response', response => {
  if (process.env.OKR_REAL_TRACE_PLAN === '1' && new URL(response.url()).pathname.startsWith('/api/biz-okr/plans')) {
    console.error('Plan HTTP:', response.request().method(), new URL(response.url()).pathname, response.status())
  }
})
let createdPlanID = ''

async function api(path, options) {
  const result = await page.evaluate(async ({ path, options }) => {
    const response = await fetch(path, { credentials: 'same-origin', ...options })
    return { status: response.status, body: await response.json() }
  }, { path, options })
  return result
}

async function uiWrite(path, method, action, expectedStatus = 200) {
  const response = page.waitForResponse(value => value.request().method() === method && new URL(value.url()).pathname === path)
  await action()
  const saved = await response
  const body = await saved.json()
  assert.equal(saved.status(), expectedStatus, JSON.stringify(body))
  return body.data
}

async function verifyIdentity() {
  await page.goto(`${base}/#/biz-okr?tab=review-fill&quarter=${quarter}&week=${week}`)
  const me = await api('/api/biz-okr/me')
  assert.equal(me.status, 200, `Cannot read current OKR identity: ${JSON.stringify(me.body)}`)
  assert.equal(me.body.data?.configured, true, 'OKR login is disabled; a synthetic Jarvis identity cannot pass this test')
  assert.equal(me.body.data?.authenticated, true, 'This browser profile is not logged into OKR; complete product login in this profile first')
  assert.equal(me.body.data?.user?.open_id, expectedOpenID, 'Logged-in user does not match the expected Feishu open_id')
  assert.equal(me.body.data?.user?.name, expectedName, 'Logged-in user name differs from the verified tester')
  evidence.identity = { name: me.body.data.user.name, open_id: me.body.data.user.open_id }
  await page.getByRole('heading', { name: '登录 OKR' }).waitFor({ state: 'hidden' })
}

async function cleanupComment() {
  for (const created of createdComments) {
    const list = await api(created.listPath)
    assert.equal(list.status, 200, `Could not read this run's test comment: ${JSON.stringify(list.body)}`)
    const item = list.body.data?.comments?.find(comment => comment.id === created.id)
    if (!item) continue
    const deletion = await api(`/api/biz-okr/comments/${encodeURIComponent(item.id)}`, {
      method: 'DELETE',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ expected_version: item.version, delete_token: item.delete_token }),
    })
    assert.equal(deletion.status, 200, `Could not delete this run's test comment: ${JSON.stringify(deletion.body)}`)
    const after = await api(created.listPath)
    assert.equal(after.status, 200)
    assert(!after.body.data?.comments?.some(comment => comment.id === item.id), 'Deleted test comment remains in the list')
    evidence.artifacts.push({ kind: 'comment', id: item.id, cleaned_up: true })
  }
  createdComments.length = 0
}

async function openCommentDrawer(plan = false) {
  const button = plan
    ? page.getByRole('button', { name: '打开全部评论', exact: true })
    : page.locator('button').filter({ hasText: /^评论\s*\d*$/ }).first()
  await button.click()
  const drawer = page.locator('aside[aria-hidden="false"]')
  await drawer.getByText('正在读取评论…', { exact: true }).waitFor({ state: 'hidden' })
  return drawer
}

async function testComment({ tab, targetWeek, planID = '' }) {
  const plan = Boolean(planID)
  const path = plan ? `/api/biz-okr/plans/${planID}/comments` : '/api/biz-okr/comments'
  const listPath = plan ? path : `${path}?quarter=${quarter}&week=${targetWeek}`
  const route = plan
    ? `/#/biz-okr?tab=okr-plan&quarter=${planQuarter}&plan_id=${planID}`
    : `/#/biz-okr?tab=${tab}&quarter=${quarter}&week=${targetWeek}`
  await page.goto(`${base}${route}`)
  const drawer = await openCommentDrawer(plan)
  const content = `[OKR真实验收 ${runID}] 储节节本人评论读写检查`
  await drawer.getByPlaceholder(plan ? '对当前 Plan 发表评论，输入 @ 选择提醒人…' : '对本周页面发表评论，输入 @ 选择提醒人…').fill(content)
  const response = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname === path)
  await drawer.getByRole('button', { name: '发布评论', exact: true }).click()
  const saved = await response
  const result = await saved.json()
  assert.equal(saved.status(), 200, JSON.stringify(result))
  const id = result.data?.id || ''
  assert(id, 'Comment response has no ID')
  createdComments.push({ id, listPath })
  assert.equal(result.data.author_open_id, expectedOpenID, 'Comment author differs from logged-in user')
  assert.equal(result.data.author_name, expectedName)
  await page.locator(`#comment-${id}`).getByText(content).waitFor()
  await page.reload()
  await openCommentDrawer(plan)
  const thread = page.locator(`#comment-${id}`)
  await thread.getByText(content).waitFor()
  const replyText = `[OKR真实验收 ${runID}] 回复`
  await thread.getByRole('button', { name: /^回复/ }).first().click()
  await thread.getByPlaceholder(new RegExp(`回复 ${expectedName}`)).fill(replyText)
  const replyResponse = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname === path)
  await thread.getByRole('button', { name: '回复', exact: true }).last().click()
  const reply = await replyResponse
  const replyBody = await reply.json()
  assert.equal(reply.status(), 200, JSON.stringify(replyBody))
  assert.equal(replyBody.data.author_open_id, expectedOpenID)
  await thread.getByText(replyText).waitFor()
  await thread.getByRole('button', { name: '编辑', exact: true }).first().click()
  const editedContent = `${content}（已编辑）`
  await thread.getByPlaceholder('修改评论…').fill(editedContent)
  const editResponse = page.waitForResponse(value => value.request().method() === 'PUT' && new URL(value.url()).pathname === `/api/biz-okr/comments/${id}`)
  await thread.getByRole('button', { name: '保存', exact: true }).click()
  assert.equal((await editResponse).status(), 200)
  await thread.getByText(editedContent).waitFor()
  await thread.getByRole('button', { name: '标记解决' }).click()
  await thread.waitFor({ state: 'hidden' })
  await page.locator('aside[aria-hidden="false"]').getByRole('button', { name: /显示历史评论/ }).click()
  await thread.getByText(editedContent).waitFor()
  await thread.getByRole('button', { name: '重新打开' }).click()
  await page.locator('aside[aria-hidden="false"]').getByRole('button', { name: '隐藏历史评论' }).click()
  await thread.getByText(editedContent).waitFor()
  const reread = await api(listPath)
  const persisted = reread.body.data?.comments?.find(comment => comment.id === id)
  assert.equal(persisted?.content, editedContent)
  assert.equal(persisted?.replies?.[0]?.content, replyText)
  evidence.results.push({ case: plan ? 'plan-comment' : `${tab}-comment`, status: 'passed', id, checks: ['author', 'save', 'reload', 'reply', 'edit', 'resolve', 'history', 'reopen', 'readback'] })
  await cleanupComment()
}

async function testExport(tab, targetWeek) {
  await page.goto(`${base}/#/biz-okr?tab=${tab}&quarter=${quarter}&week=${targetWeek}`)
  const exportButton = page.getByRole('button', { name: tab === 'review-meeting' ? '导出 OKR Review' : '导出全部 OKR', exact: true })
  await exportButton.waitFor()
  const response = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname === '/api/biz-okr/feishu-documents')
  await exportButton.click()
  const saved = await response
  const result = await saved.json()
  assert.equal(saved.status(), 201, JSON.stringify(result))
  assert(result.data?.document_id && result.data?.url, 'Real export returned no document')
  assert.equal(await page.getByRole('link', { name: '打开文档', exact: true }).getAttribute('href'), result.data.url)
  evidence.artifacts.push({ kind: 'document', id: result.data.document_id, url: result.data.url, owner_verified: false })
  evidence.results.push({ case: tab === 'review-meeting' ? 'review-export' : 'weekly-export', status: 'created_owner_readback_pending', document_id: result.data.document_id, url: result.data.url })
}

async function testNavigation() {
  for (const [tab, targetWeek] of [
    ['review-fill', week], ['review-meeting', week],
    ['weekly-fill', weeklyWeek], ['weekly-meeting', weeklyWeek],
    ['okr-plan', ''],
  ]) {
    const query = tab === 'okr-plan' ? `quarter=${planQuarter}` : `quarter=${quarter}&week=${targetWeek}`
    await page.goto(`${base}/#/biz-okr?tab=${tab}&${query}`)
    await page.locator('#okr-workspace-root').waitFor()
    const me = await api('/api/biz-okr/me')
    assert.equal(me.body.data?.user?.open_id, expectedOpenID, `Identity changed in ${tab}`)
    assert.equal(await page.getByRole('heading', { name: '登录 OKR' }).count(), 0, `Login gate appeared in ${tab}`)
  }
  evidence.results.push({ case: 'navigation', status: 'passed', checks: ['review-fill', 'review-meeting', 'weekly-fill', 'weekly-meeting', 'okr-plan', 'identity-on-each-page'] })
}

async function testPeople() {
  const search = await api(`/api/biz-okr/people/search?q=${encodeURIComponent(expectedName)}`)
  assert.equal(search.status, 200, JSON.stringify(search.body))
  const candidate = search.body.data?.candidates?.find(item => item.name === expectedName && item.email)
  assert(candidate, 'Real directory did not return a uniquely identifiable tester with full email')
  const again = await api(`/api/biz-okr/people/search?q=${encodeURIComponent(expectedName)}`)
  assert.equal(again.status, 200, JSON.stringify(again.body))
  assert(again.body.data?.candidates?.some(item => item.email === candidate.email), 'Repeated search lost the same tester')
  evidence.results.push({ case: 'people', status: 'passed', email: candidate.email, checks: ['real-directory', 'full-email', 'repeat-search'] })
  return candidate
}

async function testPlanComment() {
  await page.goto(`${base}/#/biz-okr?tab=okr-plan&quarter=${planQuarter}`)
  console.error('Plan: create draft')
  await page.getByRole('button', { name: '新建 Plan', exact: true }).click()
  await page.getByLabel('Plan 名称', { exact: true }).fill(`[OKR真实验收 ${runID}] 临时 Plan`)
  const body = await uiWrite('/api/biz-okr/plans', 'POST', () => page.getByRole('button', { name: '确认新建', exact: true }).click(), 201)
  createdPlanID = body?.id || ''
  assert(createdPlanID, 'Plan response has no ID')
  evidence.artifacts.push({ kind: 'plan', id: createdPlanID, cleaned_up: false })
  // The POST response arrives before the UI's follow-up list fetch and
  // publishPlan finish. Never write while an older Plan is still selected.
  await page.waitForFunction(id => document.querySelector('select[aria-label="选择 Plan"]')?.value === id, createdPlanID)
  console.error('Plan: create objective')
  await page.getByRole('button', { name: '+ 新建 O', exact: true }).click()
  await page.getByLabel('目标名称', { exact: true }).fill(`[OKR真实验收 ${runID}] 目标`)
  const objective = await uiWrite(`/api/biz-okr/plans/${createdPlanID}/objectives`, 'POST', () => page.getByRole('button', { name: '创建目标', exact: true }).click(), 201)
  const objectiveID = objective?.objectives?.[0]?.id
  assert(objectiveID, 'Temporary Plan has no objective')
  console.error('Plan: create KR')
  const objectivePath = `/api/biz-okr/plans/${createdPlanID}/objectives/${objectiveID}`
  await page.getByRole('button', { name: '+ 新建 KR', exact: true }).click()
  await page.getByPlaceholder('填写新 KR 内容').fill('真实验收 KR')
  await page.getByLabel('新 KR 业务分类', { exact: true }).fill('验收')
  await page.getByRole('button', { name: '确定', exact: true }).click()
  await uiWrite(objectivePath, 'PATCH', () => page.getByRole('button', { name: '创建', exact: true }).click())
  await uiWrite(objectivePath, 'PATCH', () => page.getByLabel('优先级标签', { exact: true }).selectOption('p0'))
  console.error('Plan: choose owner')
  await page.getByRole('button', { name: '管理关联人', exact: true }).first().click()
  await page.getByPlaceholder('输入姓名或邮箱搜索').fill(expectedName)
  const person = await testPeople()
  await uiWrite(objectivePath, 'PATCH', () => page.getByRole('button', { name: new RegExp(`${expectedName}.*${person.email.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`) }).click())
  const plan = await api(`/api/biz-okr/plans/${createdPlanID}`)
  assert.equal(plan.status, 200)
  assert.equal(plan.body.data?.objectives?.[0]?.krs?.[0]?.owners?.[0]?.email, person.email)
  await page.reload()
  await page.getByLabel('选择 Plan', { exact: true }).selectOption(createdPlanID)
  await page.getByLabel('KR 内容', { exact: true }).waitFor()
  evidence.results.push({ case: 'plan-definition', status: 'passed', plan_id: createdPlanID, checks: ['create-plan', 'create-objective', 'create-kr', 'priority', 'real-owner', 'reload'] })
  console.error('Plan: comment')
  await testComment({ planID: createdPlanID })
}

async function cleanupPlan() {
  if (!createdPlanID) return
  const before = await api(`/api/biz-okr/plans/${encodeURIComponent(createdPlanID)}`)
  assert.equal(before.status, 200, 'Could not read this run\'s Plan before cleanup')
  const result = await api(`/api/biz-okr/plans/${encodeURIComponent(createdPlanID)}`, { method: 'DELETE', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ delete_token: before.body.data?.delete_token }) })
  assert.equal(result.status, 200, `Could not delete this run's Plan: ${JSON.stringify(result.body)}`)
  const after = await api(`/api/biz-okr/plans/${encodeURIComponent(createdPlanID)}`)
  assert.equal(after.status, 404, 'Deleted test Plan is still readable')
  const artifact = evidence.artifacts.find(item => item.kind === 'plan' && item.id === createdPlanID)
  if (artifact) artifact.cleaned_up = true
  createdPlanID = ''
}

async function testSelfMention() {
  const person = await testPeople()
  await page.goto(`${base}/#/biz-okr?tab=review-fill&quarter=${quarter}&week=${week}`)
  const drawer = await openCommentDrawer()
  const editor = drawer.getByPlaceholder('对本周页面发表评论，输入 @ 选择提醒人…')
  await editor.fill(`[OKR真实验收 ${runID}] @${expectedName}`)
  const choice = drawer.locator('button').filter({ hasText: person.email }).first()
  await choice.waitFor()
  await choice.click()
  const content = await editor.inputValue()
  assert(content.includes(`@${expectedName}`), 'Self mention was not inserted into comment text')
  const response = page.waitForResponse(value => value.request().method() === 'POST' && new URL(value.url()).pathname === '/api/biz-okr/comments')
  await drawer.getByRole('button', { name: '发布评论', exact: true }).click()
  const saved = await response
  const body = await saved.json()
  assert.equal(saved.status(), 200, JSON.stringify(body))
  const id = body.data?.id
  assert(id, 'Self-mention comment response has no ID')
  createdComments.push({ id, listPath: `/api/biz-okr/comments?quarter=${quarter}&week=${week}` })
  assert.equal(body.data.author_open_id, expectedOpenID)
  assert(body.data.mentions?.some(mention => mention.email === person.email && mention.name === expectedName), 'The selected recipient was not persisted')
  let delivery
  for (let attempt = 0; attempt < 15; attempt++) {
    const list = await api(`/api/biz-okr/comments?quarter=${quarter}&week=${week}`)
    delivery = list.body.data?.comments?.find(comment => comment.id === id)?.notifications?.find(item => item.email === person.email)
    if (delivery && !['pending', 'sending'].includes(delivery.status)) break
    await page.waitForTimeout(2000)
  }
  assert(delivery, 'No delivery record was persisted for the selected recipient')
  assert.equal(delivery.status, 'delivered', `Self-mention delivery did not complete: ${JSON.stringify(delivery)}`)
  evidence.results.push({ case: 'self-mention', status: 'sent_inbox_readback_pending', id, recipient: person.email, message_id: delivery.message_id })
  await cleanupComment()
}

try {
  await verifyIdentity()
  const available = {
    navigation: () => testNavigation(),
    people: () => testPeople(),
    'review-comment': () => testComment({ tab: 'review-fill', targetWeek: week }),
    'weekly-comment': () => testComment({ tab: 'weekly-fill', targetWeek: weeklyWeek }),
    'plan-comment': () => testPlanComment(),
    'review-export': () => testExport('review-meeting', week),
    'weekly-export': () => testExport('weekly-meeting', weeklyWeek),
    'self-mention': () => testSelfMention(),
  }
  for (const name of cases) {
    console.error(`Running real OKR case: ${name}`)
    try {
      await available[name]()
      assert.equal(browserErrors.length, 0, `Browser errors in ${name}: ${browserErrors.join('; ')}`)
    } catch (error) {
      evidence.results.push({ case: name, status: 'failed', reason: error instanceof Error ? error.message : String(error) })
      process.exitCode = 1
    } finally {
      browserErrors.length = 0
      try { await cleanupComment() } catch (error) {
        evidence.results.push({ case: `${name}-cleanup`, status: 'failed', reason: error instanceof Error ? error.message : String(error) })
        process.exitCode = 1
      }
      try { await cleanupPlan() } catch (error) {
        evidence.results.push({ case: `${name}-plan-cleanup`, status: 'failed', reason: error instanceof Error ? error.message : String(error) })
        process.exitCode = 1
      }
    }
  }
} catch (error) {
  evidence.results.push({ case: 'run', status: 'failed', reason: error instanceof Error ? error.message : String(error) })
  process.exitCode = 1
} finally {
  try { await cleanupComment() } catch (error) {
    evidence.results.push({ case: 'cleanup', status: 'failed', reason: error instanceof Error ? error.message : String(error) })
    process.exitCode = 1
  }
  try { await cleanupPlan() } catch (error) {
    evidence.results.push({ case: 'plan-cleanup', status: 'failed', reason: error instanceof Error ? error.message : String(error) })
    process.exitCode = 1
  }
  if (sessionFile && sessionToken) {
    try {
      const logout = await api('/api/biz-okr/auth/logout', { method: 'POST' })
      assert.equal(logout.status, 200, 'Could not revoke short test session')
      unlinkSync(sessionFile)
    } catch (error) {
      evidence.results.push({ case: 'session-cleanup', status: 'failed', reason: error instanceof Error ? error.message : String(error) })
      process.exitCode = 1
    }
  }
  console.log(JSON.stringify(evidence, null, 2))
  await browser.close()
}
