import assert from 'node:assert/strict'
import test from 'node:test'

import { isWeeklyShareViewState, weeklyShareTab, weeklyShareURL } from '../src/okr/emily/share.ts'

test('weekly share link keeps only the selected page mode', () => {
  assert.equal(
    weeklyShareURL('https://emily.example/path#/okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', 'fill'),
    'https://emily.example/path#/weekly-report?tab=weekly-fill',
  )
  assert.equal(
    weeklyShareURL('https://emily.example/path#/okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', 'meeting'),
    'https://emily.example/path#/weekly-report?tab=weekly-meeting',
  )
  assert.equal(
    weeklyShareURL('http://10.78.205.9:18802/#/okr?quarter=2026-Q3&tab=weekly-meeting&week=2026-W36', 'meeting'),
    'http://10.78.205.9:18802/#/weekly-report?tab=weekly-meeting',
  )
})

test('weekly share scope only resolves the two weekly pages', () => {
  assert.equal(isWeeklyShareViewState({ share: 'weekly' }), true)
  assert.equal(isWeeklyShareViewState({}), false)
  assert.equal(weeklyShareTab('weekly-meeting'), 'weekly-meeting')
  assert.equal(weeklyShareTab('weekly-fill'), 'weekly-fill')
  assert.equal(weeklyShareTab('manage'), 'weekly-fill')
  assert.equal(weeklyShareTab('agent-flows'), 'weekly-fill')
})
