import assert from 'node:assert/strict'
import test from 'node:test'
import { withOKRScope } from '../src/okr/pageScope.ts'

test('OKR navigation updates the weekly scope', () => {
  assert.deepEqual(withOKRScope({
    tab: 'weekly-fill', quarter: '2026-Q3', week: '2026-W36',
  }, 'weekly-report', '2026-Q2', '2026-W15'), {
    tab: 'weekly-fill', quarter: '2026-Q2', week: '2026-W15',
  })
})

test('OKR definitions remove the weekly scope', () => {
  assert.deepEqual(withOKRScope({
    tab: 'structure', quarter: '2026-Q2', week: '2026-W15',
  }, 'okr', '2026-Q2', ''), { tab: 'structure', quarter: '2026-Q2' })
})

test('OKR Plan keeps the route quarter but does not keep weekly scope', () => {
  assert.deepEqual(withOKRScope({
    share: 'weekly', tab: 'okr-plan', quarter: '2026-Q4', week: '2026-W36',
  }, 'okr', '2026-Q4', ''), { share: 'weekly', tab: 'okr-plan', quarter: '2026-Q4' })
})
