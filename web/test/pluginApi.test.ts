import assert from 'node:assert/strict'
import test from 'node:test'

import { getPlugin } from '../src/api.ts'

test('plugin detail reads only the selected plugin instead of the full live catalog', async () => {
  const originalFetch = globalThis.fetch
  let requestedURL = ''
  globalThis.fetch = async (input) => {
    requestedURL = String(input)
    return new Response(JSON.stringify({
      code: 0,
      data: { id: 'product-management', skills: [] },
    }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }

  try {
    const plugin = await getPlugin('product-management')
    assert.equal(plugin.id, 'product-management')
    assert.equal(requestedURL, '/api/plugins/product-management')
  } finally {
    globalThis.fetch = originalFetch
  }
})
