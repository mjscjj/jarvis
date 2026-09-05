import assert from 'node:assert/strict'
import test from 'node:test'

import { commentCountsByTarget, commentMatchesTarget, commentTargetKey } from '../src/okr/emily/comments.ts'
import type { PageComment } from '../src/okr/emily/types.ts'

const followUpComment: PageComment = {
  id: 'comment-1',
  targetType: 'follow_up',
  targetId: 'followup-1',
  targetTitle: '确认上线节奏',
  authorName: 'Alice',
  content: '请补充日期',
  todo: false,
  resolved: false,
  createdAt: '2026-09-04T00:00:00Z',
  updatedAt: '2026-09-04T00:00:00Z',
  replies: [{
    id: 'comment-2',
    parentId: 'comment-1',
    targetType: 'follow_up',
    targetId: 'followup-1',
    targetTitle: '确认上线节奏',
    authorName: 'Bob',
    content: '收到',
    todo: false,
    resolved: false,
    createdAt: '2026-09-04T00:01:00Z',
    updatedAt: '2026-09-04T00:01:00Z',
    replies: [],
  }],
}

test('follow-up comments use the stable item id and count replies', () => {
  const target = { type: 'follow_up' as const, id: 'followup-1', title: '已改名的事项' }
  assert.equal(commentTargetKey(target), 'follow_up:followup-1')
  assert.equal(commentMatchesTarget(followUpComment, target), true)
  assert.deepEqual(commentCountsByTarget([followUpComment]), { 'follow_up:followup-1': 2 })
})

test('follow-up comments do not match another item', () => {
  assert.equal(commentMatchesTarget(followUpComment, {
    type: 'follow_up', id: 'followup-2', title: '另一条事项',
  }), false)
})
