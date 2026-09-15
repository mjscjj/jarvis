import assert from 'node:assert/strict'
import test from 'node:test'
import { buildKrConflictFields, krConflictLocation, localConflictCopyText } from '../src/okr/emily/saveNotice.ts'
import type { Kr, Objective } from '../src/okr/emily/types.ts'

function kr(title: string): Kr {
  return { id: 'kr-1', title, metricNote: '', metrics: [], points: [{ id: 'point-1', version: 0, kind: 'product', title: '活动 Agent', owners: [], tags: [], entries: [], previousEntries: [] }], owners: [], tags: [], version: 0 }
}

test('conflict fields contain only changed readable values', () => {
  const local = kr('我的 KR 文案')
  const remote = kr('服务器 KR 文案')
  remote.points[0].title = '服务器活动 Agent'
  assert.deepEqual(buildKrConflictFields(local, remote).map((field) => field.label), ['KR 文案', '具体条目「活动 Agent」/ 文案'])
})

test('conflict location names O, KR and concrete point', () => {
  const objectives: Objective[] = [{ id: 'o-1', title: 'AI提效', krs: [kr('产品提效')] }]
  assert.equal(krConflictLocation(objectives, 'kr-1', 'point-1'), 'O1「AI提效」 / KR1「产品提效」 / 具体条目「活动 Agent」')
})

test('conflict copy contains the location and local values only', () => {
  const fields = buildKrConflictFields(kr('我的 KR 文案'), kr('服务器 KR 文案'))
  const content = localConflictCopyText('O1「AI提效」 / KR1「产品提效」', fields)
  assert.match(content, /冲突位置：O1「AI提效」/)
  assert.match(content, /我的 KR 文案/)
  assert.doesNotMatch(content, /服务器 KR 文案/)
})
