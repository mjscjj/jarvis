import type { EntityRelation } from '../types'
import type { Objective } from '../okr/emily/types'

export type WorldOKREntityType = 'principal' | 'person' | 'project' | 'key_matter' | 'project_risk' | 'project_change' | 'group' | 'resource'

export interface RelatedOKRRow {
  key: string
  level: 'O' | 'KR' | '子 KR'
  type: 'okr_objective' | 'okr_kr' | 'okr_point'
  id: string
  title: string
  context: string
  relation: string
  basis?: string
  refs: string[]
  confirmed: boolean
}

interface OKRNode {
  level: RelatedOKRRow['level']
  type: RelatedOKRRow['type']
  id: string
  title: string
  context: string
}

const okrTypes = new Set<RelatedOKRRow['type']>(['okr_objective', 'okr_kr', 'okr_point'])

const directLabels: Record<string, string> = {
  belongs_to: '属于',
  contains: '包含',
  owned_by: '由其负责',
  owns: '负责',
  participates_in: '参与',
  has_participant: '有参与者',
  depends_on: '依赖',
  required_by: '被依赖',
  advances: '推进',
  advanced_by: '由其推进',
  maps_to: '映射到',
  mapped_from: '承接',
  derived_from: '源自',
  produces: '产出',
}

const inverseLabels: Record<string, string> = {
  belongs_to: '包含',
  contains: '属于',
  owned_by: '负责',
  owns: '由其负责',
  participates_in: '有其参与',
  has_participant: '参与',
  depends_on: '被依赖',
  required_by: '依赖',
  advances: '由其推进',
  advanced_by: '推进',
  maps_to: '承接',
  mapped_from: '映射到',
  derived_from: '产出',
  produces: '源自',
}

function nodeKey(type: string, id: string): string {
  return `${type}:${id}`
}

function indexOKRNodes(objectives: Objective[]): Map<string, OKRNode> {
  const nodes = new Map<string, OKRNode>()
  for (const objective of objectives) {
    nodes.set(nodeKey('okr_objective', objective.id), {
      level: 'O', type: 'okr_objective', id: objective.id, title: objective.title, context: '',
    })
    for (const kr of objective.krs) {
      nodes.set(nodeKey('okr_kr', kr.id), {
        level: 'KR', type: 'okr_kr', id: kr.id, title: kr.title, context: objective.title,
      })
      for (const point of kr.points) {
        nodes.set(nodeKey('okr_point', point.id), {
          level: '子 KR', type: 'okr_point', id: point.id, title: point.title,
          context: `${objective.title} / ${kr.title}`,
        })
      }
    }
  }
  return nodes
}

function relationEvidence(relation: EntityRelation): { basis?: string; refs: string[] } {
  const basis = typeof relation.evidence?.basis === 'string' ? relation.evidence.basis : undefined
  const refs = Array.isArray(relation.evidence?.refs)
    ? relation.evidence.refs.filter((value): value is string => typeof value === 'string')
    : []
  return { basis, refs }
}

export function relatedOKRsForWorldEntity(
  relations: EntityRelation[],
  objectives: Objective[],
  entity: { type: WorldOKREntityType; id: number | string },
): RelatedOKRRow[] {
  const nodes = indexOKRNodes(objectives)
  const entityId = String(entity.id)
  const rows = new Map<string, RelatedOKRRow>()

  for (const item of relations) {
    const entityIsSource = item.source_type === entity.type && item.source_id === entityId
    const entityIsTarget = item.target_type === entity.type && item.target_id === entityId
    if (!entityIsSource && !entityIsTarget) continue
    const otherType = entityIsSource ? item.target_type : item.source_type
    const otherId = entityIsSource ? item.target_id : item.source_id
    if (!okrTypes.has(otherType as RelatedOKRRow['type'])) continue
    const node = nodes.get(nodeKey(otherType, otherId))
    if (!node) continue
    const key = nodeKey(node.type, node.id)
    const evidence = relationEvidence(item)
    const relation = entityIsSource ? (directLabels[item.relation_type] ?? item.relation_type) : (inverseLabels[item.relation_type] ?? `被 ${item.relation_type}`)
    const current = rows.get(key)
    if (!current) {
      rows.set(key, {
        key, level: node.level, type: node.type, id: node.id, title: node.title, context: node.context,
        relation, basis: evidence.basis, refs: evidence.refs, confirmed: Boolean(item.confirmed_at),
      })
      continue
    }
    current.relation = [...new Set([...current.relation.split(' · '), relation])].join(' · ')
    current.refs = [...new Set([...current.refs, ...evidence.refs])]
    if (evidence.basis && evidence.basis !== current.basis) current.basis = [current.basis, evidence.basis].filter(Boolean).join('；')
    current.confirmed = current.confirmed && Boolean(item.confirmed_at)
  }

  const rank = { O: 0, KR: 1, '子 KR': 2 } as const
  return [...rows.values()].sort((left, right) => rank[left.level] - rank[right.level] || left.context.localeCompare(right.context, 'zh-CN') || left.title.localeCompare(right.title, 'zh-CN'))
}
