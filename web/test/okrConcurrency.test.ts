import assert from 'node:assert/strict'
import test from 'node:test'
import { adoptRemoteVersionsForOverwrite, mergePointProgressOnly, mergeScoreOnly, rebasePendingChanges } from '../src/okr/emily/concurrency.ts'
import type { Kr } from '../src/okr/emily/types.ts'

function kr(entries: Kr['points'][number]['entries']): Kr {
  return {
    id: 'kr-1', title: 'KR', metricNote: '', metrics: [], version: 1, weeklyCoreVersion: 1,
    points: [{ id: 'p-1', version: 1, kind: 'strategy', title: 'Point', entries }],
  }
}

test('保存期间的新草稿重放到服务端基线并保留他人新增进展', () => {
  const submitted = kr([{ id: 'mine', version: 1, status: 'in_progress', text: '已提交', docs: [], images: [] }])
  const latest = kr([{ id: 'mine', version: 1, status: 'in_progress', text: '提交后继续输入', docs: [], images: [] }])
  const remote = kr([
    { id: 'mine', version: 2, status: 'in_progress', text: '已提交', docs: [], images: [] },
    { id: 'other', version: 1, status: 'done', text: '同事新增', docs: [], images: [] },
  ])

  const merged = rebasePendingChanges(submitted, latest, remote)
  assert.deepEqual(merged.points[0].entries.map((entry) => [entry.id, entry.text, entry.version]), [
    ['mine', '提交后继续输入', 2],
    ['other', '同事新增', 1],
  ])
})

test('评分回包只更新评分，不覆盖尚未保存的文字', () => {
  const local = kr([{ id: 'draft', status: 'in_progress', text: '尚未自动保存', docs: [], images: [] }])
  const remote = kr([])
  remote.score = { value: 0.8, version: 1 }
  const merged = mergeScoreOnly(local, remote, 'kr', remote.id)
  assert.equal(merged.points[0].entries[0].text, '尚未自动保存')
  assert.deepEqual(merged.score, { value: 0.8, version: 1 })
})

test('Meego 回包只更新目标来源进展并保留人工草稿', () => {
  const local = kr([{ id: 'manual', status: 'in_progress', text: '人工草稿', docs: [], images: [] }])
  const remote = kr([{ id: 'meego-1', version: 1, status: 'done', text: 'Meego 已完成', docs: [], images: [], source: 'meego' }])
  const merged = mergePointProgressOnly(local, remote, 'p-1', 'meego')
  assert.deepEqual(merged.points[0].entries.map((entry) => entry.text), ['人工草稿', 'Meego 已完成'])
})

test('同一 KR 冲突重试采用远端版本与结构令牌并保留本地正文', () => {
  const local = kr([])
  local.title = '本地待保存正文'
  local.structureToken = 'old-structure'
  const remote = kr([])
  remote.title = '远端正文'
  remote.version = 8
  remote.structureToken = 'new-structure'

  const merged = adoptRemoteVersionsForOverwrite(local, remote)
  assert.equal(merged.title, '本地待保存正文')
  assert.equal(merged.version, 8)
  assert.equal(merged.structureToken, 'new-structure')
})
