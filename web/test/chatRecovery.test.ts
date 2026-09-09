import assert from 'node:assert/strict'
import test from 'node:test'

import { APIRequestError, getChatHistory, isMissingChatHistoryError, listChatThreads, resolveChatBaseURL, stopChatTurn } from '../src/api.ts'

test('chat uses same-origin behind HTTPS and the sibling port for direct LAN access', () => {
  assert.equal(resolveChatBaseURL('https://emily.bytedance.net', 18801), 'https://emily.bytedance.net')
  assert.equal(resolveChatBaseURL('http://10.199.199.203:18802', 18801), 'http://10.199.199.203:18801')
})

test('only missing chat history starts the new-session fallback', () => {
  assert.equal(isMissingChatHistoryError(new APIRequestError('chat history not found', 404, 40461)), true)
  assert.equal(isMissingChatHistoryError(new APIRequestError('other resource not found', 404, 40400)), false)
  assert.equal(isMissingChatHistoryError(new APIRequestError('chat service unavailable', 503, 50300)), false)
  assert.equal(isMissingChatHistoryError(new Error('network failed')), false)
})

test('chat history API preserves the missing-history status for fallback', async () => {
  const originalFetch = globalThis.fetch
  let requestedURL = ''
  globalThis.fetch = async (input) => {
    requestedURL = String(input)
    return new Response(JSON.stringify({
      code: 40461,
      msg: 'chat history not found',
    }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  try {
    await assert.rejects(
      getChatHistory('http://127.0.0.1:18801', 'missing/thread'),
      (cause: unknown) => isMissingChatHistoryError(cause),
    )
    assert.equal(requestedURL, 'http://127.0.0.1:18801/api/chat?thread_id=missing%2Fthread')
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('thread list and stop use the production-routed chat root', async () => {
  const originalFetch = globalThis.fetch
  const requests: Array<{ url: string; method: string }> = []
  globalThis.fetch = async (input, init) => {
    requests.push({ url: String(input), method: init?.method || 'GET' })
    const data = String(input).includes('view=threads')
      ? { threads: [] }
      : { stopped: true }
    return new Response(JSON.stringify({ code: 0, data }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  try {
    await listChatThreads('https://emily.bytedance.net')
    await stopChatTurn('https://emily.bytedance.net', 'turn-1')
    assert.deepEqual(requests, [
      { url: 'https://emily.bytedance.net/api/chat?view=threads', method: 'GET' },
      { url: 'https://emily.bytedance.net/api/chat?action=stop&turn_id=turn-1', method: 'POST' },
    ])
  } finally {
    globalThis.fetch = originalFetch
  }
})
