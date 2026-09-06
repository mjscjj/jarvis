import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { CompressOutlined, SearchOutlined, SettingOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Input, Popover, Segmented, Spin, Switch, Tag } from 'antd'
import type { ForceGraphMethods, NodeObject } from 'react-force-graph-2d'
import ForceGraph2D from 'react-force-graph-2d'
import { getPage, listPages } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import type { PageIndexItem, PageType, PageView } from '../types'
import {
  buildActiveGraph,
  buildFocusGraph,
  connectedIds,
  filterGraph,
  filterGraphByDirection,
  graphCounts,
  graphForNodeIds,
  isPageType,
  linkEndpointId,
  nodeKey,
  pageTypeMeta,
  pageTypes,
  primaryComponentIds,
  withoutIsolatedNodes,
  type RelationDirection,
  type WorldGraph,
  type WorldLink,
  type WorldNode,
} from './graphData'
import './world-map.css'

type GraphRef = ForceGraphMethods<WorldNode, WorldLink>
type NetworkScope = 'primary' | 'all' | 'focus'
type LabelDensity = 'auto' | 'all' | 'related' | 'hidden'

const emptyGraph: WorldGraph = { nodes: [], links: [] }

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function compactLabel(value: string, maxLength = 18): string {
  const characters = Array.from(value)
  return characters.length > maxLength ? `${characters.slice(0, maxLength).join('')}…` : value
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
  const [size, setSize] = useState({ width: 900, height: 680 })
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const update = () => setSize({ width: Math.max(320, element.clientWidth), height: Math.max(480, element.clientHeight) })
    update()
    const observer = new ResizeObserver(update)
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  return { ref, size }
}

export default function WorldMap() {
  const graphRef = useRef<GraphRef | undefined>(undefined)
  const forceSignatureRef = useRef('')
  const framedSignatureRef = useRef('')
  const pageCache = useRef(new Map<string, PageView>())
  const { ref: stageRef, size } = useElementSize<HTMLDivElement>()
  const [activeIndex, setActiveIndex] = useState<PageIndexItem[]>([])
  const [fullIndex, setFullIndex] = useState<PageIndexItem[]>([])
  const [activeGraph, setActiveGraph] = useState<WorldGraph>(emptyGraph)
  const [focusGraph, setFocusGraph] = useState<WorldGraph>(emptyGraph)
  const [selectedId, setSelectedId] = useState<string>()
  const [selectedPage, setSelectedPage] = useState<PageView>()
  const [scope, setScope] = useState<NetworkScope>('primary')
  const [relationDirection, setRelationDirection] = useState<RelationDirection>('both')
  const [showIsolated, setShowIsolated] = useState(false)
  const [labelDensity, setLabelDensity] = useState<LabelDensity>('auto')
  const [visibleTypes, setVisibleTypes] = useState<Set<PageType>>(() => new Set(pageTypes))
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(true)
  const [selecting, setSelecting] = useState(false)
  const [error, setError] = useState<string>()

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

  const activePrimaryIds = useMemo(() => primaryComponentIds(activeGraph), [activeGraph])
  const rawGraph = useMemo(() => {
    if (scope === 'focus') return focusGraph
    if (scope === 'primary') return graphForNodeIds(activeGraph, activePrimaryIds)
    return activeGraph
  }, [activeGraph, activePrimaryIds, focusGraph, scope])
  const graph = useMemo(() => {
    const byType = filterGraph(rawGraph, visibleTypes, selectedId)
    const byDirection = scope === 'focus' ? filterGraphByDirection(byType, selectedId, relationDirection) : byType
    return scope === 'all' && !showIsolated ? withoutIsolatedNodes(byDirection, selectedId) : byDirection
  }, [rawGraph, relationDirection, scope, selectedId, showIsolated, visibleTypes])
  const connections = useMemo(() => connectedIds(graph, selectedId), [graph, selectedId])
  const counts = useMemo(() => graphCounts(graph), [graph])

  const searchResults = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    if (!needle) return []
    return fullIndex
      .filter((item) => `${item.name} ${item.index_line || ''}`.toLocaleLowerCase().includes(needle))
      .slice(0, 12)
  }, [fullIndex, query])

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
      setFocusGraph(buildFocusGraph(page, activeIndex, fullIndex, 'relation', 'standard'))
      if (switchToFocus || !activeGraph.nodes.some((node) => node.id === key)) setScope('focus')
      setQuery('')
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSelecting(false)
    }
  }, [activeGraph.nodes, activeIndex, fullIndex])

  useEffect(() => {
    if (!selectedPage) return
    setFocusGraph(buildFocusGraph(selectedPage, activeIndex, fullIndex, 'relation', 'standard'))
  }, [activeIndex, fullIndex, selectedPage])

  const clearSelection = useCallback(() => {
    setSelectedId(undefined)
    setSelectedPage(undefined)
    setScope((current) => current === 'focus' ? 'primary' : current)
    setQuery('')
  }, [])

  const resetView = useCallback(() => {
    setVisibleTypes(new Set(pageTypes))
    setScope('primary')
    setRelationDirection('both')
    setShowIsolated(false)
    setLabelDensity('auto')
    clearSelection()
  }, [clearSelection])

  const frameGraph = useCallback((targetIds?: Set<string>, duration = 680) => {
    window.requestAnimationFrame(() => {
      if (!graphRef.current || !graph.nodes.length) return
      const filter = targetIds?.size ? (candidate: NodeObject<WorldNode>) => targetIds.has(String(candidate.id)) : undefined
      graphRef.current.zoomToFit(duration, targetIds ? 92 : 72, filter)
    })
  }, [graph.nodes.length])

  useEffect(() => {
    if (!graph.nodes.length) return
    framedSignatureRef.current = ''
    const timer = window.setTimeout(() => frameGraph(undefined, 520), scope === 'focus' ? 40 : 720)
    return () => window.clearTimeout(timer)
  }, [frameGraph, graph.nodes, scope])

  const configureForces = useCallback(() => {
    const instance = graphRef.current
    if (!instance || scope === 'focus') return
    const signature = `${scope}:${graph.nodes.length}`
    if (forceSignatureRef.current === signature) return
    const linkForce = instance.d3Force('link') as { distance?: (value: number) => unknown } | undefined
    const chargeForce = instance.d3Force('charge') as { strength?: (value: number) => unknown } | undefined
    linkForce?.distance?.(82)
    chargeForce?.strength?.(-180)
    instance.d3ReheatSimulation()
    forceSignatureRef.current = signature
  }, [graph.nodes.length, scope])

  const handleEngineStop = useCallback(() => {
    const signature = `${scope}:${graph.nodes.map((node) => node.id).join('|')}`
    if (framedSignatureRef.current === signature) return
    framedSignatureRef.current = signature
    frameGraph(undefined, 460)
  }, [frameGraph, graph.nodes, scope])

  const drawNode = useCallback((candidate: NodeObject<WorldNode>, context: CanvasRenderingContext2D, scale: number) => {
    const node = candidate as WorldNode
    if (!Number.isFinite(node.x) || !Number.isFinite(node.y)) return
    const selected = node.id === selectedId
    const connected = !selectedId || connections.has(node.id)
    const meta = pageTypeMeta[node.pageType]
    const radius = node.size * (selected ? 1.32 : 1)
    const alpha = connected ? 1 : 0.13

    context.save()
    context.globalAlpha = alpha
    if (selected) {
      context.beginPath()
      context.arc(node.x!, node.y!, radius + 6 / scale, 0, Math.PI * 2)
      context.fillStyle = `${meta.color}2c`
      context.fill()
      context.lineWidth = 2.2 / scale
      context.strokeStyle = meta.color
      context.stroke()
    }
    context.beginPath()
    context.arc(node.x!, node.y!, radius, 0, Math.PI * 2)
    context.fillStyle = meta.color
    context.fill()
    context.lineWidth = 1.5 / scale
    context.strokeStyle = selected ? '#18211f' : '#ffffff'
    context.stroke()

    const important = node.pageType === 'principal' || node.pageType === 'project'
    const automatic = selected || (selectedId ? connected : important || scale > 2.15)
    const related = selectedId ? connected : important
    const showLabel = labelDensity === 'all' || labelDensity === 'auto' && automatic || labelDensity === 'related' && related
    if (showLabel) {
      const fontSize = (selected ? 14 : 12) / scale
      const label = compactLabel(node.name, selected ? 24 : 18)
      context.font = `${selected ? 650 : 520} ${fontSize}px Inter, "PingFang SC", sans-serif`
      context.textAlign = 'center'
      context.textBaseline = 'top'
      context.fillStyle = connected ? '#26302d' : '#9aa19e'
      context.fillText(label, node.x!, node.y! + radius + 4 / scale)
    }
    context.restore()
  }, [connections, labelDensity, selectedId])

  const paintNodeArea = useCallback((candidate: NodeObject<WorldNode>, color: string, context: CanvasRenderingContext2D) => {
    const node = candidate as WorldNode
    if (!Number.isFinite(node.x) || !Number.isFinite(node.y)) return
    context.beginPath()
    context.arc(node.x!, node.y!, Math.max(7, node.size + 3), 0, Math.PI * 2)
    context.fillStyle = color
    context.fill()
  }, [])

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
    const relations = [
      ...selectedPage.outgoing.filter((link) => isPageType(link.type)).map((link) => ({ ...link, type: link.type as PageType, direction: 'outgoing' as const })),
      ...selectedPage.backlinks.filter((link) => isPageType(link.type)).map((link) => ({ ...link, type: link.type as PageType, direction: 'incoming' as const })),
    ]
    return relationDirection === 'both' ? relations : relations.filter((relation) => relation.direction === relationDirection)
  }, [relationDirection, selectedPage])

  const viewOptions = (
    <div className="world-map-options">
      <div className="world-map-options-head"><strong>视图选项</strong><span>仅影响当前浏览</span></div>
      <label className={!selectedPage || scope !== 'focus' ? 'is-disabled' : ''}>
        <span><b>关系方向</b><small>在一跳范围中生效</small></span>
        <Segmented<RelationDirection>
          block
          size="small"
          disabled={!selectedPage || scope !== 'focus'}
          value={relationDirection}
          onChange={setRelationDirection}
          options={[{ label: '双向', value: 'both' }, { label: '向外', value: 'outgoing' }, { label: '向内', value: 'incoming' }]}
        />
      </label>
      <label>
        <span><b>孤立节点</b><small>在全局范围中生效</small></span>
        <Switch checkedChildren="显示" unCheckedChildren="隐藏" checked={showIsolated} onChange={setShowIsolated} />
      </label>
      <label>
        <span><b>标签密度</b><small>减少名称互相遮挡</small></span>
        <Segmented<LabelDensity>
          block
          size="small"
          value={labelDensity}
          onChange={setLabelDensity}
          options={[{ label: '自动', value: 'auto' }, { label: '全部', value: 'all' }, { label: '相关', value: 'related' }, { label: '隐藏', value: 'hidden' }]}
        />
      </label>
    </div>
  )

  return (
    <div className="world-map-shell">
      <div className="world-map-toolbar">
        <div className="world-map-title-block">
          <strong>关系地图</strong>
          <span>实体页之间的显式引用</span>
        </div>
        <div className="world-map-actions">
          <Segmented<NetworkScope>
            value={scope}
            onChange={setScope}
            options={[{ label: '主网络', value: 'primary' }, { label: '全局', value: 'all' }, { label: '一跳', value: 'focus', disabled: !selectedPage }]}
          />
          <Popover trigger="click" placement="bottomRight" content={viewOptions}><Button icon={<SettingOutlined />}>视图选项</Button></Popover>
          <Button icon={<CompressOutlined />} onClick={() => frameGraph()}>适应画布</Button>
          <Button onClick={resetView}>重置</Button>
        </div>
      </div>

      {error && <Alert type="error" showIcon closable title="关系地图加载失败" description={error} onClose={() => setError(undefined)} />}

      <div className="world-map-layout">
        <section className="world-map-canvas-card" aria-label="世界模型关系图">
          <div className="world-map-search">
            <Input
              allowClear
              value={query}
              prefix={<SearchOutlined />}
              placeholder={`搜索 ${fullIndex.length.toLocaleString()} 个实体`}
              onChange={(event) => setQuery(event.target.value)}
              aria-label="搜索实体"
            />
            {query.trim() && (
              <div className="world-map-search-results">
                {searchResults.length === 0 ? <span className="world-map-search-empty">没有匹配的实体</span> : searchResults.map((item) => (
                  <button key={nodeKey(item.type, item.id)} type="button" onClick={() => void selectEntity(item.type, item.id, true)}>
                    <i style={{ background: pageTypeMeta[item.type].color }} />
                    <span><b>{item.name}</b><small>{pageTypeMeta[item.type].label} · {item.index_line || '暂无摘要'}</small></span>
                  </button>
                ))}
              </div>
            )}
          </div>

          <div className="world-map-legend" aria-label="实体类型筛选">
            {pageTypes.map((type) => (
              <button key={type} type="button" className={visibleTypes.has(type) ? 'is-active' : ''} aria-pressed={visibleTypes.has(type)} onClick={() => toggleType(type)}>
                <i style={{ background: pageTypeMeta[type].color }} />{pageTypeMeta[type].label}
              </button>
            ))}
          </div>

          <div className="world-map-stats">
            <span><b>{counts.nodes}</b>实体</span>
            <span><b>{counts.links}</b>引用</span>
            <span><b>{counts.facts.toLocaleString()}</b>事实</span>
          </div>

          {scope === 'focus' && <div className="world-map-direction-guide" aria-hidden="true">
            <span>{relationDirection !== 'outgoing' ? '反向引用' : ''}</span>
            <span>{relationDirection !== 'incoming' ? '向外引用' : ''}</span>
          </div>}

          <div className="world-map-stage" ref={stageRef}>
            {loading ? (
              <div className="world-map-loading"><Spin size="large" /><span>正在读取世界模型…</span></div>
            ) : graph.nodes.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前筛选没有可显示的实体" />
            ) : (
              <ForceGraph2D<WorldNode, WorldLink>
                key={scope}
                ref={graphRef}
                width={size.width}
                height={size.height}
                graphData={graph}
                backgroundColor="#fbfaf7"
                nodeCanvasObject={drawNode}
                nodePointerAreaPaint={paintNodeArea}
                nodeLabel={(node) => `${node.name} · ${pageTypeMeta[node.pageType].label}`}
                linkColor={(link) => selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? '#56625e' : selectedId ? '#d9ddda' : '#b9c0bd'}
                linkWidth={(link) => selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? 1.4 : 0.65}
                linkDirectionalArrowLength={(link) => selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? 3.2 : 0}
                linkDirectionalArrowRelPos={0.82}
                linkDirectionalArrowColor={() => '#68736f'}
                d3AlphaDecay={0.035}
                d3VelocityDecay={0.34}
                cooldownTicks={scope === 'focus' ? 0 : 220}
                minZoom={0.22}
                maxZoom={12}
                onEngineTick={configureForces}
                onEngineStop={handleEngineStop}
                onNodeClick={(node) => void selectEntity(node.pageType, node.pageId)}
                onBackgroundClick={clearSelection}
              />
            )}
          </div>
          <div className="world-map-hint">拖动画布 · 滚轮缩放 · 点击节点查看关系</div>
        </section>

        <aside className="world-map-inspector">
          {!selectedPage || !selectedId ? (
            <div className="world-map-inspector-empty">
              <span className="world-map-empty-mark"><i /><i /><i /></span>
              <strong>从一个实体开始</strong>
              <p>点击节点，或搜索完整索引。选中后会突出它与直接关联实体的关系。</p>
            </div>
          ) : (
            <>
              <div className="world-map-detail-head">
                <span className="world-map-detail-dot" style={{ background: pageTypeMeta[selectedPage.type].color }} />
                <div><Tag color={pageTypeMeta[selectedPage.type].color}>{pageTypeMeta[selectedPage.type].label}</Tag><h2>{selectedPage.name}</h2></div>
              </div>
              <p className="world-map-index-line">{fullIndex.find((item) => nodeKey(item.type, item.id) === selectedId)?.index_line || '暂无索引摘要'}</p>
              <div className="world-map-detail-metrics">
                <span><b>{selectedPage.fact_count}</b>长期事实</span>
                <span><b>{selectedPage.outgoing.length}</b>向外引用</span>
                <span><b>{selectedPage.backlinks.length}</b>反向引用</span>
              </div>
              <Button type="primary" block disabled={scope === 'focus'} onClick={() => setScope('focus')}>只看一跳关系</Button>

              <div className="world-map-section">
                <h3>实体摘要</h3>
                {selectedPage.summary ? <MarkdownReport content={selectedPage.summary} className="world-map-summary" /> : <span className="world-map-muted">暂无摘要</span>}
              </div>
              <div className="world-map-section">
                <h3>直接关系 <small>{selectedRelations.length}{scope === 'focus' && selectedRelations.length > focusGraph.links.length ? ` · 图中显示 ${focusGraph.links.length}` : ''}</small></h3>
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
          {selecting && <div className="world-map-selecting"><Spin size="small" /> 正在读取实体</div>}
        </aside>
      </div>
    </div>
  )
}
