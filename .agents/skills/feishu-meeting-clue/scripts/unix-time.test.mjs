import assert from 'node:assert/strict'
import test from 'node:test'
import { formatUnixTime } from './unix-time.mjs'

const seconds = iso => String(Date.parse(iso) / 1000)

test('UTC and Shanghai share the instant but may fall on different dates', () => {
  const value = seconds('2026-09-09T17:00:00Z')
  assert.deepEqual(formatUnixTime(value, 'Asia/Shanghai'), {
    unix_seconds: value, rfc3339: '2026-09-10T01:00:00+08:00',
  })
  assert.equal(formatUnixTime(value, 'UTC').rfc3339, '2026-09-09T17:00:00Z')
  assert.equal(formatUnixTime(seconds('2026-09-09T16:00:00Z'), 'Asia/Shanghai').rfc3339,
    '2026-09-10T00:00:00+08:00')
})

test('configured timezone observes daylight saving changes', () => {
  assert.equal(formatUnixTime(seconds('2026-03-08T06:59:00Z'), 'America/New_York').rfc3339,
    '2026-03-08T01:59:00-05:00')
  assert.equal(formatUnixTime(seconds('2026-03-08T07:00:00Z'), 'America/New_York').rfc3339,
    '2026-03-08T03:00:00-04:00')
})

test('invalid timezone and non-second inputs fail explicitly', () => {
  assert.throws(() => formatUnixTime('1', 'not-a-timezone'))
  assert.throws(() => formatUnixTime('1', ''))
  for (const value of ['', '1.5', '2026-09-10', '9999999999999999999']) {
    assert.throws(() => formatUnixTime(value, 'UTC'))
  }
})
