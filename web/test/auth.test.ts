import assert from 'node:assert/strict'
import test from 'node:test'
import { apiFetch, authEvents, getAuthStatus, getSetupStatus, repairSetupLarkCredentials, setAuthRecoveryHandler } from '../src/api.ts'

test('chat requests share session expiry handling without consuming or replaying the response', async (t) => {
  let expired = 0
  const listener = () => { expired++ }
  authEvents.addEventListener('expired', listener)
  t.after(() => authEvents.removeEventListener('expired', listener))
  const response = new Response('session expired', { status: 401 })
  const fetchMock = t.mock.method(globalThis, 'fetch', async () => response)
  assert.equal(await apiFetch('/api/chat/sessions/s1/messages', { method: 'POST' }), response)
  assert.equal(response.bodyUsed, false)
  assert.equal(expired, 1)
  assert.equal(fetchMock.mock.callCount(), 1)
})

test('401 notifies session owner without replaying a write; auth endpoints do not recurse', async (t) => {
  const calls: string[] = []
  let expired = 0
  let recoveries = 0
  const listener = () => { expired++ }
  authEvents.addEventListener('expired', listener)
  setAuthRecoveryHandler(async () => { recoveries++ })
  t.after(() => {
    authEvents.removeEventListener('expired', listener)
    setAuthRecoveryHandler(null)
  })
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    return Response.json({ code: 401, msg: 'session expired' }, { status: 401 })
  })
  await assert.rejects(repairSetupLarkCredentials('test-secret'), /session expired/)
  assert.equal(expired, 1)
  assert.equal(recoveries, 0)
  assert.deepEqual(calls, ['/api/setup/lark/credentials'])
  await assert.rejects(getAuthStatus(), /session expired/)
  assert.equal(expired, 1)
  assert.equal(recoveries, 0)
})

test('readonly requests wait for session recovery and retry once', async (t) => {
  let expired = 0
  let recoveries = 0
  const listener = () => { expired++ }
  authEvents.addEventListener('expired', listener)
  setAuthRecoveryHandler(async () => { recoveries++ })
  t.after(() => {
    authEvents.removeEventListener('expired', listener)
    setAuthRecoveryHandler(null)
  })
  const calls: string[] = []
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    if (calls.length === 1) {
      return Response.json({ code: 401, msg: 'session expired' }, { status: 401 })
    }
    return Response.json({ code: 0, data: { ok: true } })
  })

  assert.deepEqual(await getSetupStatus(), { ok: true })
  assert.equal(expired, 1)
  assert.equal(recoveries, 1)
  assert.deepEqual(calls, ['/api/setup/status', '/api/setup/status'])
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
  t.mock.timers.tick(60000)
  await pending
})
