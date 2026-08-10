import assert from 'node:assert/strict'
import test from 'node:test'
import {
  isReviewDateStateFresh,
  REVIEW_DATE_STATE_TTL_MS,
  reviewDateStateExpiresAt,
} from '../src/reviewDateState.ts'

test('review date state remains fresh for six hours', () => {
  const selectedAt = 1_000_000
  assert.equal(isReviewDateStateFresh(String(selectedAt), selectedAt), true)
  assert.equal(isReviewDateStateFresh(String(selectedAt), selectedAt + REVIEW_DATE_STATE_TTL_MS - 1), true)
  assert.equal(reviewDateStateExpiresAt(String(selectedAt)), selectedAt + REVIEW_DATE_STATE_TTL_MS)
})

test('review date state expires at six hours and rejects invalid timestamps', () => {
  const selectedAt = 1_000_000
  assert.equal(isReviewDateStateFresh(String(selectedAt), selectedAt + REVIEW_DATE_STATE_TTL_MS), false)
  assert.equal(isReviewDateStateFresh(String(selectedAt + 1), selectedAt), false)
  assert.equal(isReviewDateStateFresh(undefined, selectedAt), false)
  assert.equal(isReviewDateStateFresh('invalid', selectedAt), false)
})
