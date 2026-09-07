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
