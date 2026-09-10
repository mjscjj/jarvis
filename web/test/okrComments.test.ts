import assert from 'node:assert/strict'
import test from 'node:test'

import { buildCommentDocumentOrder, buildCommentOKRContextIndex, commentCountsByTarget, commentMatchesTarget, commentOKRContext, commentTargetFromThread, commentTargetKey, findCommentTargetLocation, groupCommentsByTarget, sortCommentsByDocumentOrder } from '../src/okr/emily/comments.ts'
import { insertCommentMention, mentionQueryAtCaret, mentionsPresentInContent } from '../src/okr/emily/mentions.ts'
import type { Objective, PageComment } from '../src/okr/emily/types.ts'

const followUpComment: PageComment = {
  id: 'comment-1',
  version: 1,
  deleteToken: 'delete-comment-1',
  targetType: 'follow_up',
  targetId: 'followup-1',
  targetTitle: '确认上线节奏',
  authorName: 'Alice',
  content: '请补充日期',
  mentions: [],
  todo: false,
  resolved: false,
  createdAt: '2026-09-04T00:00:00Z',
  updatedAt: '2026-09-04T00:00:00Z',
  replies: [{
    id: 'comment-2',
    version: 1,
    deleteToken: 'delete-comment-2',
    parentId: 'comment-1',
    targetType: 'follow_up',
    targetId: 'followup-1',
    targetTitle: '确认上线节奏',
    authorName: 'Bob',
    content: '收到',
    mentions: [],
    todo: false,
    resolved: false,
    createdAt: '2026-09-04T00:01:00Z',
    updatedAt: '2026-09-04T00:01:00Z',
    replies: [],
  }],
}

const objectives: Objective[] = [{
  id: 'o-1',
  title: '提升经营效率',
  krs: [{
    id: 'kr-1',
    title: '完成核心经营工具升级',
    metricNote: '',
    metrics: [{ id: 'metric-1', text: '工具覆盖率 80%' }],
    points: [{
      id: 'point-1',
      kind: 'product',
      title: '交付经营助手',
      entries: [{ id: 'entry-1', status: 'in_progress', text: '灰度发布', docs: [], images: [] }],
      previousEntries: [{ id: 'entry-old', status: 'done', text: '完成设计', docs: [], images: [] }],
    }],
  }],
}]

function comment(targetType: PageComment['targetType'], targetId: string, targetTitle = ''): PageComment {
  return {
    id: `comment-${targetId}`,
    targetType,
    targetId,
    targetTitle,
    authorName: '测试用户',
    content: '评论原文',
    mentions: [],
    todo: false,
    resolved: false,
    createdAt: '',
    updatedAt: '',
    replies: [],
  }
}

function placedComment(id: string, targetType: PageComment['targetType'], targetId: string, createdAt: string, selection?: { start: number; end: number; text: string }): PageComment {
  return {
    ...comment(targetType, targetId),
    id,
    selectedText: selection?.text,
    selectionStart: selection?.start,
    selectionEnd: selection?.end,
    createdAt,
    updatedAt: createdAt,
  }
}

test('follow-up comments use the stable item id and count replies', () => {
  const target = { type: 'follow_up' as const, id: 'followup-1', title: '已改名的事项' }
  assert.equal(commentTargetKey(target), 'follow_up:followup-1')
  assert.equal(commentMatchesTarget(followUpComment, target), true)
  assert.deepEqual(commentCountsByTarget([followUpComment]), { 'follow_up:followup-1': 2 })
  assert.equal(commentTargetFromThread(followUpComment).commentId, 'comment-1')
})

test('follow-up comments do not match another item', () => {
  assert.equal(commentMatchesTarget(followUpComment, {
    type: 'follow_up', id: 'followup-2', title: '另一条事项',
  }), false)
})

test('comment targets project to their current O and KR without another data source', () => {
  const index = buildCommentOKRContextIndex(objectives)
  assert.deepEqual(commentOKRContext(comment('objective', 'o-1'), index), {
    objective: { id: 'o-1', title: '提升经营效率' },
  })
  for (const [type, id] of [['kr', 'kr-1'], ['metric', 'metric-1'], ['point', 'point-1'], ['entry', 'entry-1'], ['entry', 'entry-old']] as const) {
    assert.deepEqual(commentOKRContext(comment(type, id), index), {
      objective: { id: 'o-1', title: '提升经营效率' },
      kr: { id: 'kr-1', title: '完成核心经营工具升级' },
    })
  }
})

test('page and follow-up comments do not invent an O or KR relation', () => {
  const index = buildCommentOKRContextIndex(objectives)
  assert.equal(commentOKRContext(comment('page', 'page-1'), index), undefined)
  assert.equal(commentOKRContext(comment('follow_up', 'follow-up-1'), index), undefined)
  assert.equal(commentOKRContext(comment('point', 'deleted-point', '已删除要点'), index), undefined)
})

test('deleted direct O or KR comments retain their saved target title', () => {
  assert.deepEqual(commentOKRContext(comment('objective', 'deleted-o', '历史 O'), {}), {
    objective: { id: 'deleted-o', title: '历史 O' },
  })
  assert.deepEqual(commentOKRContext(comment('kr', 'deleted-kr', '历史 KR'), {}), {
    kr: { id: 'deleted-kr', title: '历史 KR' },
  })
})

test('comment targets resolve to the ancestors a page must reveal before scrolling', () => {
  const entry = findCommentTargetLocation(objectives, { type: 'entry', id: 'entry-1' })
  assert.equal(entry?.objective.id, 'o-1')
  assert.equal(entry?.kr?.id, 'kr-1')
  assert.equal(entry?.point?.id, 'point-1')
  assert.equal(findCommentTargetLocation(objectives, { type: 'page', id: 'page-1' }), undefined)
  assert.equal(findCommentTargetLocation(objectives, { type: 'point', id: 'deleted-point' }), undefined)
})

test('comment threads follow the complete page model instead of creation time', () => {
  const orderedObjectives: Objective[] = [{
    id: 'o-order-1',
    title: '第一个 O',
    krs: [{
      id: 'kr-order-1',
      title: '第一个 KR',
      metricNote: '',
      metrics: [{ id: 'metric-order-1', text: '核心数据' }],
      // The page groups strategy before product even when storage input is
      // interleaved, so comment order must reuse that presentation rule.
      points: [{
        id: 'point-product', kind: 'product', title: '产品动作', entries: [{ id: 'entry-product', status: 'done', text: '产品进展', docs: [], images: [] }],
      }, {
        id: 'point-strategy', kind: 'strategy', title: '策略动作', entries: [{ id: 'entry-strategy', status: 'in_progress', text: '策略进展', docs: [], images: [] }],
      }],
    }],
  }, {
    id: 'o-order-2', title: '第二个 O', krs: [],
  }]
  const early = '2026-09-04T00:00:00Z'
  const late = '2026-09-05T00:00:00Z'
  const comments = [
    placedComment('orphan-late', 'point', 'deleted-2', late),
    placedComment('objective-2', 'objective', 'o-order-2', early),
    placedComment('product', 'point', 'point-product', early),
    placedComment('strategy-entry', 'entry', 'entry-strategy', early),
    placedComment('metric', 'metric', 'metric-order-1', early),
    placedComment('kr', 'kr', 'kr-order-1', early),
    placedComment('objective-1', 'objective', 'o-order-1', late),
    placedComment('follow-up-second', 'follow_up', 'follow-up-2', early),
    placedComment('page', 'page', 'page-id', late),
    placedComment('follow-up-first', 'follow_up', 'follow-up-1', late),
    placedComment('orphan-early', 'point', 'deleted-1', early),
  ]

  const sorted = sortCommentsByDocumentOrder(
    comments,
    buildCommentDocumentOrder(orderedObjectives, ['follow-up-1', 'follow-up-2']),
  )
  assert.deepEqual(sorted.map((value) => value.id), [
    'page',
    'follow-up-first',
    'follow-up-second',
    'objective-1',
    'kr',
    'metric',
    'strategy-entry',
    'product',
    'objective-2',
    'orphan-early',
    'orphan-late',
  ])
  assert.equal(comments[0].id, 'orphan-late')
})

test('comments on one text block sort by selection position and keep replies attached', () => {
  const reply = placedComment('reply', 'kr', 'kr-1', '2026-09-06T00:00:00Z')
  const block = { ...placedComment('whole-block', 'kr', 'kr-1', '2026-09-05T00:00:00Z'), replies: [reply] }
  const sameLater = placedComment('same-later', 'kr', 'kr-1', '2026-09-05T00:00:00Z', { start: 2, end: 4, text: '二三' })
  const sameEarlier = placedComment('same-earlier', 'kr', 'kr-1', '2026-09-04T00:00:00Z', { start: 2, end: 4, text: '二三' })
  const laterText = placedComment('later-text', 'kr', 'kr-1', '2026-09-03T00:00:00Z', { start: 8, end: 10, text: '八九' })

  const sorted = sortCommentsByDocumentOrder(
    [laterText, sameLater, block, sameEarlier],
    buildCommentDocumentOrder(objectives),
  )
  assert.deepEqual(sorted.map((value) => value.id), ['whole-block', 'same-earlier', 'same-later', 'later-text'])
  assert.equal(sorted[0].replies[0].id, 'reply')
})

test('comments group by exact source and count every root and reply', () => {
  const reply = placedComment('reply', 'kr', 'kr-1', '2026-09-06T00:00:00Z')
  const first = { ...placedComment('first', 'kr', 'kr-1', '2026-09-04T00:00:00Z'), replies: [reply] }
  const second = placedComment('second', 'kr', 'kr-1', '2026-09-05T00:00:00Z')
  const selection = placedComment('selection', 'kr', 'kr-1', '2026-09-06T00:00:00Z', { start: 2, end: 4, text: '二三' })

  const groups = groupCommentsByTarget([first, second, selection])
  assert.equal(groups.length, 2)
  assert.deepEqual(groups[0].comments.map((value) => value.id), ['first', 'second'])
  assert.equal(groups[0].messageCount, 3)
  assert.deepEqual(groups[1].comments.map((value) => value.id), ['selection'])
  assert.equal(groups[1].messageCount, 1)
})

test('mention query follows the active at token at the caret', () => {
  assert.deepEqual(mentionQueryAtCaret('请 @张若', 5), { start: 2, end: 5, query: '张若' })
  assert.equal(mentionQueryAtCaret('请 @张若 已确认', 9), undefined)
})

test('selecting a mention inserts visible text and keeps stable identity metadata', () => {
  const trigger = mentionQueryAtCaret('请 @张', 5)
  assert.ok(trigger)
  const inserted = insertCommentMention('请 @张', trigger, { openId: 'ou_zhang', name: '张若怡' }, [])
  assert.equal(inserted.content, '请 @张若怡 ')
  assert.deepEqual(inserted.mentions, [{ openId: 'ou_zhang', name: '张若怡' }])
  assert.equal(inserted.caret, inserted.content.length)
})

test('removing visible at text also removes its notification identity', () => {
  assert.deepEqual(mentionsPresentInContent('@Bob 保留，Carol 已删', [
    { openId: 'ou_bob', name: 'Bob' },
    { openId: 'ou_carol', name: 'Carol' },
  ]), [{ openId: 'ou_bob', name: 'Bob' }])
})
