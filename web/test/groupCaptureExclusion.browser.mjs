// Browser regression for the complete privacy flow. The real App and
// GroupsPanel are mounted while every API call is handled by deterministic
// fixtures, so this never changes a developer's Jarvis data.
import assert from 'node:assert/strict'

const { chromium } = await import(process.env.PLAYWRIGHT_MODULE || 'playwright')
const base = process.env.CHAT_TEST_URL || 'http://127.0.0.1:18801'
const browser = await chromium.launch({
  executablePath: process.env.CHROME_EXECUTABLE,
  headless: true,
  args: ['--no-sandbox'],
})
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
page.setDefaultTimeout(8000)
const errors = []
const mutations = []
const securityMutations = []
page.on('pageerror', error => errors.push(error.message))

const now = new Date().toISOString()
const session = {
  id: 's1', title: '测试对话', agent: 'trae', model: 'test', reasoning_effort: 'medium',
  sources: [], draft: {}, archived: false, messages: [], pending_attachments: [],
  created_at: now, updated_at: now,
}
const securitySettings = { p2p_scan_enabled: true, auto_related_p2p_top_n: 20 }
const groups = [
  { id: 1, chat_id: 'oc_group', chat_mode: 'group', name: '项目群', related_group: true, capture_excluded: false, pinned: true },
  { id: 2, chat_id: 'oc_p2p', chat_mode: 'p2p', name: '同事单聊', related_group: false, capture_excluded: false, pinned: false },
  { id: 3, chat_id: 'oc_topic', chat_mode: 'topic', name: '已排除话题', related_group: false, capture_excluded: true, pinned: false },
].map(group => ({
  description: null, summary: null, last_progress_at: null, owner_open_id: null, external: false,
  tenant_key: null, project_id: null, tier: 'hot', include_in_memory: false, is_key_group: false,
  last_active_at: null, created_at: now, updated_at: now, project: null, last_scan_at: null,
  last_scan_status: null, message_count: 0, ...group,
}))

try {
  await page.route('**/api/**', async route => {
    const request = route.request()
    const url = new URL(request.url())
    const path = url.pathname
    const method = request.method()
    let data

    if (path === '/api/chat/sessions/s1' && method === 'PATCH') {
      Object.assign(session, request.postDataJSON())
      data = session
    } else if (path === '/api/security-settings' && method === 'PUT') {
      const body = request.postDataJSON()
      securityMutations.push(body)
      Object.assign(securitySettings, body)
      data = {
        settings: { ...securitySettings }, restart_required: true,
        l4_document_read: { enforceable: false, enabled: false, message: '尚不可强制' },
      }
    } else if (path === '/api/groups/capture-exclusion' && method === 'PUT') {
      const body = request.postDataJSON()
      mutations.push(body)
      for (const group of groups) {
        if (!body.group_ids.includes(group.id)) continue
        group.capture_excluded = body.excluded
        group.related_group = !body.excluded
        group.pinned = !body.excluded
      }
      data = { updated: body.group_ids.length, excluded: body.excluded }
    } else {
      assert.equal(method, 'GET', `Unexpected mutation: ${method} ${path}`)
      if (path === '/api/agent-identity') data = { display_name: 'Jarvis' }
      else if (path === '/api/auth/status') data = { status: 'authenticated', user: { username: 'Test', email: 'test@example.test' } }
      else if (path === '/api/setup/bootstrap') data = { machine_configuration_ready: true }
      else if (path === '/api/setup/status') data = {
        app_ready: true, completed: true, world_model_ready: true,
        configuration: { machine_configuration_ready: true, agent_name_configured: true },
        lark: { available: true, credential_available: true, bot: { verified: true }, user: { verified: true } },
        agent: { available: true, authenticated: true },
      }
      else if (['/api/plugin-installations', '/api/debug/failures', '/api/projects'].includes(path)) data = { items: [] }
      else if (path === '/api/chat/sessions') data = { items: [session] }
      else if (path === '/api/chat/sessions/s1') data = session
      else if (path === '/api/chat/agents') data = { items: [{ id: 'trae', name: 'TRAE', available: true, default: true }] }
      else if (path === '/api/chat/agents/trae/models') data = { items: [{ id: 'test', name: 'Test', default: true }] }
      else if (path === '/api/tasks') data = { items: [], total: 0, page: 1, page_size: Number(url.searchParams.get('page_size')) }
      else if (path === '/api/security-settings') data = {
        settings: { ...securitySettings }, restart_required: false,
        l4_document_read: { enforceable: false, enabled: false, message: '尚不可强制' },
      }
      else if (path === '/api/security-audit-events') data = { items: [] }
      else if (path === '/api/groups') {
        const filtered = url.searchParams.get('capture_state') === 'excluded'
          ? groups.filter(group => group.capture_excluded)
          : url.searchParams.get('related_only') === 'true'
            ? groups.filter(group => group.related_group)
            : groups
        data = { items: filtered, total: filtered.length, page: 1, page_size: 20, broadened: false }
      } else {
        errors.push(`Unexpected API: ${path}`)
        await route.fulfill({ status: 500, json: { code: 500, msg: `Unexpected API: ${path}` } })
        return
      }
    }
    await route.fulfill({ json: { code: 0, data } })
  })

  await page.goto(`${base}/#/manage/settings?view=security`)
  await page.getByRole('heading', { name: '会话排除名单' }).waitFor()
  const automaticCard = page.getByRole('heading', { name: '自动纳入活跃单聊' }).locator('xpath=ancestor::div[contains(@class, "ant-card")]')
  await automaticCard.getByRole('switch').click()
  await automaticCard.getByRole('button', { name: '保存单聊设置' }).click()
  await page.getByText('安全设置已保存', { exact: true }).waitFor()
  assert.deepEqual(securityMutations, [{ p2p_scan_enabled: true, auto_related_p2p_top_n: 0 }])
  await page.getByRole('button', { name: '管理排除名单' }).click()
  await page.getByText('已排除 1 个会话', { exact: false }).waitFor()
  assert.match(page.url(), /#\/memory\?capture=excluded&view=groups$/)
  await page.getByText('已排除话题', { exact: true }).waitFor()
  assert.equal(await page.getByText('项目群', { exact: true }).count(), 0)

  await page.getByText('全部会话', { exact: true }).click()
  await page.getByText('全部 3 个会话', { exact: false }).waitFor()
  const selectAll = page.locator('.ant-table-thead input[type=checkbox]')
  await selectAll.check()
  await page.getByText('已选择本页 2 个会话', { exact: true }).waitFor()
  await page.getByRole('button', { name: '批量排除监听' }).click()
  await page.locator('.ant-popconfirm-buttons .ant-btn-primary').click()
  await page.getByText('全部 3 个会话', { exact: false }).waitFor()
  assert.deepEqual(mutations[0], { group_ids: [1, 2], excluded: true })

  await page.getByText('已排除', { exact: true }).first().click()
  await page.getByText('已排除 3 个会话', { exact: false }).waitFor()
  await selectAll.check()
  await page.getByText('已选择本页 3 个会话', { exact: true }).waitFor()
  await page.getByRole('button', { name: '批量取消排除' }).click()
  await page.locator('.ant-popconfirm-buttons .ant-btn-primary').click()
  await page.getByText('已排除 0 个会话', { exact: false }).waitFor()
  assert.deepEqual(mutations[1], { group_ids: [1, 2, 3], excluded: false })
  assert.deepEqual(errors, [])
  console.log(JSON.stringify({
    result: 'passed',
    checks: [
      'automatic direct-message switch preserves global direct-message policy',
      'security entry reuses world conversation list',
      'excluded filter',
      'select all only selects eligible rows',
      'batch exclusion payload',
      'excluded list reload',
      'batch restore payload',
      'empty excluded state after restore',
    ],
  }))
} catch (error) {
  console.error(JSON.stringify({ errors, securityMutations, mutations, body: (await page.locator('body').innerText()).slice(0, 2600) }))
  throw error
} finally {
  await browser.close()
}
