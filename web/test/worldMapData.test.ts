import assert from 'node:assert/strict'
import test from 'node:test'
import type { PageIndexItem, PageView } from '../src/types.ts'
import { buildActiveGraph, buildFocusGraph, filterGraph, filterGraphByDirection, graphCounts, graphForNodeIds, primaryComponentIds, readableSummary, withoutIsolatedNodes } from '../src/world-map/graphData.ts'

const index: PageIndexItem[] = [
  { type: 'principal', id: 1, name: '我', index_line: '主体', char_count: 100, last_progress_at: null },
  { type: 'project', id: 2, name: 'Jarvis', index_line: '项目', char_count: 120, last_progress_at: null },
  { type: 'person', id: 3, name: '协作者', index_line: '人物', char_count: 80, last_progress_at: null },
]

const pages: PageView[] = [
  { type: 'principal', id: 1, name: '我', summary: '主体', char_count: 100, max_chars: 1000, updated_at: '', fact_count: 4, outgoing: [{ type: 'project', id: 2, name: 'Jarvis' }], backlinks: [] },
  { type: 'project', id: 2, name: 'Jarvis', summary: '项目', char_count: 120, max_chars: 1000, updated_at: '', fact_count: 7, outgoing: [{ type: 'person', id: 3, name: '协作者' }], backlinks: [{ type: 'principal', id: 1, name: '我' }] },
  { type: 'person', id: 3, name: '协作者', summary: '人物', char_count: 80, max_chars: 1000, updated_at: '', fact_count: 2, outgoing: [], backlinks: [{ type: 'project', id: 2, name: 'Jarvis' }] },
]

test('active graph preserves explicit reference direction and counts facts', () => {
  const graph = buildActiveGraph(pages, index)
  assert.equal(graph.nodes.length, 3)
  assert.deepEqual(graph.links.map((link) => `${link.source}->${link.target}`), ['principal:1->project:2', 'project:2->person:3'])
  assert.deepEqual(graphCounts(graph), { nodes: 3, links: 2, facts: 13 })
})

test('focus graph contains exactly the selected page and one-hop references', () => {
  const graph = buildFocusGraph(pages[1], index, index)
  assert.deepEqual(new Set(graph.nodes.map((node) => node.id)), new Set(['principal:1', 'project:2', 'person:3']))
  assert.equal(graph.links.length, 2)
  assert.equal(graph.nodes.find((node) => node.id === 'project:2')?.fx, 0)
  assert.ok((graph.nodes.find((node) => node.id === 'principal:1')?.fx || 0) < 0)
  assert.ok((graph.nodes.find((node) => node.id === 'person:3')?.fx || 0) > 0)
  assert.equal(graph.nodes.every((node) => node.x === node.fx && node.y === node.fy && node.z === node.fz), true)
})

test('type filters never discard the selected entity', () => {
  const graph = buildActiveGraph(pages, index)
  const filtered = filterGraph(graph, new Set(['person']), 'project:2')
  assert.deepEqual(filtered.nodes.map((node) => node.id), ['project:2', 'person:3'])
  assert.equal(filtered.links.length, 1)
})

test('range, direction, and isolated-node filters preserve a coherent subgraph', () => {
  const graph = buildActiveGraph(pages, index)
  graph.nodes.push({ ...graph.nodes[2], id: 'person:99', pageId: 99, name: '离群节点' })

  const primary = graphForNodeIds(graph, primaryComponentIds(graph))
  assert.deepEqual(primary.nodes.map((node) => node.id), ['principal:1', 'project:2', 'person:3'])

  const outgoing = filterGraphByDirection(graph, 'project:2', 'outgoing')
  assert.deepEqual(outgoing.nodes.map((node) => node.id), ['project:2', 'person:3'])
  assert.deepEqual(outgoing.links.map((link) => link.id), ['project:2->person:3'])

  const incoming = filterGraphByDirection(graph, 'project:2', 'incoming')
  assert.deepEqual(incoming.nodes.map((node) => node.id), ['principal:1', 'project:2'])
  assert.deepEqual(incoming.links.map((link) => link.id), ['principal:1->project:2'])

  const connected = withoutIsolatedNodes(graph)
  assert.equal(connected.nodes.some((node) => node.id === 'person:99'), false)
})

test('nested summary payloads unwrap to readable markdown', () => {
  assert.equal(readableSummary('{"summary":"真实摘要"}'), '真实摘要')
  assert.equal(readableSummary('普通 Markdown'), '普通 Markdown')
})

test('primary component excludes disconnected outliers from the default frame', () => {
  const graph = buildActiveGraph(pages, index)
  graph.nodes.push({ ...graph.nodes[2], id: 'person:99', pageId: 99, name: '离群节点' })
  assert.deepEqual(primaryComponentIds(graph), new Set(['principal:1', 'project:2', 'person:3']))
})
