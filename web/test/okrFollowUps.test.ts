import assert from 'node:assert/strict'
import test from 'node:test'

import { sortFollowUpsByAssignDate } from '../src/okr/emily/followUps.ts'

function item(id: string, assignDate: string, sortOrder: number, createdAt = '2026-09-04T00:00:00Z') {
  return { id, assignDate, sortOrder, createdAt }
}

test('follow-ups sort by assign date ascending and put empty dates last', () => {
  const sorted = sortFollowUpsByAssignDate([
    item('empty', '', 0),
    item('later', '2026-08-25', 1),
    item('earlier', '2026-07-07', 4),
  ])
  assert.deepEqual(sorted.map((value) => value.id), ['earlier', 'later', 'empty'])
})

test('follow-ups with the same date keep their explicit order', () => {
  const sorted = sortFollowUpsByAssignDate([
    item('second', '2026-08-04', 3),
    item('first', '2026-08-04', 2),
  ])
  assert.deepEqual(sorted.map((value) => value.id), ['first', 'second'])
})
