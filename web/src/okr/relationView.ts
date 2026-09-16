import type { EntityRelation, PageIndexItem } from '../types'
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
  okr_level: 'O' | 'KR' | '子 KR'
  okr_title: string
  okr_context: string
  display_relation: string
  world_type_label: string
  world_name: string
  direction: 'outgoing' | 'incoming'
}

interface OKRNode {
  level: OKRRelationRow['okr_level']
  title: string
  context: string
}

const relationLabels: Record<string, string> = {
  belongs_to: '属于',
  contains: '包含',
  owned_by: '负责人是',
  owns: '负责',
  participates_in: '参与',
  has_participant: '参与者包括',
  depends_on: '依赖',
  required_by: '被其依赖',
  advances: '推进',
  advanced_by: '由其推进',
  maps_to: '对应',
  mapped_from: '对应',
  derived_from: '来源于',
  produces: '产出',
}

const worldTypeLabels: Record<string, string> = {
  principal: '本人',
  person: '协作人',
  project: '项目',
  key_matter: '关键事项',
  project_risk: '项目风险',
  project_change: '项目变更',
  group: '群聊',
  resource: '资料',
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

function indexOKRNodes(objectives: Objective[]): Map<string, OKRNode> {
  const nodes = new Map<string, OKRNode>()
  for (const objective of objectives) {
    nodes.set(ref('okr_objective', objective.id), { level: 'O', title: objective.title, context: '' })
    for (const kr of objective.krs) {
      nodes.set(ref('okr_kr', kr.id), { level: 'KR', title: kr.title, context: `目标：${objective.title}` })
      for (const point of kr.points) {
        nodes.set(ref('okr_point', point.id), {
          level: '子 KR',
          title: point.title,
          context: `目标：${objective.title} · KR：${kr.title}`,
        })
      }
    }
  }
  return nodes
}

export function relationsForOKRBoard(
  relations: EntityRelation[],
  objectives: Objective[],
  worldPages: readonly PageIndexItem[] = [],
): OKRRelationRow[] {
  const currentRefs = okrRefs(objectives)
  const okrNodes = indexOKRNodes(objectives)
  const worldNames = new Map(worldPages.map((item) => [ref(item.type, String(item.id)), item.name]))
  const seen = new Set<number>()
  const rows: OKRRelationRow[] = []
  for (const relation of relations) {
    if (seen.has(relation.id)) continue
    const sourceRef = ref(relation.source_type, relation.source_id)
    const targetRef = ref(relation.target_type, relation.target_id)
    if (currentRefs.has(sourceRef)) {
      const okr = okrNodes.get(sourceRef)
      if (!okr) continue
      seen.add(relation.id)
      rows.push({
        ...relation,
        okr_level: okr.level,
        okr_title: okr.title,
        okr_context: okr.context,
        display_relation: relationLabels[relation.relation_type] ?? '有关联',
        world_type_label: worldTypeLabels[relation.target_type] ?? '其他对象',
        world_name: worldNames.get(targetRef) ?? '名称暂不可用',
        direction: 'outgoing',
      })
      continue
    }
    if (currentRefs.has(targetRef)) {
      const okr = okrNodes.get(targetRef)
      if (!okr) continue
      const normalizedRelation = inverseRelations[relation.relation_type]
      seen.add(relation.id)
      rows.push({
        ...relation,
        okr_level: okr.level,
        okr_title: okr.title,
        okr_context: okr.context,
        display_relation: normalizedRelation ? relationLabels[normalizedRelation] ?? '有关联' : '有关联',
        world_type_label: worldTypeLabels[relation.source_type] ?? '其他对象',
        world_name: worldNames.get(sourceRef) ?? '名称暂不可用',
        direction: 'incoming',
      })
    }
  }
  return rows
}
