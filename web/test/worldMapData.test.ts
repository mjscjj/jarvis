import assert from 'node:assert/strict'
import test from 'node:test'
import type { EntityRelation, PageIndexItem, PageView } from '../src/types.ts'
import { buildActiveGraph, buildFocusGraph, filterGraph, filterGraphByDirection, graphCounts, graphForNodeIds, primaryComponentIds, readableSummary, withoutIsolatedNodes } from '../src/world-map/graphData.ts'
import { buildOKRGraph, okrWorldPageRefs } from '../src/world-map/okrGraph.ts'
import { defaultWorldLens, isOKRPluginEnabled } from '../src/world-map/lens.ts'
import type { Objective } from '../src/okr/emily/types.ts'
import { boundsForNodes, cameraFrameForBounds } from '../src/world-map/camera.ts'
import { createBoundingForce } from '../src/world-map/physics.ts'
import { cameraPaddingValue, defaultWorldMapSettings, labelLengthValue, normalizeWorldMapSettings, spacingValue } from '../src/world-map/settings.ts'

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

test('OKR lens shows native hierarchy but requires persisted relations for world owners', () => {
  const objectives: Objective[] = [{
    id: 'o-1',
    title: '稳定交付',
    krs: [{
      id: 'kr-1', title: '完成上线', metricNote: '', metrics: [], entries: [],
      owners: [{ openId: 'ou_me', name: '我' }, { openId: 'ou_dynamic', name: '临时协作者' }],
      points: [{ id: 'point-1', kind: 'strategy', title: '灰度发布', entries: [], owners: [{ openId: 'ou_person', name: '协作者' }] }],
    }],
  }]
  const graph = buildOKRGraph({ objectives, relations: [], activePages: pages, fullIndex: index, objectiveId: 'o-1' })

  assert.deepEqual(new Set(graph.nodes.map((node) => node.id)), new Set([
    'okr_objective:o-1', 'okr_kr:kr-1', 'okr_point:point-1',
  ]))
  assert.equal(graph.links.filter((link) => link.relationType === 'contains').length, 2)
  assert.equal(graph.links.filter((link) => link.relationType === 'owned_by').length, 0)
})

test('OKR lens keeps the complete quarter hierarchy in overview and combines confirmed canonical relations', () => {
  const objectives: Objective[] = [{
    id: 'o-1', title: '稳定交付', krs: [{ id: 'kr-1', title: '完成上线', metricNote: '', metrics: [], points: [{ id: 'point-1', kind: 'product', title: '灰度', entries: [] }] }],
  }]
  const relations: EntityRelation[] = [
    { id: 7, source_type: 'okr_kr', source_id: 'kr-1', relation_type: 'maps_to', target_type: 'key_matter', target_id: '77', evidence: {}, confidence: 1, confirmed_at: '2026-09-07T00:00:00Z', created_at: '', updated_at: '' },
    { id: 8, source_type: 'okr_point', source_id: 'point-1', relation_type: 'advances', target_type: 'key_matter', target_id: '77', evidence: {}, confidence: 1, confirmed_at: '2026-09-07T00:00:00Z', created_at: '', updated_at: '' },
    { id: 9, source_type: 'key_matter', source_id: '77', relation_type: 'advanced_by', target_type: 'okr_point', target_id: 'point-1', evidence: {}, confidence: 1, confirmed_at: '2026-09-07T00:00:00Z', created_at: '', updated_at: '' },
  ]
  const expanded = buildOKRGraph({ objectives, relations, activePages: pages, fullIndex: [...index, { type: 'key_matter', id: 77, name: '灰度事项', index_line: '', char_count: 0, last_progress_at: null }], objectiveId: 'o-1' })
  assert.ok(expanded.links.some((link) => link.id === 'relation:7' && link.source === 'okr_kr:kr-1' && link.target === 'key_matter:77' && link.label === '映射到'))
  assert.ok(expanded.links.some((link) => link.id === 'relation:8' && link.source === 'okr_point:point-1' && link.target === 'key_matter:77' && link.label === '推进'))
  assert.equal(expanded.links.some((link) => link.id === 'relation:9'), false)

  const overview = buildOKRGraph({ objectives, relations, activePages: pages, fullIndex: index })
  assert.deepEqual(new Set(overview.nodes.map((node) => node.id)), new Set(['okr_objective:o-1', 'okr_kr:kr-1', 'okr_point:point-1', 'key_matter:77']))
})

test('world map defaults to OKR only when the OKR plugin is active in the runtime', () => {
  const enabled = [{ key: 'okr', name: 'OKR 插件', description: '', is_enabled: true, configured_enabled: true, restart_required: false, requires: [] }]
  const pendingRestart = [{ ...enabled[0], is_enabled: false, configured_enabled: true, restart_required: true }]
  assert.equal(isOKRPluginEnabled(enabled), true)
  assert.equal(defaultWorldLens(enabled), 'okr')
  assert.equal(isOKRPluginEnabled(pendingRestart), false)
  assert.equal(defaultWorldLens(pendingRestart), 'world')
})

test('OKR world pages are loaded from confirmed canonical targets even when absent from the active index', () => {
  const objectives: Objective[] = [{
    id: 'o-1', title: '稳定交付', krs: [{ id: 'kr-1', title: '完成上线', metricNote: '', metrics: [], points: [] }],
  }]
  const relations: EntityRelation[] = [
    { id: 1, source_type: 'okr_objective', source_id: 'o-1', relation_type: 'maps_to', target_type: 'project', target_id: '19', evidence: {}, confidence: 1, confirmed_at: '2026-09-07T00:00:00Z', created_at: '', updated_at: '' },
    { id: 2, source_type: 'okr_kr', source_id: 'kr-1', relation_type: 'owned_by', target_type: 'person', target_id: '71', evidence: {}, confidence: 1, confirmed_at: '2026-09-07T00:00:00Z', created_at: '', updated_at: '' },
    { id: 3, source_type: 'okr_kr', source_id: 'kr-1', relation_type: 'maps_to', target_type: 'key_matter', target_id: '60', evidence: {}, confidence: 1, confirmed_at: null, created_at: '', updated_at: '' },
  ]
  assert.deepEqual(okrWorldPageRefs(objectives, relations), [
    { type: 'project', id: 19 },
    { type: 'person', id: 71 },
  ])
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

test('camera frames the selected neighborhood and respects viewport aspect ratio', () => {
  const landscape = cameraFrameForBounds({ x: [-80, 80], y: [-60, 60], z: [-8, 8] }, 1000, 650)
  const portrait = cameraFrameForBounds({ x: [-80, 80], y: [-60, 60], z: [-8, 8] }, 420, 650)
  assert.deepEqual(landscape.center, { x: 0, y: 0, z: 0 })
  assert.ok(landscape.distance >= 86)
  assert.ok(portrait.distance > landscape.distance)
})

test('deterministic bounds prefer fixed focus coordinates over stale simulation positions', () => {
  const bounds = boundsForNodes([{ x: 900, y: 900, z: 900, fx: 0, fy: 0, fz: 0 }, { x: -800, y: -800, z: -800, fx: 100, fy: 50, fz: 10 }], 10)
  assert.deepEqual(bounds, { x: [-10, 110], y: [-10, 60], z: [-10, 20] })
})

test('trimmed camera bounds ignore a small number of extreme coordinates', () => {
  const nodes = Array.from({ length: 20 }, (_, index) => ({ x: index * 4, y: index * 2, z: index }))
  nodes.push({ x: 8000, y: -9000, z: 7000 })
  const bounds = boundsForNodes(nodes, 0, .05)
  assert.ok(bounds)
  assert.deepEqual(bounds.x, [4, 76])
  assert.deepEqual(bounds.y, [0, 36])
  assert.deepEqual(bounds.z, [1, 19])
})

test('primary component excludes disconnected outliers from the default frame', () => {
  const graph = buildActiveGraph(pages, index)
  graph.nodes.push({ ...graph.nodes[2], id: 'person:99', pageId: 99, name: '离群节点' })
  assert.deepEqual(primaryComponentIds(graph), new Set(['principal:1', 'project:2', 'person:3']))
})

test('bounding force pulls distant nodes inward without moving nearby nodes', () => {
  const near = { x: 20, y: 0, z: 0, vx: 0, vy: 0, vz: 0 }
  const far = { x: 200, y: 0, z: 0, vx: 0, vy: 0, vz: 0 }
  const force = createBoundingForce(100)
  force.initialize([near, far])
  force(1)
  assert.equal(near.vx, 0)
  assert.ok(far.vx < 0)
})

test('world map settings normalize persisted values and preserve safe defaults', () => {
  const settings = normalizeWorldMapSettings({ labelMode: 'all', cameraRange: 'invalid', arrows: false })
  assert.equal(settings.labelMode, 'all')
  assert.equal(settings.cameraRange, defaultWorldMapSettings.cameraRange)
  assert.equal(settings.arrows, false)
  assert.equal(settings.particles, defaultWorldMapSettings.particles)
  assert.equal(labelLengthValue('full'), Number.POSITIVE_INFINITY)
  assert.ok(cameraPaddingValue('far') > cameraPaddingValue('near'))
  assert.ok(spacingValue('loose') > spacingValue('compact'))
})
