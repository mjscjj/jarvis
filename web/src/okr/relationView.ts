import type { EntityRelation } from '../types'
import type { Objective } from './emily/types'

export const okrRelationTypes = ['okr_objective', 'okr_kr', 'okr_point'] as const

const inverseRelations: Record<string, string> = {
  belongs_to: 'contains',
  contains: 'belongs_to',
  owned_by: 'owns',
  owns: 'owned_by',
  participates_in: 'has_participant',
  has_participant: 'participates_in',
  depends_on: 'required_by',
  required_by: 'depends_on',
  advances: 'advanced_by',
  advanced_by: 'advances',
  maps_to: 'mapped_from',
  mapped_from: 'maps_to',
  derived_from: 'produces',
  produces: 'derived_from',
}

export interface OKRRelationRow extends EntityRelation {
  okr_ref: string
  display_relation: string
  world_ref: string
  direction: 'outgoing' | 'incoming'
}

export interface OKRCoverageCount {
  total: number
  related: number
  unrelated: number
}

export interface OKRObjectiveCoverage {
  id: string
  title: string
  krs: number
  points: number
  relatedNodes: number
  unrelatedNodes: number
}

export interface OKRProjectionCoverage {
  objectives: OKRCoverageCount
  krs: OKRCoverageCount
  points: OKRCoverageCount
  owners: OKRCoverageCount
  byObjective: OKRObjectiveCoverage[]
}

function ref(type: string, id: string): string {
  return `${type}:${id}`
}

export function okrRefs(objectives: Objective[]): Set<string> {
  const refs = new Set<string>()
  for (const objective of objectives) {
    refs.add(ref('okr_objective', objective.id))
    for (const kr of objective.krs) {
      refs.add(ref('okr_kr', kr.id))
      for (const point of kr.points) refs.add(ref('okr_point', point.id))
    }
  }
  return refs
}

export function relationsForOKRBoard(relations: EntityRelation[], objectives: Objective[]): OKRRelationRow[] {
  const currentRefs = okrRefs(objectives)
  const seen = new Set<number>()
  const rows: OKRRelationRow[] = []
  for (const relation of relations) {
    if (seen.has(relation.id)) continue
    const sourceRef = ref(relation.source_type, relation.source_id)
    const targetRef = ref(relation.target_type, relation.target_id)
    if (currentRefs.has(sourceRef)) {
      seen.add(relation.id)
      rows.push({
        ...relation,
        okr_ref: sourceRef,
        display_relation: relation.relation_type,
        world_ref: targetRef,
        direction: 'outgoing',
      })
      continue
    }
    if (currentRefs.has(targetRef)) {
      seen.add(relation.id)
      rows.push({
        ...relation,
        okr_ref: targetRef,
        display_relation: inverseRelations[relation.relation_type] ?? `← ${relation.relation_type}`,
        world_ref: sourceRef,
        direction: 'incoming',
      })
    }
  }
  return rows
}

export function projectionCoverageForOKRBoard(relations: EntityRelation[], objectives: Objective[]): OKRProjectionCoverage {
  const rows = relationsForOKRBoard(relations, objectives)
  const related = new Set(rows.filter((row) => {
    if (row.direction !== 'outgoing' || !row.confirmed_at) return false
    const [type] = row.okr_ref.split(':', 1)
    const [worldType] = row.world_ref.split(':', 1)
    if (type === 'okr_objective') return row.relation_type === 'maps_to' && worldType === 'project'
    if (type === 'okr_kr') return row.relation_type === 'maps_to' && worldType === 'key_matter'
    if (type === 'okr_point') return (row.relation_type === 'maps_to' || row.relation_type === 'advances') && worldType === 'key_matter'
    return false
  }).map((row) => row.okr_ref))
  const count = (refs: string[]): OKRCoverageCount => {
    const relatedCount = refs.filter((value) => related.has(value)).length
    return { total: refs.length, related: relatedCount, unrelated: refs.length - relatedCount }
  }
  const objectiveRefs = objectives.map((objective) => ref('okr_objective', objective.id))
  const krRefs = objectives.flatMap((objective) => objective.krs.map((kr) => ref('okr_kr', kr.id)))
  const pointRefs = objectives.flatMap((objective) => objective.krs.flatMap((kr) => kr.points.map((point) => ref('okr_point', point.id))))
  const ownerOccurrences = objectives.flatMap((objective) => objective.krs.flatMap((kr) => [
    ...(kr.owners ?? []).map((owner) => ({ nodeRef: ref('okr_kr', kr.id), openId: owner.openId })),
    ...kr.points.flatMap((point) => (point.owners ?? []).map((owner) => ({ nodeRef: ref('okr_point', point.id), openId: owner.openId }))),
  ])).filter((owner) => owner.openId)
  const relatedOwners = ownerOccurrences.filter((owner) => rows.some((row) =>
    row.direction === 'outgoing' && Boolean(row.confirmed_at) && row.okr_ref === owner.nodeRef &&
    row.relation_type === 'owned_by' && (row.target_type === 'person' || row.target_type === 'principal') &&
    row.evidence?.owner_open_id === owner.openId,
  )).length
  return {
    objectives: count(objectiveRefs),
    krs: count(krRefs),
    points: count(pointRefs),
    owners: { total: ownerOccurrences.length, related: relatedOwners, unrelated: ownerOccurrences.length - relatedOwners },
    byObjective: objectives.map((objective) => {
      const refs = [
        ref('okr_objective', objective.id),
        ...objective.krs.map((kr) => ref('okr_kr', kr.id)),
        ...objective.krs.flatMap((kr) => kr.points.map((point) => ref('okr_point', point.id))),
      ]
      const relatedNodes = refs.filter((value) => related.has(value)).length
      return {
        id: objective.id,
        title: objective.title,
        krs: objective.krs.length,
        points: objective.krs.reduce((sum, kr) => sum + kr.points.length, 0),
        relatedNodes,
        unrelatedNodes: refs.length - relatedNodes,
      }
    }),
  }
}
