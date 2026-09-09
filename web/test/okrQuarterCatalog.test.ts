import assert from 'node:assert/strict'
import test from 'node:test'

import { fallbackQuarterForUnavailableScope, quarterOptions } from '../src/okr/emily/quarterCatalog.ts'

test('quarter selector never displays a different quarter from its current value', () => {
  assert.deepEqual(quarterOptions('2026-Q4', ['2026-Q3', '2026-Q2']), ['2026-Q4', '2026-Q3', '2026-Q2'])
  assert.deepEqual(quarterOptions('2026-Q3', ['2026-Q3', '2026-Q2']), ['2026-Q3', '2026-Q2'])
})

test('weekly scope falls back only when the requested quarter has no formal OKR', () => {
  assert.equal(fallbackQuarterForUnavailableScope('2026-Q4', ['2026-Q3', '2026-Q2']), '2026-Q3')
  assert.equal(fallbackQuarterForUnavailableScope('2026-Q3', ['2026-Q3', '2026-Q2']), undefined)
  assert.equal(fallbackQuarterForUnavailableScope('2026-Q4', []), undefined)
})
