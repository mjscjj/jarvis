import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Empty, Input, Segmented, Spin, Switch, Tag, Tooltip } from 'antd'
import type { ForceGraphMethods, NodeObject } from 'react-force-graph-3d'
import ForceGraph3D from 'react-force-graph-3d'
import SpriteText from 'three-spritetext'
import { AdditiveBlending, Group, Mesh, MeshBasicMaterial, MeshLambertMaterial, SphereGeometry } from 'three'
import { getPage, listPages } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import type { PageIndexItem, PageType, PageView } from '../types'
import { focusDistance } from './camera'
import {
  buildActiveGraph,
  buildFocusGraph,
  connectedIds,
  filterGraph,
  graphCounts,
  linkEndpointId,
  nodeKey,
  pageTypeMeta,
  pageTypes,
  type WorldGraph,
  type WorldLink,
  type WorldNode,
} from './graphData'
import './world-map.css'

type GraphRef = ForceGraphMethods<WorldNode, WorldLink>
type ViewMode = 'global' | 'focus'

const emptyGraph: WorldGraph = { nodes: [], links: [] }

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

async function loadPages(index: PageIndexItem[], signal: AbortSignal, concurrency = 8): Promise<PageView[]> {
  const result = new Array<PageView>(index.length)
  let cursor = 0
  async function worker() {
    while (!signal.aborted) {
      const current = cursor++
      if (current >= index.length) return
      const item = index[current]
      result[current] = await getPage(item.type, item.id, signal)
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, index.length) }, worker))
  return result
}

function useElementSize<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [size, setSize] = useState({ width: 900, height: 660 })
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const update = () => setSize({ width: Math.max(320, element.clientWidth), height: Math.max(460, element.clientHeight) })
    update()
    const observer = new ResizeObserver(update)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  return { ref, size }
}

function relationText(link: WorldLink, selectedId: string): { arrow: string; id: string } {
  const source = linkEndpointId(link.source)
  const target = linkEndpointId(link.target)
  return source === selectedId ? { arrow: '引用了 →', id: target } : { arrow: '← 引用于', id: source }
}

export default function WorldMap() {
  const graphRef = useRef<GraphRef | undefined>(undefined)
  const { ref: stageRef, size } = useElementSize<HTMLDivElement>()
  const pageCache = useRef(new Map<string, PageView>())
  const [activeIndex, setActiveIndex] = useState<PageIndexItem[]>([])
  const [fullIndex, setFullIndex] = useState<PageIndexItem[]>([])
  const [activeGraph, setActiveGraph] = useState<WorldGraph>(emptyGraph)
  const [focusGraph, setFocusGraph] = useState<WorldGraph>(emptyGraph)
  const [selectedId, setSelectedId] = useState<string>()
  const [selectedPage, setSelectedPage] = useState<PageView>()
  const [mode, setMode] = useState<ViewMode>('global')
  const [visibleTypes, setVisibleTypes] = useState<Set<PageType>>(() => new Set(pageTypes))
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [selecting, setSelecting] = useState(false)
  const [error, setError] = useState<string>()
  const [autoRotate, setAutoRotate] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    Promise.all([listPages(false, controller.signal), listPages(true, controller.signal)])
      .then(async ([active, all]) => {
        setActiveIndex(active)
        setFullIndex(all)
        const pages = await loadPages(active, controller.signal)
        pages.forEach((page) => pageCache.current.set(nodeKey(page.type, page.id), page))
        setActiveGraph(buildActiveGraph(pages, active))
        setError(undefined)
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [])

  const rawGraph = mode === 'focus' ? focusGraph : activeGraph
  const graph = useMemo(
    () => filterGraph(rawGraph, visibleTypes, selectedId),
    [rawGraph, selectedId, visibleTypes],
  )
  const connections = useMemo(() => connectedIds(graph, selectedId), [graph, selectedId])
  const counts = useMemo(() => graphCounts(graph), [graph])
  const nodeById = useMemo(() => new Map(graph.nodes.map((node) => [node.id, node])), [graph.nodes])

  const searchResults = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase()
    if (!normalized) return []
    return fullIndex
      .filter((item) => `${item.name} ${item.index_line || ''}`.toLocaleLowerCase().includes(normalized))
      .slice(0, 12)
  }, [fullIndex, query])

  const focusCamera = useCallback((node: WorldNode) => {
    const graphNode = graph.nodes.find((candidate) => candidate.id === node.id) || node
    const x = graphNode.x || 0
    const y = graphNode.y || 0
    const z = graphNode.z || 0
    const magnitude = Math.hypot(x, y, z) || 1
    const distance = focusDistance(node.size, size.height)
    graphRef.current?.cameraPosition(
      { x: x + (x / magnitude) * distance, y: y + (y / magnitude) * distance, z: z + (z / magnitude) * distance },
      { x, y, z },
      850,
    )
  }, [graph.nodes, size.height])

  const selectEntity = useCallback(async (type: PageType, id: number, switchToFocus = false) => {
    const key = nodeKey(type, id)
    setSelecting(true)
    try {
      let page = pageCache.current.get(key)
      if (!page) {
        page = await getPage(type, id)
        pageCache.current.set(key, page)
      }
      setSelectedId(key)
      setSelectedPage(page)
      setFocusGraph(buildFocusGraph(page, activeIndex, fullIndex))
      if (switchToFocus || !activeGraph.nodes.some((node) => node.id === key)) setMode('focus')
      setQuery('')
      setError(undefined)
      window.setTimeout(() => {
        if (switchToFocus || !activeGraph.nodes.some((node) => node.id === key)) {
          graphRef.current?.zoomToFit(750, 80)
          return
        }
        const target = graph.nodes.find((node) => node.id === key)
        if (target) focusCamera(target)
      }, 180)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSelecting(false)
    }
  }, [activeGraph.nodes, activeIndex, focusCamera, fullIndex, graph])

  const resetView = useCallback(() => {
    setSelectedId(undefined)
    setSelectedPage(undefined)
    setMode('global')
    setVisibleTypes(new Set(pageTypes))
    setQuery('')
    window.setTimeout(() => graphRef.current?.zoomToFit(700, 56), 120)
  }, [])

  useEffect(() => {
    const controls = graphRef.current?.controls() as { autoRotate?: boolean; autoRotateSpeed?: number } | undefined
    if (!controls) return
    controls.autoRotate = autoRotate
    controls.autoRotateSpeed = 0.38
  }, [autoRotate, graph])

  useEffect(() => {
    if (!graph.nodes.length) return
    const timeout = window.setTimeout(() => graphRef.current?.zoomToFit(750, 54), 260)
    return () => window.clearTimeout(timeout)
  }, [mode, visibleTypes, graph.nodes.length])

  const nodeObject = useCallback((node: NodeObject<WorldNode>) => {
    const worldNode = node as WorldNode
    const selected = worldNode.id === selectedId
    const connected = !selectedId || connections.has(worldNode.id)
    const meta = pageTypeMeta[worldNode.pageType]
    const color = connected ? meta.color : meta.dimColor
    const group = new Group()
    const sphere = new Mesh(
      new SphereGeometry(worldNode.size, 22, 22),
      new MeshLambertMaterial({ color, transparent: true, opacity: connected ? 0.92 : 0.25 }),
    )
    group.add(sphere)
    if (selected) {
      const halo = new Mesh(
        new SphereGeometry(worldNode.size * 1.38, 20, 20),
        new MeshBasicMaterial({ color: meta.color, transparent: true, opacity: 0.17, blending: AdditiveBlending, depthWrite: false }),
      )
      group.add(halo)
    }
    const label = new SpriteText(worldNode.name)
    label.color = connected ? '#f8fbff' : '#8090a8'
    label.backgroundColor = selected ? 'rgba(9, 16, 30, .86)' : 'rgba(9, 16, 30, .58)'
    label.padding = [3, 5]
    label.borderRadius = 3
    label.textHeight = selected ? 4.6 : 3.4
    label.position.y = worldNode.size + 5
    label.material.depthWrite = false
    label.onBeforeRender = (_renderer, _scene, camera) => {
      const distance = camera.position.distanceTo(label.getWorldPosition(label.position.clone()))
      label.visible = selected || worldNode.pageType === 'principal' || worldNode.pageType === 'project' || (connected && distance < 180)
    }
    group.add(label)
    return group
  }, [connections, selectedId])

  const toggleType = (type: PageType) => {
    setVisibleTypes((current) => {
      const next = new Set(current)
      if (next.has(type) && next.size > 1) next.delete(type)
      else next.add(type)
      return next
    })
  }

  const selectedRelations = selectedId
    ? rawGraph.links.filter((link) => linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId)
    : []

  return (
    <div className="world-map-shell">
      <div className="world-map-toolbar">
        <div className="world-map-title-block">
          <span className="world-map-kicker">LIVE WORLD MODEL</span>
          <strong>语义观测台</strong>
          <span>节点来自实体页，连线只表示 Markdown 中的显式引用</span>
        </div>
        <div className="world-map-actions">
          <Segmented<ViewMode> value={mode} onChange={setMode} options={[{ label: '活跃全局', value: 'global' }, { label: '一跳关系', value: 'focus', disabled: !selectedPage }]} />
          <Tooltip title="让星图缓慢旋转"><span className="world-map-switch"><Switch size="small" checked={autoRotate} onChange={setAutoRotate} /> 漂移</span></Tooltip>
          <Button onClick={() => graphRef.current?.zoomToFit(650, 54)}>适配</Button>
          <Button onClick={resetView}>重置</Button>
        </div>
      </div>

      {error && <Alert type="error" showIcon closable title="世界地图加载失败" description={error} onClose={() => setError(undefined)} />}

      <div className="world-map-layout">
        <section className="world-map-canvas-card">
          <div className="world-map-search">
            <Input.Search allowClear value={query} onChange={(event) => setQuery(event.target.value)} placeholder={`搜索全部 ${fullIndex.length.toLocaleString()} 个实体`} loading={selecting} />
            {searchResults.length > 0 && (
              <div className="world-map-search-results">
                {searchResults.map((item) => (
                  <button key={nodeKey(item.type, item.id)} type="button" onClick={() => void selectEntity(item.type, item.id, true)}>
                    <span className="world-map-search-dot" style={{ background: pageTypeMeta[item.type].color }} />
                    <span><strong>{item.name}</strong><small>{pageTypeMeta[item.type].label} · {item.index_line || '暂无摘要'}</small></span>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="world-map-legend">
            {pageTypes.map((type) => (
              <button key={type} type="button" className={visibleTypes.has(type) ? 'is-active' : ''} onClick={() => toggleType(type)}>
                <i style={{ background: pageTypeMeta[type].color }} />{pageTypeMeta[type].label}
              </button>
            ))}
          </div>

          <div className="world-map-stats">
            <span><b>{counts.nodes}</b> 实体</span><span><b>{counts.links}</b> 引用</span><span><b>{counts.facts.toLocaleString()}</b> 事实</span>
          </div>

          <div className="world-map-stage" ref={stageRef}>
            {loading ? (
              <div className="world-map-loading"><Spin size="large" /><span>正在读取真实世界模型…</span></div>
            ) : graph.nodes.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前筛选没有可显示的实体" />
            ) : (
              <ForceGraph3D<WorldNode, WorldLink>
                ref={graphRef}
                width={size.width}
                height={size.height}
                graphData={graph}
                backgroundColor="#07101d"
                nodeThreeObject={nodeObject}
                nodeThreeObjectExtend={false}
                nodeLabel={(node) => `${node.name} · ${pageTypeMeta[node.pageType].label}`}
                linkColor={(link) => !selectedId || connections.has(linkEndpointId(link.source)) && connections.has(linkEndpointId(link.target)) ? '#5f7896' : '#243247'}
                linkWidth={(link) => selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? 1.25 : 0.42}
                linkOpacity={0.52}
                linkDirectionalArrowLength={2.8}
                linkDirectionalArrowRelPos={0.8}
                linkDirectionalArrowColor={() => '#8ca7c4'}
                linkDirectionalParticles={(link) => selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? 2 : 0}
                linkDirectionalParticleWidth={1.1}
                linkDirectionalParticleSpeed={0.004}
                linkDirectionalParticleColor={() => '#d7e8ff'}
                d3AlphaDecay={0.035}
                d3VelocityDecay={0.32}
                warmupTicks={60}
                cooldownTicks={160}
                showNavInfo={false}
                onNodeClick={(node) => void selectEntity(node.pageType, node.pageId)}
                onBackgroundClick={() => { setSelectedId(undefined); setSelectedPage(undefined) }}
              />
            )}
          </div>
          <div className="world-map-hint">拖拽旋转 · 滚轮缩放 · 点击节点查看真实详情</div>
        </section>

        <aside className="world-map-inspector">
          {!selectedPage || !selectedId ? (
            <div className="world-map-inspector-empty">
              <span className="world-map-orbit-mark">◎</span>
              <strong>选择一个实体</strong>
              <p>点击图中节点，或搜索完整索引中的历史实体。右侧会展示实体摘要、事实数量和真实引用关系。</p>
            </div>
          ) : (
            <>
              <div className="world-map-detail-head">
                <span className="world-map-detail-dot" style={{ background: pageTypeMeta[selectedPage.type].color }} />
                <div><Tag color={pageTypeMeta[selectedPage.type].color}>{pageTypeMeta[selectedPage.type].label}</Tag><h2>{selectedPage.name}</h2></div>
              </div>
              <div className="world-map-detail-metrics">
                <span><b>{selectedPage.fact_count}</b>长期事实</span>
                <span><b>{selectedPage.outgoing.length}</b>向外引用</span>
                <span><b>{selectedPage.backlinks.length}</b>反向引用</span>
              </div>
              <Button type="primary" block onClick={() => { setMode('focus'); window.setTimeout(() => graphRef.current?.zoomToFit(650, 80), 150) }}>只看一跳关系</Button>
              <div className="world-map-section">
                <h3>长期事实页</h3>
                {selectedPage.summary ? <MarkdownReport content={selectedPage.summary} className="world-map-summary" /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无摘要" />}
              </div>
              <div className="world-map-section">
                <h3>引用关系 <small>{selectedRelations.length}</small></h3>
                <div className="world-map-relations">
                  {selectedRelations.length === 0 && <span className="world-map-muted">暂无显式引用关系</span>}
                  {selectedRelations.map((link) => {
                    const relation = relationText(link, selectedId)
                    const neighbor = nodeById.get(relation.id) || rawGraph.nodes.find((node) => node.id === relation.id)
                    if (!neighbor) return null
                    return (
                      <button key={link.id} type="button" onClick={() => void selectEntity(neighbor.pageType, neighbor.pageId, true)}>
                        <i style={{ background: pageTypeMeta[neighbor.pageType].color }} />
                        <span><small>{relation.arrow}</small>{neighbor.name}</span>
                      </button>
                    )
                  })}
                </div>
              </div>
            </>
          )}
        </aside>
      </div>
    </div>
  )
}
