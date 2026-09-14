import assert from 'node:assert/strict'
import test from 'node:test'

import { createPeopleSearchCache } from '../src/okr/emily/peopleSearchCache.ts'

function peopleResult(name: string) {
  return {
    candidates: [{ email: `${name.toLowerCase()}@example.test`, name, department: '测试部门' }],
    has_more: false,
  }
}

test('OKR people searches share normalized positive results', async () => {
  let calls = 0
  const searchOKRPeople = createPeopleSearchCache(async () => {
    calls++
    return peopleResult('AliceCache')
  })

  const first = await searchOKRPeople(' AliceCache ')
  const second = await searchOKRPeople('alicecache')

  assert.equal(calls, 1)
  assert.equal(first.candidates[0]?.name, 'AliceCache')
  assert.deepEqual(second, first)
})

test('canceling one picker does not cancel a shared OKR people request', async () => {
  let release!: () => void
  const gate = new Promise<void>((resolve) => { release = resolve })
  let calls = 0
  const searchOKRPeople = createPeopleSearchCache(async () => {
    calls++
    await gate
    return peopleResult('ConcurrentCache')
  })
  const controller = new AbortController()

  const canceled = searchOKRPeople('ConcurrentCache', controller.signal)
  const active = searchOKRPeople(' concurrentcache ')
  controller.abort()
  release()

  await assert.rejects(canceled, { name: 'AbortError' })
  assert.equal((await active).candidates[0]?.name, 'ConcurrentCache')
  assert.equal(calls, 1)
})

test('positive searches cache for 300 minutes and empty searches for 2 minutes', async (t) => {
  let now = 1_000_000
  t.mock.method(Date, 'now', () => now)
  let positiveCalls = 0
  const positive = createPeopleSearchCache(async () => {
    positiveCalls++
    return peopleResult(`Positive${positiveCalls}`)
  })
  await positive('ttl-positive')
  now += 299 * 60 * 1000
  assert.equal((await positive('ttl-positive')).candidates[0]?.name, 'Positive1')
  now += 2 * 60 * 1000
  assert.equal((await positive('ttl-positive')).candidates[0]?.name, 'Positive2')

  let emptyCalls = 0
  const empty = createPeopleSearchCache(async () => {
    emptyCalls++
    return { candidates: [], has_more: false }
  })
  await empty('ttl-empty')
  now += 60 * 1000
  await empty('ttl-empty')
  assert.equal(emptyCalls, 1)
  now += 2 * 60 * 1000
  await empty('ttl-empty')
  assert.equal(emptyCalls, 2)
})
