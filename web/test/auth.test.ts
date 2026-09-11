import assert from 'node:assert/strict'
import test from 'node:test'
import { authEvents, getAuthStatus, getSetupStatus, repairSetupLarkCredentials } from '../src/api.ts'

test('401 notifies session owner without replaying a write; auth endpoints do not recurse', async (t) => {
  const calls: string[] = []
  let expired = 0
  const listener = () => { expired++ }
  authEvents.addEventListener('expired', listener)
  t.after(() => authEvents.removeEventListener('expired', listener))
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    return new Response(JSON.stringify({ code: 401, msg: 'session expired' }), { status: 401 })
  })
  await assert.rejects(repairSetupLarkCredentials('test-secret'), /session expired/)
  assert.equal(expired, 1)
  assert.deepEqual(calls, ['/api/setup/lark/credentials'])
  await assert.rejects(getAuthStatus(), /session expired/)
  assert.equal(expired, 1)
})

test('network errors do not signal expired credentials', async (t) => {
  let expired = 0
  const listener = () => { expired++ }
  authEvents.addEventListener('expired', listener)
  t.after(() => authEvents.removeEventListener('expired', listener))
  t.mock.method(globalThis, 'fetch', async () => { throw new Error('network offline') })
  await assert.rejects(getSetupStatus(), /network offline/)
  assert.equal(expired, 0)
})

test('status timeout aborts its pending request and exposes an actionable error', async (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  t.mock.method(globalThis, 'fetch', async (_path: string, options: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      options.signal!.addEventListener('abort', () => reject(options.signal!.reason), { once: true })
    }))
  const pending = assert.rejects(getSetupStatus(), /请求超时/)
  t.mock.timers.tick(30000)
  await pending
})
