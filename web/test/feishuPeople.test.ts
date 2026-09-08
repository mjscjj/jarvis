import assert from 'node:assert/strict'
import test from 'node:test'

import { searchFeishuPeople } from '../src/api.ts'

test('all people pickers use the shared Feishu directory endpoint', async (t) => {
  let requestedPath = ''
  let requestedMethod = ''
  const controller = new AbortController()
  t.mock.method(globalThis, 'fetch', async (path: string | URL | Request, init?: RequestInit) => {
    requestedPath = String(path)
    requestedMethod = init?.method ?? ''
    assert.equal(init?.signal, controller.signal)
    return new Response(JSON.stringify({
      code: 0,
      data: {
        candidates: [{
          open_id: 'ou_1',
          name: '张 三',
          email: 'zhangsan@example.com',
          department: '测试部门',
          p2p_chat_id: 'oc_1',
          is_external: false,
          has_chatted: true,
        }],
        has_more: false,
      },
    }), { status: 200 })
  })

  const result = await searchFeishuPeople('张 三@example.com', controller.signal)

  assert.equal(requestedPath, '/api/people/search?q=%E5%BC%A0+%E4%B8%89%40example.com')
  assert.equal(requestedMethod, 'GET')
  assert.equal(result.candidates[0].open_id, 'ou_1')
  assert.equal(result.has_more, false)
})
