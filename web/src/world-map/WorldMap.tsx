import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Alert, Button, Empty, Input, Segmented, Spin, Switch, Tag, Tooltip } from 'antd'
import type { ForceGraphMethods, NodeObject } from 'react-force-graph-3d'
import ForceGraph3D from 'react-force-graph-3d'
import SpriteText from 'three-spritetext'
import { AdditiveBlending, Group, Mesh, MeshBasicMaterial, MeshLambertMaterial, SphereGeometry, Vector3 } from 'three'
import { getPage, listPages } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import type { PageIndexItem, PageType, PageView } from '../types'
import { cameraFrameForBounds } from './camera'
import {
  buildActiveGraph,
  buildFocusGraph,
  connectedIds,
  filterGraph,
  graphCounts,
  isPageType,
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

export default function WorldMap() {
  const graphRef = useRef<GraphRef | undefined>(undefined)
  const cameraRunRef = useRef(0)
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

  const searchResults = useMemo(() => {
    const normalized = query.trim().toLocaleLowerCase()
    if (!normalized) return []
    return fullIndex
      .filter((item) => `${item.name} ${item.index_line || ''}`.toLocaleLowerCase().includes(normalized))
      .slice(0, 12)
  }, [fullIndex, query])

  const frameGraph = useCallback((targetIds?: Set<string>, duration = 760) => {
    const run = ++cameraRunRef.current
    const attempt = (remaining: number) => {
      window.requestAnimationFrame(() => {
        if (run !== cameraRunRef.current || !graphRef.current) return
        const bounds = graphRef.current.getGraphBbox(targetIds ? (node) => targetIds.has(String(node.id)) : undefined)
        const values = [...bounds.x, ...bounds.y, ...bounds.z]
        const span = Math.max(bounds.x[1] - bounds.x[0], bounds.y[1] - bounds.y[0], bounds.z[1] - bounds.z[0])
        if ((!values.every(Number.isFinite) || (mode === 'global' && graph.nodes.length > 5 && span < 30)) && remaining > 0) {
          window.setTimeout(() => attempt(remaining - 1), 80)
          return
        }
        if (!values.every(Number.isFinite)) return
        const frame = cameraFrameForBounds(bounds, size.width, size.height, 50, targetIds ? 1.3 : 1.18)
        const angleX = mode === 'focus' ? 0 : frame.distance * .12
        const angleY = mode === 'focus' ? 0 : frame.distance * .05
        graphRef.current.cameraPosition(
          { x: frame.center.x + angleX, y: frame.center.y + angleY, z: frame.center.z + frame.distance },
          frame.center,
          duration,
        )
      })
    }
    attempt(8)
  }, [graph.nodes.length, mode, size.height, size.width])

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
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSelecting(false)
    }
  }, [activeGraph.nodes, activeIndex, fullIndex])

  const resetView = useCallback(() => {
    setSelectedId(undefined)
    setSelectedPage(undefined)
    setMode('global')
    setVisibleTypes(new Set(pageTypes))
    setQuery('')
  }, [])

  useEffect(() => {
    const controls = graphRef.current?.controls() as { autoRotate?: boolean; autoRotateSpeed?: number } | undefined
    if (!controls) return
    controls.autoRotate = autoRotate
    controls.autoRotateSpeed = 0.38
  }, [autoRotate, graph])

  useEffect(() => {
    if (!graph.nodes.length) return
    const targetIds = selectedId ? connections : undefined
    frameGraph(targetIds)
    return () => { cameraRunRef.current += 1 }
  }, [connections, frameGraph, graph.nodes.length, mode, selectedId, visibleTypes])

  const nodeObject = useCallback((node: NodeObject<WorldNode>) => {
    const worldNode = node as WorldNode
    const selected = worldNode.id === selectedId
    const connected = !selectedId || connections.has(worldNode.id)
    const meta = pageTypeMeta[worldNode.pageType]
    const color = connected ? meta.color : meta.dimColor
    const group = new Group()
    const displayRadius = worldNode.size * (selected ? 1.24 : 1)
    const sphere = new Mesh(
      new SphereGeometry(displayRadius, 22, 22),
      new MeshLambertMaterial({ color, transparent: true, opacity: connected ? 0.92 : 0.25 }),
    )
    group.add(sphere)
    if (selected) {
      const halo = new Mesh(
        new SphereGeometry(displayRadius * 1.72, 20, 20),
        new MeshBasicMaterial({ color: meta.color, transparent: true, opacity: 0.14, blending: AdditiveBlending, depthWrite: false }),
      )
      group.add(halo)
    }
    const label = new SpriteText(worldNode.name)
    label.color = connected ? '#f8fbff' : '#8090a8'
    label.backgroundColor = selected ? 'rgba(9, 16, 30, .86)' : 'rgba(9, 16, 30, .58)'
    label.padding = [3, 5]
    label.borderRadius = 3
    label.textHeight = selected ? 4.2 : 3.5
    label.position.y = displayRadius + 4.5
    label.material.depthWrite = false
    const baseScale = label.scale.clone()
    const worldPosition = new Vector3()
    label.onBeforeRender = (_renderer, _scene, camera) => {
      const distance = camera.position.distanceTo(label.getWorldPosition(worldPosition))
      const screenScale = Math.min(2.35, Math.max(.82, distance / 165))
      label.scale.set(baseScale.x * screenScale, baseScale.y * screenScale, baseScale.z)
      label.visible = selected || worldNode.pageType === 'principal' || (mode === 'focus' && connected) || (worldNode.pageType === 'project' && distance < 260) || (connected && distance < 170)
    }
    group.add(label)
    return group
  }, [connections, mode, selectedId])

  const toggleType = (type: PageType) => {
    setVisibleTypes((current) => {
      const next = new Set(current)
      if (next.has(type) && next.size > 1) next.delete(type)
      else next.add(type)
      return next
    })
  }

  const selectedRelations = useMemo(() => {
    if (!selectedPage) return []
    return [
      ...selectedPage.outgoing.filter((link) => isPageType(link.type)).map((link) => ({ ...link, type: link.type as PageType, direction: 'outgoing' as const })),
      ...selectedPage.backlinks.filter((link) => isPageType(link.type)).map((link) => ({ ...link, type: link.type as PageType, direction: 'incoming' as const })),
    ]
  }, [selectedPage])

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
          <Button onClick={() => frameGraph(selectedId ? connections : undefined)}>适配</Button>
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

          {selectedPage && (
            <div className="world-map-selection-badge">
              <i style={{ background: pageTypeMeta[selectedPage.type].color }} />
              <span><small>当前实体</small>{selectedPage.name}</span>
            </div>
          )}

          {mode === 'focus' && (
            <div className="world-map-direction-guide" aria-hidden="true">
              <span>← 反向引用</span><span>向外引用 →</span>
            </div>
          )}

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
                onBackgroundClick={() => { setSelectedId(undefined); setSelectedPage(undefined); setMode('global') }}
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
              <Button type="primary" block onClick={() => setMode('focus')}>只看一跳关系</Button>
              <div className="world-map-section">
                <h3>长期事实页</h3>
                {selectedPage.summary ? <MarkdownReport content={selectedPage.summary} className="world-map-summary" /> : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无摘要" />}
              </div>
              <div className="world-map-section">
                <h3>引用关系 <small>{selectedRelations.length}{mode === 'focus' && selectedRelations.length > focusGraph.links.length ? ` · 图中显示 ${focusGraph.links.length}` : ''}</small></h3>
                <div className="world-map-relations">
                  {selectedRelations.length === 0 && <span className="world-map-muted">暂无显式引用关系</span>}
                  {selectedRelations.map((relation) => (
                      <button key={`${relation.direction}:${relation.type}:${relation.id}`} type="button" onClick={() => void selectEntity(relation.type, relation.id, true)}>
                        <i style={{ background: pageTypeMeta[relation.type].color }} />
                        <span><small>{relation.direction === 'outgoing' ? '引用了 →' : '← 引用于'}</small>{relation.name}</span>
                      </button>
                  ))}
                </div>
              </div>
            </>
          )}
        </aside>
      </div>
    </div>
  )
}
