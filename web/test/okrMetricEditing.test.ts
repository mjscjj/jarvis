import assert from 'node:assert/strict'
import test from 'node:test'

import { canAddMetric } from '../src/okr/emily/metricEditing.ts'

test('Review fill can add weekly metric rows without changing the definition', () => {
  assert.equal(canAddMetric(false), true)
})

test('read-only and meeting views cannot add metric rows', () => {
  assert.equal(canAddMetric(true), false)
})
