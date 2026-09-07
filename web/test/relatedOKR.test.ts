import assert from 'node:assert/strict'
import test from 'node:test'
import type { EntityRelation } from '../src/types.ts'
import type { Objective } from '../src/okr/emily/types.ts'
import { relatedOKRsForWorldEntity } from '../src/world/relatedOKR.ts'

const objectives: Objective[] = [{
  id: 'o-1', title: '提升交付', krs: [{
    id: 'kr-1', title: '完成项目上线', metricNote: '', metrics: [], entries: [],
    owners: [{ openId: 'ou_owner', name: '负责人' }],
    points: [{ id: 'point-1', kind: 'strategy', title: '完成灰度', entries: [], owners: [{ openId: 'ou_point', name: '执行人' }] }],
  }],
}]

function relation(overrides: Partial<EntityRelation>): EntityRelation {
  return {
    id: 1, source_type: 'okr_kr', source_id: 'kr-1', relation_type: 'maps_to',
    target_type: 'project', target_id: '7', evidence: { basis: '项目直接承接该 KR', refs: ['okr_kr:kr-1', 'project:7'] },
    confidence: 1, confirmed_at: '2026-09-07T00:00:00Z', created_at: '', updated_at: '', ...overrides,
  }
}

test('shows an incoming OKR mapping from a world entity perspective', () => {
  const rows = relatedOKRsForWorldEntity([relation({})], objectives, { type: 'project', id: 7 })
  assert.deepEqual(rows.map((row) => [row.level, row.title, row.relation, row.basis]), [
    ['KR', '完成项目上线', '承接', '项目直接承接该 KR'],
  ])
})

test('combines multiple confirmed world edges to the same OKR node', () => {
  const rows = relatedOKRsForWorldEntity([
    relation({}),
    relation({ id: 2, source_type: 'project', source_id: '7', relation_type: 'advances', target_type: 'okr_kr', target_id: 'kr-1' }),
  ], objectives, { type: 'project', id: 7 })
  assert.equal(rows.length, 1)
  assert.equal(rows[0].relation, '承接 · 推进')
})

test('shows person ownership only through a persisted strong relation', () => {
  const rows = relatedOKRsForWorldEntity([relation({
    relation_type: 'owned_by', target_type: 'person', target_id: '3',
    evidence: { owner_open_id: 'ou_owner', refs: ['okr_kr:kr-1', 'person:3'] },
  })], objectives, { type: 'person', id: 3 })
  assert.deepEqual(rows.map((row) => [row.level, row.title, row.relation]), [
    ['KR', '完成项目上线', '负责'],
  ])
})

test('does not synthesize ownership when the strong relation is missing', () => {
  assert.deepEqual(relatedOKRsForWorldEntity([], objectives, { type: 'person', id: 3 }), [])
})

test('ignores relations to OKR nodes outside the loaded quarter', () => {
  const rows = relatedOKRsForWorldEntity([relation({ source_id: 'kr-other' })], objectives, { type: 'project', id: 7 })
  assert.deepEqual(rows, [])
})
