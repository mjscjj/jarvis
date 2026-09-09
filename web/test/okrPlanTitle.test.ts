import assert from 'node:assert/strict'
import test from 'node:test'

import { planOptionLabel } from '../src/okr/emily/planTitle.ts'

test('Plan selector does not repeat the quarter already shown beside it', () => {
  assert.equal(planOptionLabel('2026 Q4 Platform', '2026-Q4'), 'Platform')
  assert.equal(planOptionLabel('2026-Q4 · Commercial', '2026-Q4'), 'Commercial')
  assert.equal(planOptionLabel('2026Q4', '2026-Q4'), 'Plan')
})

test('Plan selector preserves titles that do not repeat the active quarter', () => {
  assert.equal(planOptionLabel('Platform', '2026-Q4'), 'Platform')
  assert.equal(planOptionLabel('2026 Q3 Archive', '2026-Q4'), '2026 Q3 Archive')
})
