import assert from 'node:assert/strict'
import test from 'node:test'
import { withOKRScope, withOKRTarget } from '../src/okr/chatContext.ts'

test('OKR chat publishes the weekly scope and clears targets from the previous scope', () => {
  assert.deepEqual(withOKRScope({
    tab: 'weekly-fill', quarter: '2026-Q3', week: '2026-W36', kr_id: 'old-kr', point_id: 'old-point',
  }, 'weekly-report', '2026-Q2', '2026-W15'), {
    tab: 'weekly-fill', quarter: '2026-Q2', week: '2026-W15',
  })
})

test('OKR chat publishes selected stable IDs without a strict page DTO', () => {
  assert.deepEqual(withOKRTarget(
    { tab: 'weekly-fill', quarter: '2026-Q2', week: '2026-W15', kr_id: 'old' },
    'weekly-report',
    '2026-Q2',
    '2026-W15',
    { objectiveId: 'o-1', krId: 'kr-1', pointId: 'point-1', progressId: 'progress-1' },
  ), {
    tab: 'weekly-fill', quarter: '2026-Q2', week: '2026-W15',
    objective_id: 'o-1', kr_id: 'kr-1', point_id: 'point-1', progress_id: 'progress-1',
  })
})

test('OKR chat clears a stale item target when navigation selects a broad scope', () => {
  assert.deepEqual(withOKRTarget(
    { tab: 'manage', quarter: '2026-Q3', objective_id: 'o-old', kr_id: 'kr-old', point_id: 'point-old' },
    'okr',
    '2026-Q3',
    '',
    {},
  ), { tab: 'manage', quarter: '2026-Q3' })
})

test('OKR chat removes weekly scope and targets on stable definitions', () => {
  assert.deepEqual(withOKRScope({
    tab: 'structure', quarter: '2026-Q2', week: '2026-W15', progress_id: 'progress-1',
  }, 'okr', '2026-Q2', ''), { tab: 'structure', quarter: '2026-Q2' })
})

test('OKR Plan keeps the route quarter but does not keep weekly scope', () => {
  assert.deepEqual(withOKRScope({
    share: 'weekly', tab: 'okr-plan', quarter: '2026-Q4', week: '2026-W36', progress_id: 'progress-1',
  }, 'okr', '2026-Q4', ''), { share: 'weekly', tab: 'okr-plan', quarter: '2026-Q4' })
})
