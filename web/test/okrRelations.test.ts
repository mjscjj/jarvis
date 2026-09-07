import assert from 'node:assert/strict'
import test from 'node:test'
import type { EntityRelation } from '../src/types.ts'
import type { Objective } from '../src/okr/emily/types.ts'
import { projectionCoverageForOKRBoard, relationsForOKRBoard } from '../src/okr/relationView.ts'

const objectives: Objective[] = [{
  id: 'o-current',
  title: 'Current objective',
  krs: [{
    id: 'kr-current', title: 'Current KR', metricNote: '', metrics: [],
    points: [{ id: 'point-current', kind: 'strategy', title: 'Current point', entries: [] }],
  }],
}]

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
  const rows = relationsForOKRBoard([outgoing, incoming], objectives)
  assert.deepEqual(rows.map((item) => [item.okr_ref, item.display_relation, item.world_ref]), [
    ['okr_point:point-current', 'maps_to', 'key_matter:7'],
    ['okr_kr:kr-current', 'advanced_by', 'project:9'],
  ])
})

test('deduplicates combined queries and filters relations from other quarters', () => {
  const current = relation(1, {})
  const otherQuarter = relation(2, { source_id: 'point-other' })
  assert.deepEqual(relationsForOKRBoard([current, current, otherQuarter], objectives).map((item) => item.id), [1])
})

test('keeps an unknown incoming relation token and makes its direction visible', () => {
  const rows = relationsForOKRBoard([relation(3, {
    source_type: 'resource', source_id: '4', relation_type: 'custom_link',
    target_type: 'okr_objective', target_id: 'o-current',
  })], objectives)
  assert.equal(rows[0].display_relation, '← custom_link')
  assert.equal(rows[0].direction, 'incoming')
})

test('counts only canonical Ontology projections as complete coverage', () => {
  const coverage = projectionCoverageForOKRBoard([
    relation(1, {}),
    relation(2, {
      source_type: 'project', source_id: '9', relation_type: 'advances',
      target_type: 'okr_kr', target_id: 'kr-current',
    }),
  ], objectives)
  assert.deepEqual(coverage.objectives, { total: 1, related: 0, unrelated: 1 })
  assert.deepEqual(coverage.krs, { total: 1, related: 0, unrelated: 1 })
  assert.deepEqual(coverage.points, { total: 1, related: 1, unrelated: 0 })
  assert.deepEqual(coverage.owners, { total: 0, related: 0, unrelated: 0 })
  assert.deepEqual(coverage.byObjective, [{
    id: 'o-current', title: 'Current objective', krs: 1, points: 1, relatedNodes: 1, unrelatedNodes: 2,
  }])
})

test('counts every structured Owner occurrence only when owned_by is persisted and confirmed', () => {
  const withOwner: Objective[] = [{
    ...objectives[0],
    krs: [{
      ...objectives[0].krs[0],
      owners: [{ name: '负责人', openId: 'ou_owner' }],
      points: [{ ...objectives[0].krs[0].points[0], owners: [{ name: '负责人', openId: 'ou_owner' }] }],
    }],
  }]
  const coverage = projectionCoverageForOKRBoard([
    relation(1, { source_type: 'okr_kr', source_id: 'kr-current', relation_type: 'owned_by', target_type: 'person', target_id: '3', evidence: { owner_open_id: 'ou_owner' } }),
    relation(2, { source_type: 'okr_point', source_id: 'point-current', relation_type: 'owned_by', target_type: 'person', target_id: '3', evidence: { owner_open_id: 'ou_owner' }, confirmed_at: null }),
  ], withOwner)
  assert.deepEqual(coverage.owners, { total: 2, related: 1, unrelated: 1 })
})

test('requires Objective to Project and KR to KeyMatter mappings', () => {
  const coverage = projectionCoverageForOKRBoard([
    relation(1, { source_type: 'okr_objective', source_id: 'o-current', target_type: 'project', target_id: '9' }),
    relation(2, { source_type: 'okr_kr', source_id: 'kr-current', target_type: 'key_matter', target_id: '8' }),
    relation(3, {}),
  ], objectives)
  assert.deepEqual(coverage.objectives, { total: 1, related: 1, unrelated: 0 })
  assert.deepEqual(coverage.krs, { total: 1, related: 1, unrelated: 0 })
  assert.deepEqual(coverage.points, { total: 1, related: 1, unrelated: 0 })
})
