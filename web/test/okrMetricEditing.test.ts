import assert from 'node:assert/strict'
import test from 'node:test'

import { canAddMetric } from '../src/okr/emily/metricEditing.ts'

test('Review fill can seed the first weekly metric without unlocking definition structure', () => {
  assert.equal(canAddMetric(false, true, 0), true)
  assert.equal(canAddMetric(false, true, 1), false)
})

test('management can add metrics and meeting views stay read-only', () => {
  assert.equal(canAddMetric(false, false, 0), true)
  assert.equal(canAddMetric(false, false, 2), true)
  assert.equal(canAddMetric(true, true, 0), false)
  assert.equal(canAddMetric(true, false, 0), false)
})
