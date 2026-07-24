import assert from 'node:assert/strict'
import test from 'node:test'

import { countUnrecoveredFailures } from '../src/runtimeFailures.ts'

test('counts only unrecovered runtime failures', () => {
  const count = countUnrecoveredFailures([
    { recovered: false },
    { recovered: true },
    { recovered: false },
  ])

  assert.equal(count, 2)
})
