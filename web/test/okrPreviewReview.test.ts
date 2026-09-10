import assert from 'node:assert/strict'
import test from 'node:test'

import { previewReviewKey, previewReviewRequest } from '../src/okr/emily/aiReview.ts'

test('Progress point review sends the exact target scope to the review API', () => {
  const request = previewReviewRequest({ reviewType: 'progress', quarter: '2026-Q3', week: '2026-W37' }, {
    kind: 'point',
    objectiveId: 'o-1',
    krId: 'kr-1',
    pointId: 'point-1',
    title: '提升结算效率',
  })
  assert.deepEqual(request, {
    reviewType: 'progress',
    quarter: '2026-Q3',
    week: '2026-W37',
    planId: undefined,
    kind: 'point',
    objectiveId: 'o-1',
    krId: 'kr-1',
    pointId: 'point-1',
  })
})

test('Plan full review selects the plan prompt and omits the progress week', () => {
  const request = previewReviewRequest({ reviewType: 'plan', quarter: '2026-Q4', planId: 'plan-1' }, { kind: 'all', title: '全部 OKR' })
  assert.deepEqual(request, {
    reviewType: 'plan',
    quarter: '2026-Q4',
    week: undefined,
    planId: 'plan-1',
    kind: 'all',
    objectiveId: undefined,
    krId: undefined,
    pointId: undefined,
  })
})

test('Review refuses to run without the source-specific identifiers', () => {
  assert.throws(() => previewReviewRequest({ reviewType: 'progress', quarter: '', week: '2026-W37' }, { kind: 'all', title: '全部 OKR' }), /季度/)
  assert.throws(() => previewReviewRequest({ reviewType: 'progress', quarter: '2026-Q3', week: '' }, { kind: 'all', title: '全部 OKR' }), /周次/)
  assert.throws(() => previewReviewRequest({ reviewType: 'plan', quarter: '2026-Q4', planId: '' }, { kind: 'all', title: '全部 OKR' }), /Plan/)
})

test('Review results are keyed per target so KR and point reviews do not overwrite each other', () => {
  assert.equal(previewReviewKey({ kind: 'all', title: '全部 OKR' }), 'all')
  assert.equal(previewReviewKey({ kind: 'objective', objectiveId: 'o-1', title: 'O1' }), 'objective:o-1')
  assert.equal(previewReviewKey({ kind: 'kr', objectiveId: 'o-1', krId: 'kr-1', title: 'KR1' }), 'kr:kr-1')
  assert.equal(previewReviewKey({ kind: 'point', objectiveId: 'o-1', krId: 'kr-1', pointId: 'p-1', title: 'P1' }), 'point:p-1')
})

test('Objective review sends only the selected O scope', () => {
  const request = previewReviewRequest({ reviewType: 'plan', quarter: '2026-Q4', planId: 'plan-1' }, {
    kind: 'objective',
    objectiveId: 'plan-o-1',
    title: '建立增长飞轮',
  })
  assert.deepEqual(request, {
    reviewType: 'plan',
    quarter: '2026-Q4',
    week: undefined,
    planId: 'plan-1',
    kind: 'objective',
    objectiveId: 'plan-o-1',
    krId: undefined,
    pointId: undefined,
  })
})
