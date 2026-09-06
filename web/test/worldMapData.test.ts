import assert from 'node:assert/strict'
import test from 'node:test'
import type { PageIndexItem, PageView } from '../src/types.ts'
import { buildActiveGraph, buildFocusGraph, filterGraph, graphCounts, readableSummary } from '../src/world-map/graphData.ts'
import { focusDistance } from '../src/world-map/camera.ts'

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
})

test('type filters never discard the selected entity', () => {
  const graph = buildActiveGraph(pages, index)
  const filtered = filterGraph(graph, new Set(['person']), 'project:2')
  assert.deepEqual(filtered.nodes.map((node) => node.id), ['project:2', 'person:3'])
  assert.equal(filtered.links.length, 1)
})

test('nested summary payloads unwrap to readable markdown', () => {
  assert.equal(readableSummary('{"summary":"真实摘要"}'), '真实摘要')
  assert.equal(readableSummary('普通 Markdown'), '普通 Markdown')
})

test('focus camera stays usable across viewport and node sizes', () => {
  assert.ok(focusDistance(4, 320) >= 92)
  assert.ok(focusDistance(12, 1200) <= 290)
  assert.ok(focusDistance(12, 800) > focusDistance(4, 800))
})
