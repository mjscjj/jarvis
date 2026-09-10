import assert from 'node:assert/strict'
import test from 'node:test'
import { getPlugin, listPlugins } from '../src/api.ts'

test('plugin list and authorization detail use separate reads', async (t) => {
  const calls: string[] = []
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    const data = path === '/api/plugins/oncall'
      ? { id: 'oncall', authorization: { status: 'authorized' } }
      : { items: [{ id: 'oncall', authorization: { status: 'pending' } }] }
    return new Response(JSON.stringify({ code: 0, data }), { status: 200 })
  })

  const list = await listPlugins()
  assert.equal(list.items[0].authorization.status, 'pending')
  const detail = await getPlugin('oncall')
  assert.equal(detail.authorization.status, 'authorized')
  assert.deepEqual(calls, ['/api/plugins', '/api/plugins/oncall'])
})
