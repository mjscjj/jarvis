import type { EntityRelation, PageIndexItem, PageType, PageView } from '../types'
import type { Objective } from '../okr/emily/types'
import { isPageType, nodeKey, nodeSize, readableSummary, type WorldGraph, type WorldLink, type WorldNode } from './graphData.ts'

export interface OKRGraphInput {
  objectives: Objective[]
  relations: EntityRelation[]
  activePages: PageView[]
  fullIndex: PageIndexItem[]
  objectiveId?: string
}

const okrTypes = new Set(['okr_objective', 'okr_kr', 'okr_point'])

function isCanonicalOKRWorldRelation(relation: EntityRelation): boolean {
  if (!relation.confirmed_at) return false
  if (relation.source_type === 'okr_objective') {
    return relation.relation_type === 'maps_to' && relation.target_type === 'project'
  }
  if (relation.source_type === 'okr_kr') {
    return relation.relation_type === 'maps_to' && relation.target_type === 'key_matter' ||
      relation.relation_type === 'owned_by' && (relation.target_type === 'person' || relation.target_type === 'principal')
  }
  if (relation.source_type === 'okr_point') {
    return (relation.relation_type === 'maps_to' || relation.relation_type === 'advances') && relation.target_type === 'key_matter' ||
      relation.relation_type === 'owned_by' && (relation.target_type === 'person' || relation.target_type === 'principal')
  }
  return false
}

export function okrWorldPageRefs(objectives: Objective[], relations: EntityRelation[]): Array<{ type: PageType; id: number }> {
  const currentRefs = new Set<string>()
  for (const objective of objectives) {
    currentRefs.add(nodeKey('okr_objective', objective.id))
    for (const kr of objective.krs) {
      currentRefs.add(nodeKey('okr_kr', kr.id))
      for (const point of kr.points) currentRefs.add(nodeKey('okr_point', point.id))
    }
  }
  const refs = new Map<string, { type: PageType; id: number }>()
  for (const relation of relations) {
    if (!isCanonicalOKRWorldRelation(relation) || !currentRefs.has(nodeKey(relation.source_type, relation.source_id)) || !isPageType(relation.target_type)) continue
    const id = Number(relation.target_id)
    if (!Number.isSafeInteger(id) || id <= 0) continue
    refs.set(nodeKey(relation.target_type, id), { type: relation.target_type, id })
  }
  return [...refs.values()]
}

function relationLabel(value: string): string {
  return ({
    contains: '包含',
    belongs_to: '属于',
    owned_by: '负责人',
    owns: '负责',
    participates_in: '参与',
    has_participant: '参与者',
    depends_on: '依赖',
    required_by: '被依赖',
    advances: '推进',
    advanced_by: '由其推进',
    maps_to: '映射到',
    mapped_from: '映射自',
    derived_from: '来源于',
    produces: '产出',
  } as Record<string, string>)[value] ?? value
}

function okrNode(type: 'okr_objective' | 'okr_kr' | 'okr_point', id: string, name: string, objectiveId: string): WorldNode {
  return {
    id: nodeKey(type, id),
    nodeType: type,
    entityId: id,
    objectiveId,
    name,
    indexLine: type === 'okr_objective' ? '季度 Objective' : type === 'okr_kr' ? '关键结果' : '子 KR',
    summary: null,
    charCount: name.length,
    factCount: null,
    updatedAt: null,
    active: true,
    size: type === 'okr_objective' ? 7.2 : type === 'okr_kr' ? 5.8 : 4.7,
  }
}

function pageNode(type: PageType, pageId: number, item: PageIndexItem | undefined, activePages: Map<string, PageView>): WorldNode {
  const id = nodeKey(type, pageId)
  const page = activePages.get(id)
  const summary = readableSummary(page?.summary)
  return {
    id,
    nodeType: type,
    pageType: type,
    pageId,
    entityId: String(pageId),
    name: page?.name || item?.name || id,
    indexLine: item?.index_line || summary.split('\n').find(Boolean) || '',
    summary: summary || null,
    charCount: page?.char_count ?? item?.char_count ?? 0,
    factCount: page?.fact_count ?? null,
    updatedAt: page?.updated_at ?? item?.last_progress_at ?? null,
    active: Boolean(page),
    size: page ? nodeSize(page) : 4.5,
  }
}

function externalNode(type: string, id: string): WorldNode {
  return {
    id: nodeKey(type, id),
    nodeType: 'external',
    entityId: id,
    name: `${type}:${id}`,
    indexLine: '关系指向的外部对象',
    summary: null,
    charCount: 0,
    factCount: null,
    updatedAt: null,
    active: true,
    size: 4.5,
  }
}

function addLink(links: WorldLink[], seen: Set<string>, link: WorldLink) {
  if (seen.has(link.id)) return
  seen.add(link.id)
  links.push(link)
}

export function buildOKRGraph(input: OKRGraphInput): WorldGraph {
  const selectedObjectives = input.objectiveId
    ? input.objectives.filter((objective) => objective.id === input.objectiveId)
    : input.objectives
  const nodes = new Map<string, WorldNode>()
  const links: WorldLink[] = []
  const seenLinks = new Set<string>()
  const indexes = new Map(input.fullIndex.map((item) => [nodeKey(item.type, item.id), item]))
  const activePages = new Map(input.activePages.map((page) => [nodeKey(page.type, page.id), page]))

  for (const objective of selectedObjectives) {
    const objectiveNode = okrNode('okr_objective', objective.id, objective.title, objective.id)
    nodes.set(objectiveNode.id, objectiveNode)
    for (const kr of objective.krs) {
      const krNode = okrNode('okr_kr', kr.id, kr.title, objective.id)
      nodes.set(krNode.id, krNode)
      addLink(links, seenLinks, {
        id: `contains:${objectiveNode.id}->${krNode.id}`,
        source: objectiveNode.id,
        target: krNode.id,
        relationType: 'contains',
        label: '包含',
        strength: 'structural',
      })
      for (const point of kr.points) {
        const pointNode = okrNode('okr_point', point.id, point.title, objective.id)
        nodes.set(pointNode.id, pointNode)
        addLink(links, seenLinks, {
          id: `contains:${krNode.id}->${pointNode.id}`,
          source: krNode.id,
          target: pointNode.id,
          relationType: 'contains',
          label: '包含',
          strength: 'structural',
        })
      }
    }
  }

  const currentOKRRefs = new Set(nodes.keys())
  const seenRelations = new Set<number>()
  for (const relation of input.relations) {
    if (seenRelations.has(relation.id) || !isCanonicalOKRWorldRelation(relation)) continue
    const source = nodeKey(relation.source_type, relation.source_id)
    const target = nodeKey(relation.target_type, relation.target_id)
    const sourceIsCurrent = currentOKRRefs.has(source)
    if (!sourceIsCurrent) continue
    if (okrTypes.has(relation.target_type)) continue
    seenRelations.add(relation.id)
    for (const [type, id, key] of [[relation.source_type, relation.source_id, source], [relation.target_type, relation.target_id, target]] as const) {
      if (nodes.has(key)) continue
      const item = isPageType(type) ? indexes.get(key) : undefined
      const pageId = Number(id)
      nodes.set(key, isPageType(type) && Number.isSafeInteger(pageId) && (item || activePages.has(key)) ? pageNode(type, pageId, item, activePages) : externalNode(type, id))
    }
    addLink(links, seenLinks, {
      id: `relation:${relation.id}`,
      source,
      target,
      relationType: relation.relation_type,
      label: relationLabel(relation.relation_type),
      strength: relation.relation_type === 'owned_by' ? 'owner' : 'strong',
    })
  }

  for (const page of input.activePages) {
    const source = nodeKey(page.type, page.id)
    if (!nodes.has(source)) continue
    for (const reference of page.outgoing ?? []) {
      const target = nodeKey(reference.type, reference.id)
      if (!nodes.has(target)) continue
      addLink(links, seenLinks, {
        id: `reference:${source}->${target}`,
        source,
        target,
        relationType: 'reference',
        label: '引用',
        strength: 'reference',
      })
    }
  }

  return { nodes: [...nodes.values()], links }
}
