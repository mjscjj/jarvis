import assert from 'node:assert/strict'
import test from 'node:test'

import { weeklyShareURL } from '../src/okr/emily/share.ts'

test('weekly share link keeps only the selected page mode', () => {
  assert.equal(
    weeklyShareURL('https://emily.example/path#/okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', 'fill'),
    'https://emily.example/path#/weekly-report?tab=weekly-fill',
  )
  assert.equal(
    weeklyShareURL('https://emily.example/path#/okr?quarter=2026-Q3&tab=weekly-fill&week=2026-W36', 'meeting'),
    'https://emily.example/path#/weekly-report?tab=weekly-meeting',
  )
})
