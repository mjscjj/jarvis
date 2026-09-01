import assert from 'node:assert/strict'
import test from 'node:test'
import { findKrDraftIssue } from '../src/okr/emily/draftValidation.ts'
import type { Kr } from '../src/okr/emily/types.ts'

function kr(overrides: Partial<Kr> = {}): Kr {
  return {
    id: 'kr-1',
    title: 'KR2 Contact center',
    metricNote: '',
    metrics: [],
    points: [],
    ...overrides,
  }
}

test('空核心数据给出保留草稿的友好提示', () => {
  const issue = findKrDraftIssue(kr({ metrics: [{ id: 'metric-1', text: '', light: 'green' }] }))

  assert.equal(issue?.title, '核心数据还没填写完整')
  assert.match(issue?.message ?? '', /1 条核心数据/)
  assert.match(issue?.message ?? '', /其他修改仍然保留/)
})

test('一次提示列全策略和产品空白项', () => {
  const issue = findKrDraftIssue(kr({
    points: [
      { id: 'point-1', kind: 'strategy', title: ' ', entries: [] },
      { id: 'point-2', kind: 'product', title: '', entries: [] },
    ],
  }))

  assert.equal(issue?.title, '这条 KR 还没填写完整')
  assert.match(issue?.message ?? '', /1 条策略具体 KR/)
  assert.match(issue?.message ?? '', /1 条产品具体 KR/)
})

test('完整 KR 不拦截自动保存', () => {
  assert.equal(findKrDraftIssue(kr({
    metrics: [{ id: 'metric-1', text: '核心指标达到 80%', light: 'green' }],
    points: [{ id: 'point-1', kind: 'strategy', title: '完成策略', entries: [] }],
  })), undefined)
})
