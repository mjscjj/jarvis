import assert from 'node:assert/strict'
import test from 'node:test'

import { APIRequestError, getChatHistory, isMissingChatHistoryError } from '../src/api.ts'

test('only missing chat history starts the new-session fallback', () => {
  assert.equal(isMissingChatHistoryError(new APIRequestError('chat history not found', 404, 40461)), true)
  assert.equal(isMissingChatHistoryError(new APIRequestError('other resource not found', 404, 40400)), false)
  assert.equal(isMissingChatHistoryError(new APIRequestError('chat service unavailable', 503, 50300)), false)
  assert.equal(isMissingChatHistoryError(new Error('network failed')), false)
})

test('chat history API preserves the missing-history status for fallback', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = async () => new Response(JSON.stringify({
    code: 40461,
    msg: 'chat history not found',
  }), {
    status: 404,
    headers: { 'Content-Type': 'application/json' },
  })

  try {
    await assert.rejects(
      getChatHistory('http://127.0.0.1:18801', 'missing-thread'),
      (cause: unknown) => isMissingChatHistoryError(cause),
    )
  } finally {
    globalThis.fetch = originalFetch
  }
})
