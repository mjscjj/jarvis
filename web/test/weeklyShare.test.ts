import assert from 'node:assert/strict'
import test from 'node:test'

import { isWeeklyShareViewState, weeklyShareTab, weeklyShareURL } from '../src/okr/emily/share.ts'

test('weekly share link keeps only the selected dataset and view', () => {
  assert.equal(
    weeklyShareURL('https://emily.example/path#/okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', { dataset: 'weekly', view: 'fill' }),
    'https://emily.example/path#/weekly-report?tab=weekly-fill',
  )
  assert.equal(
    weeklyShareURL('https://emily.example/path#/okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', { dataset: 'weekly', view: 'meeting' }),
    'https://emily.example/path#/weekly-report?tab=weekly-meeting',
  )
  assert.equal(
    weeklyShareURL('http://10.78.205.9:18802/#/okr?quarter=2026-Q3&tab=review-fill&week=2026-W36', { dataset: 'review', view: 'fill' }),
    'http://10.78.205.9:18802/#/weekly-report?tab=review-fill',
  )
  assert.equal(
    weeklyShareURL('http://10.78.205.9:18802/#/okr?quarter=2026-Q3&tab=review-fill&week=2026-W36', { dataset: 'review', view: 'meeting' }),
    'http://10.78.205.9:18802/#/weekly-report?tab=review-meeting',
  )
})

test('weekly share scope resolves all four weekly pages', () => {
  assert.equal(isWeeklyShareViewState({ share: 'weekly' }), true)
  assert.equal(isWeeklyShareViewState({}), false)
  assert.equal(weeklyShareTab('weekly-meeting'), 'weekly-meeting')
  assert.equal(weeklyShareTab('weekly-fill'), 'weekly-fill')
  assert.equal(weeklyShareTab('review-fill'), 'review-fill')
  assert.equal(weeklyShareTab('review-meeting'), 'review-meeting')
  // The retired single-page Review link falls back instead of resolving.
  assert.equal(weeklyShareTab('okr-review'), 'weekly-fill')
  assert.equal(weeklyShareTab('manage'), 'weekly-fill')
  assert.equal(weeklyShareTab('agent-flows'), 'weekly-fill')
})
