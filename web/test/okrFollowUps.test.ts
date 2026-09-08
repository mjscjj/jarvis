import assert from 'node:assert/strict'
import test from 'node:test'

import { canEditFollowUpStatus, FOLLOW_UP_STATUS_CLASS, FOLLOW_UP_STATUS_OPTIONS, isClosedFollowUp, sortFollowUpsByAssignDate } from '../src/okr/emily/followUps.ts'

test('follow-up statuses include abandoned and use the Review labels', () => {
  assert.deepEqual(FOLLOW_UP_STATUS_OPTIONS, [
    { value: 'not_started', label: '未开始' },
    { value: 'in_progress', label: '进行中' },
    { value: 'done', label: '已完成' },
    { value: 'abandoned', label: '废弃' },
  ])
  assert.equal(isClosedFollowUp('not_started'), false)
  assert.equal(isClosedFollowUp('in_progress'), false)
  assert.equal(isClosedFollowUp('done'), true)
  assert.equal(isClosedFollowUp('abandoned'), true)
  assert.equal(new Set(Object.values(FOLLOW_UP_STATUS_CLASS)).size, FOLLOW_UP_STATUS_OPTIONS.length)
})

test('Review meeting keeps the row read-only while allowing Todo status edits', () => {
  assert.equal(canEditFollowUpStatus(false, false), true)
  assert.equal(canEditFollowUpStatus(true, true), true)
  assert.equal(canEditFollowUpStatus(true, false), false)
})

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
