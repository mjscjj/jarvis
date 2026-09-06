import type { PageIndexItem, PageLink, PageType, PageView } from '../types'
import type { LayoutMode, SpacingMode } from './settings'

export const pageTypes: PageType[] = ['principal', 'project', 'key_matter', 'person', 'group', 'resource']

export const pageTypeMeta: Record<PageType, { label: string; color: string; dimColor: string }> = {
  principal: { label: '我', color: '#f4d06f', dimColor: '#645d42' },
  project: { label: '项目', color: '#6ee7b7', dimColor: '#315f52' },
  key_matter: { label: '关键事项', color: '#fb7185', dimColor: '#6b3641' },
  person: { label: '人物', color: '#70b7ff', dimColor: '#315777' },
  group: { label: '会话', color: '#b78cff', dimColor: '#503e70' },
  resource: { label: '资源', color: '#67e8f9', dimColor: '#2c6269' },
}

export interface WorldNode {
  id: string
  pageType: PageType
  pageId: number
  name: string
  indexLine: string
  summary: string | null
  charCount: number
  factCount: number | null
  updatedAt: string | null
  active: boolean
  size: number
  x?: number
  y?: number
  z?: number
  fx?: number
  fy?: number
  fz?: number
}

export interface WorldLink {
  id: string
  source: string | WorldNode
  target: string | WorldNode
  label: '引用'
}

export interface WorldGraph {
  nodes: WorldNode[]
  links: WorldLink[]
}

export function nodeKey(type: string, id: number): string {
  return `${type}:${id}`
}

export function isPageType(value: string): value is PageType {
  return pageTypes.includes(value as PageType)
}

export function linkEndpointId(endpoint: string | WorldNode): string {
  return typeof endpoint === 'string' ? endpoint : endpoint.id
}

export function readableSummary(value: string | null | undefined): string {
  if (!value) return ''
  const trimmed = value.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return trimmed
  try {
    const parsed = JSON.parse(trimmed) as unknown
    if (typeof parsed === 'string') return readableSummary(parsed)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const record = parsed as Record<string, unknown>
      for (const key of ['summary', 'content', 'text', 'description']) {
        if (typeof record[key] === 'string') return readableSummary(record[key])
      }
    }
  } catch {
    // The page summary is usually Markdown; malformed JSON-like text is still valid content.
  }
  return trimmed
}

function indexMap(index: PageIndexItem[]): Map<string, PageIndexItem> {
  return new Map(index.map((item) => [nodeKey(item.type, item.id), item]))
}

function nodeFromPage(page: PageView, activeIds: Set<string>, indexes: Map<string, PageIndexItem>): WorldNode {
  const id = nodeKey(page.type, page.id)
  const summary = readableSummary(page.summary)
  return {
    id,
    pageType: page.type,
    pageId: page.id,
    name: page.name,
    indexLine: indexes.get(id)?.index_line || summary.split('\n').find(Boolean) || '',
    summary: summary || null,
    charCount: page.char_count,
    factCount: page.fact_count,
    updatedAt: page.updated_at,
    active: activeIds.has(id),
    size: nodeSize(page),
  }
}

function nodeFromLink(link: PageLink, activeIds: Set<string>, indexes: Map<string, PageIndexItem>): WorldNode | null {
  if (!isPageType(link.type)) return null
  const id = nodeKey(link.type, link.id)
  const item = indexes.get(id)
  return {
    id,
    pageType: link.type,
    pageId: link.id,
    name: link.name || item?.name || id,
    indexLine: item?.index_line || '',
    summary: null,
    charCount: item?.char_count || 0,
    factCount: null,
    updatedAt: item?.last_progress_at || null,
    active: activeIds.has(id),
    size: 4.5,
  }
}

export function nodeSize(page: PageView): number {
  const semanticWeight = Math.log2(Math.max(1, page.fact_count) + 1) * .34 + Math.log2(Math.max(1, page.char_count) + 1) * .08
  const boost = page.type === 'principal' ? 1.25 : page.type === 'project' ? .7 : page.type === 'key_matter' ? .4 : 0
  return Math.min(7.6, Math.max(4.5, 3.4 + semanticWeight + boost))
}

export function buildActiveGraph(pages: PageView[], activeIndex: PageIndexItem[]): WorldGraph {
  const indexes = indexMap(activeIndex)
  const activeIds = new Set(indexes.keys())
  const nodes = pages.map((page) => nodeFromPage(page, activeIds, indexes))
  const nodeIds = new Set(nodes.map((node) => node.id))
  const links: WorldLink[] = []
  const seen = new Set<string>()

  for (const page of pages) {
    const source = nodeKey(page.type, page.id)
    for (const outgoing of page.outgoing || []) {
      const target = nodeKey(outgoing.type, outgoing.id)
      const id = `${source}->${target}`
      if (!nodeIds.has(target) || seen.has(id)) continue
      seen.add(id)
      links.push({ id, source, target, label: '引用' })
    }
  }
  return { nodes, links }
}

export function buildFocusGraph(
  page: PageView,
  activeIndex: PageIndexItem[],
  fullIndex: PageIndexItem[],
  layoutMode: LayoutMode = 'relation',
  spacingMode: SpacingMode = 'standard',
): WorldGraph {
  const indexes = indexMap(fullIndex)
  const activeIds = new Set(activeIndex.map((item) => nodeKey(item.type, item.id)))
  const center = { ...nodeFromPage(page, activeIds, indexes), x: 0, y: 0, z: 0, fx: 0, fy: 0, fz: 0 }
  const nodes = new Map<string, WorldNode>([[center.id, center]])
  const links: WorldLink[] = []
  const seen = new Set<string>()

  const add = (reference: PageLink, source: string, target: string) => {
    const id = `${source}->${target}`
    if (seen.has(id)) return
    const neighbor = nodeFromLink(reference, activeIds, indexes)
    if (!neighbor) return
    seen.add(id)
    nodes.set(neighbor.id, neighbor)
    links.push({ id, source, target, label: '引用' })
  }
  const outgoing = (page.outgoing || []).slice(0, 12)
  const backlinks = (page.backlinks || []).filter((item) => !outgoing.some((other) => nodeKey(other.type, other.id) === nodeKey(item.type, item.id))).slice(0, Math.max(0, 24 - outgoing.length))
  if (outgoing.length + backlinks.length < 24) {
    outgoing.push(...(page.outgoing || []).slice(outgoing.length, 24 - backlinks.length))
  }
  for (const reference of outgoing) add(reference, center.id, nodeKey(reference.type, reference.id))
  for (const reference of backlinks) add(reference, nodeKey(reference.type, reference.id), center.id)

  const positionSide = (references: PageLink[], side: -1 | 1) => {
    const spacing = spacingMode === 'compact' ? .78 : spacingMode === 'loose' ? 1.35 : 1
    const unique = references.filter((reference, index) => references.findIndex((candidate) => nodeKey(candidate.type, candidate.id) === nodeKey(reference.type, reference.id)) === index)
    unique.forEach((reference, index) => {
      const node = nodes.get(nodeKey(reference.type, reference.id))
      if (!node) return
      const column = Math.floor(index / 6)
      const row = index % 6
      const rowsInColumn = Math.min(6, unique.length - column * 6)
      node.x = side * (105 + column * 108) * spacing
      node.y = (row - (rowsInColumn - 1) / 2) * 50 * spacing
      node.z = layoutMode === 'flat2d' ? 0 : ((row % 3) - 1) * 10 * spacing
      if (layoutMode === 'relation') {
        node.fx = node.x
        node.fy = node.y
        node.fz = node.z
      } else if (layoutMode === 'flat2d') {
        node.fz = 0
      }
    })
  }
  positionSide(backlinks, -1)
  positionSide(outgoing, 1)
  return { nodes: [...nodes.values()], links }
}

export function filterGraph(graph: WorldGraph, visibleTypes: Set<PageType>, selectedId?: string): WorldGraph {
  const keep = new Set(graph.nodes.filter((node) => visibleTypes.has(node.pageType) || node.id === selectedId).map((node) => node.id))
  return {
    nodes: graph.nodes.filter((node) => keep.has(node.id)),
    links: graph.links.filter((link) => keep.has(linkEndpointId(link.source)) && keep.has(linkEndpointId(link.target))),
  }
}

export function connectedIds(graph: WorldGraph, selectedId: string | undefined): Set<string> {
  if (!selectedId) return new Set()
  const result = new Set([selectedId])
  for (const link of graph.links) {
    const source = linkEndpointId(link.source)
    const target = linkEndpointId(link.target)
    if (source === selectedId) result.add(target)
    if (target === selectedId) result.add(source)
  }
  return result
}

export function graphCounts(graph: WorldGraph): { nodes: number; links: number; facts: number } {
  return {
    nodes: graph.nodes.length,
    links: graph.links.length,
    facts: graph.nodes.reduce((sum, node) => sum + (node.factCount || 0), 0),
  }
}
