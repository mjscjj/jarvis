import assert from 'node:assert/strict'
import test from 'node:test'

import { isWeeklyShareViewState, WEEKLY_SHARE_NAV, weeklyShareTab, weeklyShareURL, weeklyShareWorkspaceTab } from '../src/okr/emily/share.ts'

test('weekly share link keeps only the selected dataset and view', () => {
  assert.equal(
    weeklyShareURL('https://emily.example/path#/biz-okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', { dataset: 'weekly', view: 'fill' }, ''),
    'https://emily.example/path#/weekly-report?tab=weekly-fill&quarter=2026-Q3&week=2026-W36',
  )
  assert.equal(
    weeklyShareURL('https://emily.example/path#/biz-okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', { dataset: 'weekly', view: 'meeting' }, ''),
    'https://emily.example/path#/weekly-report?tab=weekly-meeting&quarter=2026-Q3&week=2026-W36',
  )
  assert.equal(
    weeklyShareURL('http://10.78.205.9:18802/#/biz-okr?quarter=2026-Q3&tab=review-fill&week=2026-W36', { dataset: 'review', view: 'fill' }, ''),
    'http://10.78.205.9:18802/#/weekly-report?tab=review-fill&quarter=2026-Q3&week=2026-W36',
  )
  assert.equal(
    weeklyShareURL('http://10.78.205.9:18802/#/biz-okr?quarter=2026-Q3&tab=review-fill&week=2026-W36', { dataset: 'review', view: 'meeting' }, ''),
    'http://10.78.205.9:18802/#/weekly-report?tab=review-meeting&quarter=2026-Q3&week=2026-W36',
  )
})

test('a configured public address replaces the IP the author browsed in on', () => {
  assert.equal(
    weeklyShareURL('http://10.78.205.9:18802/#/biz-okr?tab=review-fill', { dataset: 'review', view: 'fill' }, 'http://emily.example:18802'),
    'http://emily.example:18802/#/weekly-report?tab=review-fill',
  )
  assert.equal(
    weeklyShareURL('http://127.0.0.1:18802/#/weekly-report?tab=weekly-fill', { dataset: 'weekly', view: 'meeting' }, 'https://emily.example/'),
    'https://emily.example/#/weekly-report?tab=weekly-meeting',
  )
})

test('weekly share scope resolves OKR Plan and all four weekly pages', () => {
  assert.equal(isWeeklyShareViewState({ share: 'weekly' }), true)
  assert.equal(isWeeklyShareViewState({}), false)
  assert.deepEqual(WEEKLY_SHARE_NAV.map((item) => item.key), [
    'okr-plan',
    'review-fill',
    'review-meeting',
    'weekly-fill',
    'weekly-meeting',
  ])
  assert.equal(WEEKLY_SHARE_NAV[0].label, 'OKR Plan')
  assert.equal(weeklyShareTab('okr-plan'), 'okr-plan')
  assert.equal(weeklyShareWorkspaceTab('okr-plan'), undefined)
  assert.equal(weeklyShareWorkspaceTab('review-fill'), 'review-fill')
  assert.equal(weeklyShareTab('weekly-meeting'), 'weekly-meeting')
  assert.equal(weeklyShareTab('weekly-fill'), 'weekly-fill')
  assert.equal(weeklyShareTab('review-fill'), 'review-fill')
  assert.equal(weeklyShareTab('review-meeting'), 'review-meeting')
  // The retired single-page Review link falls back instead of resolving.
  assert.equal(weeklyShareTab('okr-review'), 'weekly-fill')
  assert.equal(weeklyShareTab('manage'), 'weekly-fill')
  assert.equal(weeklyShareTab('agent-flows'), 'weekly-fill')
})
