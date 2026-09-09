import assert from 'node:assert/strict'
import test from 'node:test'
import type { EntityRelation, PageIndexItem } from '../src/types.ts'
import type { Objective } from '../src/okr/emily/types.ts'
import { relationsForOKRBoard } from '../src/okr/relationView.ts'

const objectives: Objective[] = [{
  id: 'o-current',
  title: 'Current objective',
  krs: [{
    id: 'kr-current', title: 'Current KR', metricNote: '', metrics: [],
    points: [{ id: 'point-current', kind: 'strategy', title: 'Current point', entries: [] }],
  }],
}]

const worldPages: PageIndexItem[] = [
  { type: 'key_matter', id: 7, name: '完成灰度发布', index_line: '', char_count: 0, last_progress_at: null },
  { type: 'project', id: 9, name: '交付平台', index_line: '', char_count: 0, last_progress_at: null },
]

function relation(id: number, overrides: Partial<EntityRelation>): EntityRelation {
  return {
    id,
    source_type: 'okr_point',
    source_id: 'point-current',
    relation_type: 'maps_to',
    target_type: 'key_matter',
    target_id: '7',
    evidence: {},
    confidence: 1,
    confirmed_at: '2026-09-07T00:00:00Z',
    created_at: '',
    updated_at: '',
    ...overrides,
  }
}

test('normalizes both relation directions around the current OKR board', () => {
  const outgoing = relation(1, {})
  const incoming = relation(2, {
    source_type: 'project', source_id: '9', relation_type: 'advances',
    target_type: 'okr_kr', target_id: 'kr-current',
  })
  const rows = relationsForOKRBoard([outgoing, incoming], objectives, worldPages)
  assert.deepEqual(rows.map((item) => [item.okr_level, item.okr_title, item.display_relation, item.world_type_label, item.world_name]), [
    ['子 KR', 'Current point', '对应', '关键事项', '完成灰度发布'],
    ['KR', 'Current KR', '由其推进', '项目', '交付平台'],
  ])
})

test('deduplicates combined queries and filters relations from other quarters', () => {
  const current = relation(1, {})
  const otherQuarter = relation(2, { source_id: 'point-other' })
  assert.deepEqual(relationsForOKRBoard([current, current, otherQuarter], objectives).map((item) => item.id), [1])
})

test('keeps unknown relation data but presents it without technical tokens', () => {
  const rows = relationsForOKRBoard([relation(3, {
    source_type: 'resource', source_id: '4', relation_type: 'custom_link',
    target_type: 'okr_objective', target_id: 'o-current',
  })], objectives, [{ type: 'resource', id: 4, name: '上线说明', index_line: '', char_count: 0, last_progress_at: null }])
  assert.equal(rows[0].display_relation, '有关联')
  assert.equal(rows[0].world_type_label, '资料')
  assert.equal(rows[0].world_name, '上线说明')
  assert.equal(rows[0].direction, 'incoming')
})

test('uses a readable placeholder when a related object can no longer be resolved', () => {
  const rows = relationsForOKRBoard([relation(4, {})], objectives)
  assert.equal(rows[0].world_name, '名称暂不可用')
  assert.equal(rows[0].world_type_label, '关键事项')
})
