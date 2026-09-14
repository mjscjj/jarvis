import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AimOutlined, CompressOutlined, FullscreenExitOutlined, FullscreenOutlined, MinusOutlined, PlusOutlined, SearchOutlined, SettingOutlined } from '@ant-design/icons'
import { Alert, Button, Empty, Input, Popover, Segmented, Select, Space, Spin, Switch, Tag, Tooltip } from 'antd'
import type { ForceGraphMethods, NodeObject } from 'react-force-graph-2d'
import ForceGraph2D from 'react-force-graph-2d'
import { getPage, getProfile, listAppModules, listPages, listPersons, listRelations } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import type { EntityRelation, PageIndexItem, PageType, PageView, Person } from '../types'
import { getGenericOKRBoard, type BoardData } from '../okr/emily/api'
import {
  buildActiveGraph,
  buildFocusGraph,
  connectedIds,
  filterGraph,
  filterGraphByDirection,
  graphCounts,
  graphForNodeIds,
  linkEndpointId,
  nodeKey,
  worldNodeTypeMeta,
  pageTypes,
  primaryComponentIds,
  worldNodeTypes,
  withoutIsolatedNodes,
  type RelationDirection,
  type WorldGraph,
  type WorldLink,
  type WorldNode,
  type WorldNodeType,
} from './graphData'
import { defaultWorldLens, isOKRPluginEnabled, type WorldLens } from './lens'
import { buildOKRGraph, okrWorldPageRefs, type OKRWorldIdentity } from './okrGraph'
import './world-map.css'

type GraphRef = ForceGraphMethods<WorldNode, WorldLink>
type NetworkScope = 'primary' | 'all' | 'focus'
type LabelDensity = 'auto' | 'all' | 'related' | 'hidden'
type CanvasTheme = 'light' | 'dark'
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

async function loadPageRefs(refs: Array<{ type: PageType; id: number }>, cache: Map<string, PageView>, signal: AbortSignal, concurrency = 8): Promise<{ pages: PageView[]; failed: number }> {
  const result = new Array<PageView | undefined>(refs.length)
  let failed = 0
  let cursor = 0
  async function worker() {
    while (!signal.aborted) {
      const current = cursor++
      if (current >= refs.length) return
      const ref = refs[current]
      const key = nodeKey(ref.type, ref.id)
      let page = cache.get(key)
      if (!page) {
        try {
          page = await getPage(ref.type, ref.id, signal)
          cache.set(key, page)
        } catch (cause) {
          if (signal.aborted) throw cause
          failed++
          continue
        }
      }
      result[current] = page
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, refs.length) }, worker))
  return { pages: result.filter((page): page is PageView => Boolean(page)), failed }
}

async function loadAllPersons(signal: AbortSignal): Promise<Person[]> {
  const items: Person[] = []
  for (let page = 1; ; page++) {
    const result = await listPersons(page, 100, signal)
    items.push(...result.items)
    if (items.length >= result.total || result.items.length === 0) return items
  }
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
  const shellRef = useRef<HTMLDivElement>(null)
  const graphRef = useRef<GraphRef | undefined>(undefined)
  const forceSignatureRef = useRef('')
  const framedSignatureRef = useRef('')
  const lensInitializedRef = useRef(false)
  const pageCache = useRef(new Map<string, PageView>())
  const { ref: stageRef, size } = useElementSize<HTMLDivElement>()
  const [activeIndex, setActiveIndex] = useState<PageIndexItem[]>([])
  const [fullIndex, setFullIndex] = useState<PageIndexItem[]>([])
  const [activeGraph, setActiveGraph] = useState<WorldGraph>(emptyGraph)
  const [activePages, setActivePages] = useState<PageView[]>([])
  const [focusGraph, setFocusGraph] = useState<WorldGraph>(emptyGraph)
  const [selectedId, setSelectedId] = useState<string>()
  const [selectedPage, setSelectedPage] = useState<PageView>()
  const [selectedNode, setSelectedNode] = useState<WorldNode>()
  const [lens, setLens] = useState<WorldLens>('world')
  const [okrEnabled, setOKREnabled] = useState(false)
  const [okrBoard, setOKRBoard] = useState<BoardData>()
  const [okrRelations, setOKRRelations] = useState<EntityRelation[]>([])
  const [okrWorldPages, setOKRWorldPages] = useState<PageView[]>([])
  const [okrWorldIdentities, setOKRWorldIdentities] = useState<OKRWorldIdentity[]>([])
  const [okrQuarter, setOKRQuarter] = useState('')
  const [objectiveId, setObjectiveId] = useState('')
  const [scope, setScope] = useState<NetworkScope>('primary')
  const [relationDirection, setRelationDirection] = useState<RelationDirection>('both')
  const [showIsolated, setShowIsolated] = useState(false)
  const [labelDensity, setLabelDensity] = useState<LabelDensity>('auto')
  const [canvasTheme, setCanvasTheme] = useState<CanvasTheme>('light')
  const [fullscreen, setFullscreen] = useState(false)
  const [visibleTypes, setVisibleTypes] = useState<Set<WorldNodeType>>(() => new Set(pageTypes))
  const [query, setQuery] = useState('')
  const [worldLoading, setWorldLoading] = useState(true)
  const [moduleLoading, setModuleLoading] = useState(true)
  const [okrLoading, setOKRLoading] = useState(true)
  const [selecting, setSelecting] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const updateFullscreen = () => setFullscreen(document.fullscreenElement === shellRef.current)
    document.addEventListener('fullscreenchange', updateFullscreen)
    return () => document.removeEventListener('fullscreenchange', updateFullscreen)
  }, [])

  useEffect(() => {
    let controller = new AbortController()
    const reload = () => {
      controller.abort()
      controller = new AbortController()
      setModuleLoading(true)
      listAppModules(controller.signal)
        .then(({ items }) => {
          const enabled = isOKRPluginEnabled(items)
          setOKREnabled(enabled)
          if (!lensInitializedRef.current) {
            const initialLens = defaultWorldLens(items)
            setLens(initialLens)
            setVisibleTypes(new Set(initialLens === 'okr' ? worldNodeTypes : pageTypes))
            setScope(initialLens === 'okr' ? 'all' : 'primary')
            lensInitializedRef.current = true
          } else if (!enabled) {
            setLens('world')
            setVisibleTypes(new Set(pageTypes))
            setScope('primary')
            setObjectiveId('')
            setSelectedId(undefined)
            setSelectedPage(undefined)
            setSelectedNode(undefined)
          }
        })
        .catch((cause: unknown) => {
          if (!controller.signal.aborted) {
            setOKREnabled(false)
            lensInitializedRef.current = true
            setError(errorText(cause))
          }
        })
        .finally(() => { if (!controller.signal.aborted) setModuleLoading(false) })
    }
    reload()
    window.addEventListener('jarvis:app-modules-changed', reload)
    return () => {
      controller.abort()
      window.removeEventListener('jarvis:app-modules-changed', reload)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    setWorldLoading(true)
    listPages(false, controller.signal)
      .then(async (active) => {
        setActiveIndex(active)
        setFullIndex(active)
        const pages = await loadPages(active, controller.signal)
        pages.forEach((page) => pageCache.current.set(nodeKey(page.type, page.id), page))
        setActivePages(pages)
        setActiveGraph(buildActiveGraph(pages, active))
        setError(undefined)
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setWorldLoading(false) })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    const needle = query.trim()
    if (lens !== 'world') return
    if (!needle) {
      setFullIndex(activeIndex)
      return
    }
    const controller = new AbortController()
    const timer = window.setTimeout(() => {
      listPages(true, controller.signal, needle)
        .then((matches) => {
          if (controller.signal.aborted) return
          const merged = new Map(activeIndex.map((item) => [nodeKey(item.type, item.id), item]))
          matches.forEach((item) => merged.set(nodeKey(item.type, item.id), item))
          setFullIndex([...merged.values()])
        })
        .catch((cause: unknown) => {
          if (!controller.signal.aborted) setError(errorText(cause))
        })
    }, 180)
    return () => {
      window.clearTimeout(timer)
      controller.abort()
    }
  }, [activeIndex, lens, query])

  useEffect(() => {
    if (lens !== 'okr' || !okrEnabled) return
    const controller = new AbortController()
    setOKRLoading(true)
    setOKRBoard(undefined)
    setOKRRelations([])
    setOKRWorldPages([])
    setOKRWorldIdentities([])
    Promise.all([
      getGenericOKRBoard(okrQuarter, controller.signal),
      listRelations({ nodeTypes: ['okr_objective', 'okr_kr', 'okr_point'] }, controller.signal),
      getProfile(controller.signal),
      loadAllPersons(controller.signal),
    ]).then(async ([board, relationResult, profile, people]) => {
      const identities: OKRWorldIdentity[] = [
        ...(profile.saved && profile.id > 0 ? [{ pageType: 'principal' as const, pageId: profile.id }] : []),
        ...people.map((person) => ({ unionId: person.union_id ?? undefined, pageType: 'person' as const, pageId: person.id })),
      ]
      const loaded = await loadPageRefs(okrWorldPageRefs(board.objectives, relationResult.items, identities), pageCache.current, controller.signal)
      if (controller.signal.aborted) return
      setOKRBoard(board)
      setOKRRelations(relationResult.items)
      setOKRWorldPages(loaded.pages)
      setOKRWorldIdentities(identities)
      setError(loaded.failed > 0 ? `${loaded.failed} 个关联的现实实体暂时无法读取；OKR 原生结构仍完整展示。` : undefined)
    }).catch((cause: unknown) => {
      if (!controller.signal.aborted) setError(errorText(cause))
    }).finally(() => { if (!controller.signal.aborted) setOKRLoading(false) })
    return () => controller.abort()
  }, [lens, okrEnabled, okrQuarter])

  const currentOKRBoard = !okrQuarter || okrBoard?.quarter === okrQuarter ? okrBoard : undefined

  const okrPages = useMemo(() => {
    const pages = new Map(activePages.map((page) => [nodeKey(page.type, page.id), page]))
    okrWorldPages.forEach((page) => pages.set(nodeKey(page.type, page.id), page))
    return [...pages.values()]
  }, [activePages, okrWorldPages])
  const okrIndex = useMemo(() => {
    const items = new Map(fullIndex.map((item) => [nodeKey(item.type, item.id), item]))
    okrWorldPages.forEach((page) => items.set(nodeKey(page.type, page.id), {
      type: page.type, id: page.id, name: page.name, index_line: null, char_count: page.char_count, last_progress_at: page.updated_at,
    }))
    return [...items.values()]
  }, [fullIndex, okrWorldPages])

  const okrGraph = useMemo(() => buildOKRGraph({
    objectives: currentOKRBoard?.objectives ?? [],
    relations: okrRelations,
    activePages: okrPages,
    fullIndex: okrIndex,
    identities: okrWorldIdentities,
    objectiveId: objectiveId || undefined,
  }), [currentOKRBoard, objectiveId, okrIndex, okrPages, okrRelations, okrWorldIdentities])
  const loading = worldLoading || moduleLoading || lens === 'okr' && okrLoading

  const activePrimaryIds = useMemo(() => primaryComponentIds(activeGraph), [activeGraph])
  const rawGraph = useMemo(() => {
    if (lens === 'okr') return okrGraph
    if (scope === 'focus') return focusGraph
    if (scope === 'primary') return graphForNodeIds(activeGraph, activePrimaryIds)
    return activeGraph
  }, [activeGraph, activePrimaryIds, focusGraph, lens, okrGraph, scope])
  const graph = useMemo(() => {
    const byType = filterGraph(rawGraph, visibleTypes, selectedId)
    const byDirection = (scope === 'focus' || lens === 'okr' && selectedId) ? filterGraphByDirection(byType, selectedId, relationDirection) : byType
    return lens === 'world' && scope === 'all' && !showIsolated ? withoutIsolatedNodes(byDirection, selectedId) : byDirection
  }, [lens, rawGraph, relationDirection, scope, selectedId, showIsolated, visibleTypes])
  const connections = useMemo(() => connectedIds(graph, selectedId), [graph, selectedId])
  const counts = useMemo(() => graphCounts(graph), [graph])

  const searchResults = useMemo(() => {
    const needle = query.trim().toLocaleLowerCase()
    if (!needle) return []
    if (lens === 'okr') return okrGraph.nodes.filter((item) => `${item.name} ${item.indexLine}`.toLocaleLowerCase().includes(needle)).slice(0, 12)
    return fullIndex
      .filter((item) => `${item.name} ${item.index_line || ''}`.toLocaleLowerCase().includes(needle))
      .slice(0, 12)
  }, [fullIndex, lens, okrGraph.nodes, query])

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
      setSelectedNode(undefined)
      setFocusGraph(buildFocusGraph(page, activeIndex, fullIndex))
      if (lens === 'world' && (switchToFocus || !activeGraph.nodes.some((node) => node.id === key))) setScope('focus')
      setQuery('')
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSelecting(false)
    }
  }, [activeGraph.nodes, activeIndex, fullIndex, lens])

  const selectGraphNode = useCallback((node: WorldNode) => {
    if (node.pageType && node.pageId) {
      void selectEntity(node.pageType, node.pageId, lens === 'world')
      return
    }
    setSelectedId(node.id)
    setSelectedNode(node)
    setSelectedPage(undefined)
    setQuery('')
  }, [lens, selectEntity])

  useEffect(() => {
    if (!selectedPage) return
    setFocusGraph(buildFocusGraph(selectedPage, activeIndex, fullIndex))
  }, [activeIndex, fullIndex, selectedPage])

  const clearSelection = useCallback(() => {
    setSelectedId(undefined)
    setSelectedPage(undefined)
    setSelectedNode(undefined)
    setScope((current) => current === 'focus' ? 'primary' : current)
    setQuery('')
  }, [])

  const resetView = useCallback(() => {
    setVisibleTypes(new Set(lens === 'world' ? pageTypes : worldNodeTypes))
    setScope(lens === 'world' ? 'primary' : 'all')
    setRelationDirection('both')
    setShowIsolated(false)
    setLabelDensity('auto')
    setCanvasTheme('light')
    if (lens === 'okr') setObjectiveId('')
    clearSelection()
  }, [clearSelection, lens])

  const changeLens = (nextLens: WorldLens) => {
    if (nextLens === 'okr' && !okrEnabled) return
    setLens(nextLens)
    setVisibleTypes(new Set(nextLens === 'world' ? pageTypes : worldNodeTypes))
    setScope(nextLens === 'world' ? 'primary' : 'all')
    setObjectiveId('')
    clearSelection()
  }

  const changeOKRQuarter = (nextQuarter: string) => {
    setOKRQuarter(nextQuarter)
    setObjectiveId('')
    clearSelection()
  }

  const frameGraph = useCallback((targetIds?: Set<string>, duration = 680) => {
    window.requestAnimationFrame(() => {
      if (!graphRef.current || !graph.nodes.length) return
      if (targetIds?.size === 1) {
        const target = graph.nodes.find((node) => targetIds.has(node.id))
        if (target && Number.isFinite(target.x) && Number.isFinite(target.y)) {
          graphRef.current.centerAt(target.x, target.y, duration)
          graphRef.current.zoom(2.4, duration)
        }
        return
      }
      const filter = targetIds?.size ? (candidate: NodeObject<WorldNode>) => targetIds.has(String(candidate.id)) : undefined
      graphRef.current.zoomToFit(duration, targetIds ? 92 : 72, filter)
    })
  }, [graph.nodes])

  const changeZoom = useCallback((factor: number) => {
    const instance = graphRef.current
    if (!instance) return
    instance.zoom(Math.min(12, Math.max(.22, instance.zoom() * factor)), 220)
  }, [])

  const focusSelection = useCallback(() => {
    if (!selectedId) return
    frameGraph(connections.size > 1 ? connections : new Set([selectedId]), 480)
  }, [connections, frameGraph, selectedId])

  const toggleFullscreen = useCallback(async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen()
      else if (shellRef.current?.requestFullscreen) await shellRef.current.requestFullscreen()
      else throw new Error('当前浏览器不支持全屏显示')
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }, [])

  useEffect(() => {
    if (!graph.nodes.length) return
    framedSignatureRef.current = ''
    const targetIds = selectedId && connections.size > 1 ? connections : undefined
    const timer = window.setTimeout(() => frameGraph(targetIds, 520), scope === 'focus' ? 40 : 720)
    return () => window.clearTimeout(timer)
  }, [connections, frameGraph, graph.nodes, scope, selectedId])

  const configureForces = useCallback(() => {
    const instance = graphRef.current
    if (!instance || scope === 'focus') return
    const signature = `${lens}:${scope}:${graph.nodes.length}`
    if (forceSignatureRef.current === signature) return
    const linkForce = instance.d3Force('link') as { distance?: (value: number) => unknown } | undefined
    const chargeForce = instance.d3Force('charge') as { strength?: (value: number) => unknown } | undefined
    linkForce?.distance?.(lens === 'okr' ? 54 : 82)
    chargeForce?.strength?.(lens === 'okr' ? -105 : -180)
    instance.d3ReheatSimulation()
    forceSignatureRef.current = signature
  }, [graph.nodes.length, lens, scope])

  const handleEngineStop = useCallback(() => {
    const signature = `${lens}:${scope}:${graph.nodes.map((node) => node.id).join('|')}`
    if (framedSignatureRef.current === signature) return
    framedSignatureRef.current = signature
    frameGraph(undefined, 460)
  }, [frameGraph, graph.nodes, lens, scope])

  const drawNode = useCallback((candidate: NodeObject<WorldNode>, context: CanvasRenderingContext2D, scale: number) => {
    const node = candidate as WorldNode
    if (!Number.isFinite(node.x) || !Number.isFinite(node.y)) return
    const selected = node.id === selectedId
    const connected = !selectedId || connections.has(node.id)
    const meta = worldNodeTypeMeta[node.nodeType]
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
      context.strokeStyle = selected ? canvasTheme === 'dark' ? '#f7faf8' : '#18211f' : canvasTheme === 'dark' ? '#171b19' : '#ffffff'
    context.stroke()

    const important = node.nodeType === 'principal' || node.nodeType === 'project' || node.nodeType === 'okr_objective'
    const automatic = selected || (selectedId ? connected : important || scale > 2.15)
    const related = selectedId ? connected : important
    const showLabel = labelDensity === 'all' || labelDensity === 'auto' && automatic || labelDensity === 'related' && related
    if (showLabel) {
      const fontSize = (selected ? 14 : 12) / scale
      const label = compactLabel(node.name, selected ? 24 : 18)
      context.font = `${selected ? 650 : 520} ${fontSize}px Inter, "PingFang SC", sans-serif`
      context.textAlign = 'center'
      context.textBaseline = 'top'
      context.fillStyle = connected ? canvasTheme === 'dark' ? '#edf2ef' : '#26302d' : canvasTheme === 'dark' ? '#68716d' : '#9aa19e'
      context.fillText(label, node.x!, node.y! + radius + 4 / scale)
    }
    context.restore()
  }, [canvasTheme, connections, labelDensity, selectedId])

  const paintNodeArea = useCallback((candidate: NodeObject<WorldNode>, color: string, context: CanvasRenderingContext2D) => {
    const node = candidate as WorldNode
    if (!Number.isFinite(node.x) || !Number.isFinite(node.y)) return
    context.beginPath()
    context.arc(node.x!, node.y!, Math.max(7, node.size + 3), 0, Math.PI * 2)
    context.fillStyle = color
    context.fill()
  }, [])

  const toggleType = (type: WorldNodeType) => {
    setVisibleTypes((current) => {
      const next = new Set(current)
      if (next.has(type) && next.size > 1) next.delete(type)
      else next.add(type)
      return next
    })
  }

  const selectedRelations = useMemo(() => {
    if (!selectedId) return []
    const relations: Array<WorldLink & { neighborId: string; direction: 'outgoing' | 'incoming' }> = []
    for (const link of graph.links) {
      const source = linkEndpointId(link.source)
      const target = linkEndpointId(link.target)
      if (source === selectedId) relations.push({ ...link, neighborId: target, direction: 'outgoing' })
      if (target === selectedId) relations.push({ ...link, neighborId: source, direction: 'incoming' })
    }
    return relationDirection === 'both' ? relations : relations.filter((relation) => relation.direction === relationDirection)
  }, [graph.links, relationDirection, selectedId])
  const inspectedNode = useMemo(() => selectedId ? graph.nodes.find((node) => node.id === selectedId) ?? selectedNode : undefined, [graph.nodes, selectedId, selectedNode])

  const viewOptions = (
    <div className="world-map-options">
      <div className="world-map-options-head"><strong>视图选项</strong><span>仅影响当前浏览</span></div>
      <label className={!selectedId || lens === 'world' && scope !== 'focus' ? 'is-disabled' : ''}>
        <span><b>关系方向</b><small>在一跳范围中生效</small></span>
        <Segmented<RelationDirection>
          block
          size="small"
          disabled={!selectedId || lens === 'world' && scope !== 'focus'}
          value={relationDirection}
          onChange={setRelationDirection}
          options={[{ label: '双向', value: 'both' }, { label: '向外', value: 'outgoing' }, { label: '向内', value: 'incoming' }]}
        />
      </label>
      <label className={lens === 'okr' ? 'is-disabled' : ''}>
        <span><b>孤立节点</b><small>在全局范围中生效</small></span>
        <Switch disabled={lens === 'okr'} checkedChildren="显示" unCheckedChildren="隐藏" checked={showIsolated} onChange={setShowIsolated} />
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
    <div className="world-map-shell" ref={shellRef}>
      <div className="world-map-toolbar">
        <div className="world-map-title-block">
          <strong>世界地图</strong>
          <span>{lens === 'world' ? '现实实体页之间的显式引用' : 'OKR 原生结构与现实承接关系'}</span>
        </div>
        <div className="world-map-actions">
          <Segmented<WorldLens> value={lens} onChange={changeLens} options={[
            { label: '现实世界', value: 'world' },
            ...(okrEnabled ? [{ label: 'OKR 全景' as const, value: 'okr' as const }] : []),
          ]} />
          {lens === 'world' ? (
            <Segmented<NetworkScope>
              value={scope}
              onChange={setScope}
              options={[{ label: '主网络', value: 'primary' }, { label: '全局', value: 'all' }, { label: '一跳', value: 'focus', disabled: !selectedPage }]}
            />
          ) : (
            <>
              <Select
                aria-label="选择 OKR 季度"
                value={okrQuarter || okrBoard?.quarter}
                loading={loading}
                options={(okrBoard?.availableQuarters ?? []).map((value) => ({ value, label: value.replace('-', ' ') }))}
                onChange={changeOKRQuarter}
                style={{ width: 110 }}
              />
              <Select
                aria-label="选择 Objective"
                value={objectiveId || undefined}
                allowClear
                placeholder="整季度概览"
                options={(okrBoard?.objectives ?? []).map((objective) => ({ value: objective.id, label: objective.title }))}
                onChange={(value) => { setObjectiveId(value ?? ''); clearSelection() }}
                style={{ width: 220 }}
              />
            </>
          )}
          <Popover trigger="click" placement="bottomRight" content={viewOptions}><Button icon={<SettingOutlined />}>视图选项</Button></Popover>
          <Segmented<CanvasTheme>
            className="world-map-theme-switch"
            value={canvasTheme}
            onChange={setCanvasTheme}
            options={[{ label: '白色', value: 'light' }, { label: '黑色', value: 'dark' }]}
          />
          <Space.Compact className="world-map-zoom-controls">
            <Tooltip title="缩小"><Button aria-label="缩小关系图" icon={<MinusOutlined />} onClick={() => changeZoom(0.72)} /></Tooltip>
            <Tooltip title="放大"><Button aria-label="放大关系图" icon={<PlusOutlined />} onClick={() => changeZoom(1.38)} /></Tooltip>
            <Button icon={<AimOutlined />} disabled={!selectedId} onClick={focusSelection}>聚焦</Button>
            <Button icon={<CompressOutlined />} onClick={() => frameGraph()}>适应</Button>
          </Space.Compact>
          <Button icon={fullscreen ? <FullscreenExitOutlined /> : <FullscreenOutlined />} onClick={() => void toggleFullscreen()}>{fullscreen ? '退出全屏' : '全屏'}</Button>
          <Button onClick={resetView}>重置</Button>
        </div>
      </div>

      {error && <Alert type="error" showIcon closable title="关系地图加载失败" description={error} onClose={() => setError(undefined)} />}

      <div className={`world-map-layout${canvasTheme === 'dark' ? ' is-dark' : ''}`}>
        <section className="world-map-canvas-card" aria-label="世界模型关系图">
          <div className="world-map-search">
            <Input
              allowClear
              value={query}
              prefix={<SearchOutlined />}
              placeholder={lens === 'okr' ? `搜索 ${okrGraph.nodes.length.toLocaleString()} 个 OKR 与现实实体` : '搜索全部人、事、群和资源'}
              onChange={(event) => setQuery(event.target.value)}
              aria-label="搜索实体"
            />
            {query.trim() && (
              <div className="world-map-search-results">
                {searchResults.length === 0 ? <span className="world-map-search-empty">没有匹配的实体</span> : searchResults.map((item) => (
                  'nodeType' in item ? (
                    <button key={item.id} type="button" onClick={() => selectGraphNode(item)}>
                      <i style={{ background: worldNodeTypeMeta[item.nodeType].color }} />
                      <span><b>{item.name}</b><small>{worldNodeTypeMeta[item.nodeType].label} · {item.indexLine || '暂无摘要'}</small></span>
                    </button>
                  ) : (
                    <button key={nodeKey(item.type, item.id)} type="button" onClick={() => void selectEntity(item.type, item.id, true)}>
                      <i style={{ background: worldNodeTypeMeta[item.type].color }} />
                      <span><b>{item.name}</b><small>{worldNodeTypeMeta[item.type].label} · {item.index_line || '暂无摘要'}</small></span>
                    </button>
                  )
                ))}
              </div>
            )}
          </div>

          <div className="world-map-legend" aria-label="实体类型筛选">
            {(lens === 'world' ? pageTypes : worldNodeTypes).map((type) => (
              <button key={type} type="button" className={visibleTypes.has(type) ? 'is-active' : ''} aria-pressed={visibleTypes.has(type)} onClick={() => toggleType(type)}>
                <i style={{ background: worldNodeTypeMeta[type].color }} />{worldNodeTypeMeta[type].label}
              </button>
            ))}
          </div>

          <div className="world-map-stats">
            <span><b>{counts.nodes}</b>实体</span>
            <span><b>{counts.links}</b>关系</span>
            {lens === 'world' && <span><b>{counts.facts.toLocaleString()}</b>事实</span>}
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
                key={`${lens}:${scope}`}
                ref={graphRef}
                width={size.width}
                height={size.height}
                graphData={graph}
                backgroundColor={canvasTheme === 'dark' ? '#101310' : '#fbfaf7'}
                nodeCanvasObject={drawNode}
                nodePointerAreaPaint={paintNodeArea}
                nodeLabel={(node) => `${node.name} · ${worldNodeTypeMeta[node.nodeType].label}`}
                linkLabel={(link) => link.label}
                linkColor={(link) => link.strength === 'strong' ? '#e56f50' : link.strength === 'owner' ? '#6385d2' : selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? canvasTheme === 'dark' ? '#c4cec9' : '#56625e' : selectedId ? canvasTheme === 'dark' ? '#303733' : '#d9ddda' : canvasTheme === 'dark' ? '#4b5550' : '#b9c0bd'}
                linkWidth={(link) => link.strength === 'strong' ? 2.1 : selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? 1.4 : 0.65}
                linkLineDash={(link) => link.strength === 'reference' ? [3, 4] : null}
                linkDirectionalArrowLength={(link) => selectedId && (linkEndpointId(link.source) === selectedId || linkEndpointId(link.target) === selectedId) ? 3.2 : 0}
                linkDirectionalArrowRelPos={0.82}
                linkDirectionalArrowColor={() => canvasTheme === 'dark' ? '#c8d1cd' : '#68736f'}
                d3AlphaDecay={0.035}
                d3VelocityDecay={0.34}
                cooldownTicks={scope === 'focus' ? 0 : 220}
                minZoom={0.22}
                maxZoom={12}
                onEngineTick={configureForces}
                onEngineStop={handleEngineStop}
                onNodeClick={selectGraphNode}
                onBackgroundClick={clearSelection}
              />
            )}
          </div>
          <div className="world-map-hint">拖动画布 · 滚轮缩放 · 点击节点查看关系</div>
        </section>

        <aside className="world-map-inspector">
          {!inspectedNode || !selectedId ? (
            <div className="world-map-inspector-empty">
              <span className="world-map-empty-mark"><i /><i /><i /></span>
              <strong>从一个实体开始</strong>
              <p>点击节点，或搜索完整索引。选中后会突出它与直接关联实体的关系。</p>
            </div>
          ) : (
            <>
              <div className="world-map-detail-head">
                <span className="world-map-detail-dot" style={{ background: worldNodeTypeMeta[inspectedNode.nodeType].color }} />
                <div><Tag color={worldNodeTypeMeta[inspectedNode.nodeType].color}>{worldNodeTypeMeta[inspectedNode.nodeType].label}</Tag><h2>{inspectedNode.name}</h2></div>
              </div>
              <p className="world-map-index-line">{inspectedNode.indexLine || '暂无索引摘要'}</p>
              <div className="world-map-detail-metrics">
                <span><b>{inspectedNode.factCount ?? '—'}</b>长期事实</span>
                <span><b>{selectedRelations.filter((item) => item.direction === 'outgoing').length}</b>向外关系</span>
                <span><b>{selectedRelations.filter((item) => item.direction === 'incoming').length}</b>向内关系</span>
              </div>
              {lens === 'world' && selectedPage && <Button type="primary" block disabled={scope === 'focus'} onClick={() => setScope('focus')}>只看一跳关系</Button>}
              {lens === 'okr' && inspectedNode.nodeType === 'okr_objective' && objectiveId !== inspectedNode.entityId && (
                <Button type="primary" block onClick={() => { setObjectiveId(inspectedNode.entityId); clearSelection() }}>聚焦这个 Objective</Button>
              )}

              <div className="world-map-section">
                <h3>{selectedPage ? '实体摘要' : '数据来源'}</h3>
                {selectedPage?.summary ? <MarkdownReport content={selectedPage.summary} className="world-map-summary" /> : (
                  <span className="world-map-muted">{selectedPage ? '世界实体页暂无摘要。' : inspectedNode.nodeType.startsWith('okr_') ? `来自 OKR 插件，稳定引用 ${inspectedNode.id}` : `来自 EntityRelation，稳定引用 ${inspectedNode.id}`}</span>
                )}
              </div>
              <div className="world-map-section">
                <h3>直接关系 <small>{selectedRelations.length}</small></h3>
                <div className="world-map-relations">
                  {selectedRelations.length === 0 && <span className="world-map-muted">暂无直接关系</span>}
                  {selectedRelations.map((relation) => (
                    <button key={`${relation.direction}:${relation.id}`} type="button" onClick={() => { const neighbor = graph.nodes.find((node) => node.id === relation.neighborId); if (neighbor) selectGraphNode(neighbor) }}>
                      <i style={{ background: worldNodeTypeMeta[graph.nodes.find((node) => node.id === relation.neighborId)?.nodeType ?? 'external'].color }} />
                      <span><small>{relation.direction === 'outgoing' ? `${relation.label} →` : `← ${relation.label}`}</small>{graph.nodes.find((node) => node.id === relation.neighborId)?.name ?? relation.neighborId}</span>
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
