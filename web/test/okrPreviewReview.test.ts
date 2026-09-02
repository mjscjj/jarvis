import assert from 'node:assert/strict'
import test from 'node:test'

import { previewReviewKey, previewReviewRequest } from '../src/okr/emily/aiReview.ts'

test('Preview point review sends the exact target scope to the review API', () => {
  const request = previewReviewRequest('2026-Q3', '2026-W37', {
    kind: 'point',
    objectiveId: 'o-1',
    krId: 'kr-1',
    pointId: 'point-1',
    title: '提升结算效率',
  })
  assert.deepEqual(request, {
    quarter: '2026-Q3',
    week: '2026-W37',
    kind: 'point',
    krId: 'kr-1',
    pointId: 'point-1',
  })
})

test('Preview full review asks for one all-scope review instead of per-item scopes', () => {
  const request = previewReviewRequest('2026-Q3', '2026-W37', { kind: 'all', title: '全部 OKR' })
  assert.deepEqual(request, {
    quarter: '2026-Q3',
    week: '2026-W37',
    kind: 'all',
    krId: undefined,
    pointId: undefined,
  })
})

test('Preview review refuses to run without a quarter and week', () => {
  assert.throws(() => previewReviewRequest('', '2026-W37', { kind: 'all', title: '全部 OKR' }), /季度/)
  assert.throws(() => previewReviewRequest('2026-Q3', '', { kind: 'all', title: '全部 OKR' }), /周次/)
})

test('Review results are keyed per target so KR and point reviews do not overwrite each other', () => {
  assert.equal(previewReviewKey({ kind: 'all', title: '全部 OKR' }), 'all')
  assert.equal(previewReviewKey({ kind: 'kr', objectiveId: 'o-1', krId: 'kr-1', title: 'KR1' }), 'kr:kr-1')
  assert.equal(previewReviewKey({ kind: 'point', objectiveId: 'o-1', krId: 'kr-1', pointId: 'p-1', title: 'P1' }), 'point:p-1')
})
