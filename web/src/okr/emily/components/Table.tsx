import { useMemo, useState } from 'react'
import { getMeegoPreview } from '../api'
import { useBoard } from '../board'
import { buildBusinessNavigation, businessKrCount, krCount } from '../hierarchy'
import { TAG_TYPE_LABEL, TAG_VALUE_LABEL } from '../labels'
import { hasOwner, joinOwnerNames, splitOwnerNames } from '../people'
import { KINDS } from '../rows'
import { KIND_LABEL, isDone, statusOf } from '../template'
import type { Entry, Kr, KrTag, MeegoPreview, Objective, Point, PointKind } from '../types'
import { HierarchyNav } from './HierarchyNav'
import { Images, LightPicker, Links, StatusSelect, Text } from './ui'

function Caret({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      title={open ? '收起' : '展开'}
      aria-label={open ? '折叠当前层级' : '展开当前层级'}
      aria-expanded={open}
      className="mt-0.5 shrink-0 rounded p-0.5 text-slate-400 hover:bg-slate-100 hover:text-slate-700"
    >
      <svg viewBox="0 0 12 12" aria-hidden className={`size-3 transition-transform ${open ? 'rotate-90' : ''}`}>
        <path d="M4 2.2 L8.8 6 L4 9.8 Z" fill="currentColor" />
      </svg>
    </button>
  )
}

function Empty({ children }: { children: React.ReactNode }) {
  return <div className="rounded-lg border border-dashed border-slate-200 px-3 py-5 text-center text-xs text-slate-400">{children}</div>
}

function priorityTone(priority?: Kr['priority']) {
  if (priority === 'p0') return 'border-red-200 bg-red-50 font-semibold text-red-700'
  if (priority === 'p1') return 'border-amber-200 bg-amber-50 font-semibold text-amber-700'
  return 'border-slate-200 bg-white text-slate-500'
}

function PersonPicker({ kr }: { kr: Kr }) {
  const { objectives, setKrOwner } = useBoard()
  const [draft, setDraft] = useState('')
  const people = splitOwnerNames(kr.ownerName)
  const options = useMemo(() => [...new Set(objectives.flatMap((objective) => objective.krs.flatMap((item) => splitOwnerNames(item.ownerName))))].filter((item) => !people.includes(item)).sort(), [objectives, people])
  const commit = (value: string) => {
    const clean = value.trim()
    if (!clean) return
    setKrOwner(kr.id, joinOwnerNames([...people, clean]))
    setDraft('')
  }

  return (
    <div className="flex min-h-7 flex-wrap items-center gap-1.5">
      {people.map((person) => (
        <span key={person} className="group/person inline-flex items-center gap-1 rounded-full border border-slate-200 bg-slate-50 py-0.5 pr-1.5 pl-1 text-[11px] text-slate-600">
          <span className="flex size-4 items-center justify-center rounded-full bg-slate-300 text-[9px] font-semibold text-white">{person.slice(0, 1)}</span>
          {person}
          <button type="button" onClick={() => setKrOwner(kr.id, joinOwnerNames(people.filter((item) => item !== person)))} className="text-slate-300 hover:text-red-500" title="移除人员">×</button>
        </span>
      ))}
      <input
        list={`owner-options-${kr.id}`}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        onBlur={() => commit(draft)}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === '、' || event.key === ',') {
            event.preventDefault()
            commit(draft)
          }
        }}
        placeholder={people.length === 0 ? '添加人员' : '+ 人员'}
        className="h-6 w-20 rounded border border-dashed border-slate-200 bg-white px-1.5 text-[11px] text-slate-500 outline-none placeholder:text-slate-300 focus:w-28 focus:border-blue-300"
      />
      <datalist id={`owner-options-${kr.id}`}>{options.map((person) => <option key={person} value={person} />)}</datalist>
    </div>
  )
}

function EntryRow({ pointId, entry, readOnly }: { pointId: string; entry: Entry; readOnly: boolean }) {
  const { patchEntry, removeEntry } = useBoard()

  return (
    <div className="group/entry flex items-start gap-2.5 rounded-lg border border-slate-200/80 bg-slate-50/70 px-3 py-2.5">
      <span className="pt-0.5">
        <StatusSelect value={entry.status} onChange={(status) => patchEntry(pointId, entry.id, { status })} readOnly={readOnly} />
      </span>
      <div className="min-w-0 flex-1">
        <Text
          value={entry.text}
          onChange={(text) => patchEntry(pointId, entry.id, { text })}
          placeholder="可衡量的本周进展；无更新请写明预期更新时间"
          className="text-[14px] leading-[22px] text-slate-700"
          readOnly={readOnly}
          commentTarget={{ type: 'entry', id: entry.id, title: entry.text }}
        />
        <div className="mt-1 flex flex-wrap items-center gap-1.5">
          <Links value={entry.docs} onChange={(docs) => patchEntry(pointId, entry.id, { docs })} readOnly={readOnly} />
          <Images value={entry.images} onChange={(images) => patchEntry(pointId, entry.id, { images })} readOnly={readOnly} />
        </div>
      </div>
      {!readOnly && (
        <button
          type="button"
          onClick={() => removeEntry(pointId, entry.id)}
          title="删除这条进展"
          className="text-slate-300 opacity-0 transition-opacity group-hover/entry:opacity-100 hover:text-red-500"
        >
          ×
        </button>
      )}
    </div>
  )
}

function EntryList({ point, done, readOnly }: { point: Point; done: boolean; readOnly: boolean }) {
  const { addEntry } = useBoard()
  const list = point.entries.filter((entry) => isDone(entry.status) === done)

  return (
    <div className="space-y-1.5">
      {list.map((entry) => <EntryRow key={entry.id} pointId={point.id} entry={entry} readOnly={readOnly} />)}
      {list.length === 0 && (
        <div className="rounded-lg border border-dashed border-slate-200 px-3 py-4 text-xs text-slate-400">
          {done ? '状态改为「已完成」的条目会自动移到这里' : '暂无进展'}
        </div>
      )}
      {!readOnly && !done && (
        <button type="button" onClick={() => addEntry(point.id)} className="text-xs text-slate-400 hover:text-blue-600">
          + 一条进展
        </button>
      )}
    </div>
  )
}

function HistoryPreview({ point }: { point: Point }) {
  const { previousWeek } = useBoard()
  const previous = point.previousEntries ?? []
  const currentKey = point.entries.map((entry) => `${entry.status}:${entry.text}`).join('\n')
  const previousKey = previous.map((entry) => `${entry.status}:${entry.text}`).join('\n')
  if (!previousWeek || previous.length === 0 || currentKey === previousKey) return null

  return (
    <details className="mt-3 border-t border-dashed border-slate-200 pt-2 text-xs text-slate-400">
      <summary className="cursor-pointer select-none hover:text-slate-600">较 {previousWeek} 有更新 · 查看上次</summary>
      <div className="mt-2 space-y-1 rounded-lg bg-slate-50 px-3 py-2">
        {previous.map((entry) => (
          <div key={entry.id}><span className="mr-2 text-slate-500">{statusOf(entry.status)?.label}</span>{entry.text}</div>
        ))}
      </div>
    </details>
  )
}

function MetricBox({ kr, readOnly }: { kr: Kr; readOnly: boolean }) {
  const { setMetricNote, patchMetric, addMetric, removeMetric } = useBoard()

  return (
    <section className="rounded-xl border border-blue-100 bg-blue-50/55 p-2.5">
      <div className="mb-2 flex items-center gap-3">
        <h3 className="border-l-[3px] border-blue-500 pl-2 text-[12px] font-semibold text-blue-700">核心数据</h3>
        {kr.metricNote && (
          <span className="min-w-0 flex-1 text-right text-[11px] text-slate-400">
            <Text value={kr.metricNote} onChange={(value) => setMetricNote(kr.id, value)} className="text-right text-[11px] text-slate-400" readOnly={readOnly} />
          </span>
        )}
      </div>
      <div className="group/metrics overflow-hidden rounded-lg border border-blue-100 bg-white/90">
        {kr.metrics.map((metric) => (
          <div key={metric.id} className="group/metric flex min-h-12 items-start gap-3 border-b border-slate-100 px-4 py-2.5">
            <div className="min-w-0 flex-1">
              <div className="flex items-start gap-3">
                <Text
                  fit
                  value={metric.text}
                  onChange={(text) => patchMetric(kr.id, metric.id, { text })}
                  placeholder="例：Q3 累计自然入驻 1,253 家，线索到入驻转化率 16.51%"
                  className="text-[16px] leading-6 tracking-[0.005em] text-slate-800"
                  readOnly={readOnly}
                  commentTarget={{ type: 'metric', id: metric.id, title: metric.text }}
                />
                <span className="pt-1 [&>span>button]:size-4 [&>span>span]:size-4">
                  <LightPicker value={metric.light ?? 'green'} onChange={(light) => patchMetric(kr.id, metric.id, { light })} readOnly={readOnly} />
                </span>
              </div>
              {(metric.images?.length ?? 0) > 0 && (
                <div className="mt-2">
                  <Images value={metric.images ?? []} onChange={(images) => patchMetric(kr.id, metric.id, { images })} readOnly={readOnly} />
                </div>
              )}
            </div>
            {!readOnly && (
              <button
                type="button"
                onClick={() => removeMetric(kr.id, metric.id)}
                title="删除这一条"
                className="ml-auto text-slate-300 opacity-0 transition-opacity group-hover/metric:opacity-100 hover:text-red-500"
              >
                ×
              </button>
            )}
          </div>
        ))}
        {kr.metrics.length === 0 && (
          <button
            type="button"
            disabled={readOnly}
            onClick={() => addMetric(kr.id)}
            className="flex min-h-12 w-full items-center gap-3 border-b border-slate-100 px-4 py-2.5 text-left text-[16px] leading-6 tracking-[0.005em] text-slate-400 last:border-b-0 enabled:hover:bg-slate-50 enabled:hover:text-slate-500"
          >
            <span>例：Q3 累计自然入驻 1,253 家，线索到入驻转化率 16.51%</span>
            <span title="默认绿灯" className="size-4 shrink-0 rounded-full bg-emerald-500 ring-4 ring-emerald-50" />
          </button>
        )}
        {!readOnly && kr.metrics.length > 0 && (
          <button type="button" onClick={() => addMetric(kr.id)} className="w-full border-t border-slate-100 px-4 py-2 text-left text-xs text-slate-400 hover:bg-slate-50 hover:text-blue-600">+ 一条核心数据</button>
        )}
      </div>
    </section>
  )
}

function tagText(tag: KrTag) {
  if (tag.type === 'management_focus' || tag.type === 'biweekly' || tag.type === 'platform_report') return TAG_TYPE_LABEL[tag.type]
  return TAG_VALUE_LABEL[tag.value] ?? tag.value
}

function tagClass(tag: KrTag) {
  if (tag.type === 'management_focus') return 'border-amber-200 bg-amber-50 text-amber-700'
  if (tag.type === 'region') return 'border-violet-200 bg-violet-50 text-violet-700'
  return 'border-slate-200 bg-slate-100 text-slate-600'
}

function KrHeader({ objectiveId, kr, open, onToggle, readOnly }: { objectiveId: string; kr: Kr; open: boolean; onToggle: () => void; readOnly: boolean }) {
  const { setKrTitle, setKrPriority } = useBoard()

  return (
    <header className="group/kr border-b border-slate-100 px-3.5 py-2.5">
      <div className="flex items-start gap-2">
        <Caret open={open} onToggle={onToggle} />
        <div className="min-w-0 flex-1">
          <Text value={kr.title} onChange={(value) => setKrTitle(objectiveId, kr.id, value)} placeholder="KR 标题" className="text-[14px] font-semibold leading-5 text-slate-900" readOnly={readOnly} commentTarget={{ type: 'kr', id: kr.id, title: kr.title }} />
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5 pl-1">
            {!readOnly && <PersonPicker kr={kr} />}
            {readOnly && splitOwnerNames(kr.ownerName).map((person) => <span key={person} className="rounded-full border border-slate-200 bg-slate-50 px-2 py-1 text-[11px] text-slate-600">{person}</span>)}
            {!readOnly ? (
              <select value={kr.priority ?? 'p1'} onChange={(event) => setKrPriority(kr.id, event.target.value as NonNullable<Kr['priority']>)} className={`rounded-md border px-2 py-1 text-xs outline-none focus:border-blue-400 ${priorityTone(kr.priority)}`}>
                <option value="p0">P0</option><option value="p1">P1</option><option value="p2">P2</option>
              </select>
            ) : kr.priority && <span className={`rounded-md border px-2 py-1 text-xs uppercase ${priorityTone(kr.priority)}`}>{kr.priority}</span>}
            {(kr.tags ?? []).filter((tag) => tag.type !== 'custom').map((tag) => (
              <span key={`${tag.type}:${tag.value}`} className={`rounded-md border px-2 py-1 text-xs ${tagClass(tag)}`}>{tagText(tag)}</span>
            ))}
            {kr.metrics.length > 0 && <span className="inline-flex items-center gap-1" title="核心数据红黄绿灯">{kr.metrics.map((metric) => <i key={metric.id} className={`size-2 rounded-full ${metric.light === 'red' ? 'bg-red-500' : metric.light === 'yellow' ? 'bg-amber-400' : 'bg-emerald-500'}`} />)}</span>}
          </div>
        </div>
      </div>
    </header>
  )
}

function PointHeader({ objectiveId, krId, point, index, open, onToggle, readOnly, showProgress = true }: { objectiveId: string; krId: string; point: Point; index: number; open: boolean; onToggle: () => void; readOnly: boolean; showProgress?: boolean }) {
  const { setPointTitle, setPointMeegoLink, removePoint, week } = useBoard()
  const [preview, setPreview] = useState<MeegoPreview>()
  const [previewError, setPreviewError] = useState('')
  const [previewing, setPreviewing] = useState(false)
  const [editingMeego, setEditingMeego] = useState(Boolean(point.meegoWorkItemId))
  const doing = point.entries.filter((entry) => !isDone(entry.status)).length
  const done = point.entries.length - doing

  const loadPreview = async () => {
    setPreviewing(true)
    setPreviewError('')
    try {
      setPreview(await getMeegoPreview(point.id, week))
    } catch (error) {
      setPreview(undefined)
      setPreviewError(error instanceof Error ? error.message : '暂时无法读取 Meego')
    } finally {
      setPreviewing(false)
    }
  }

  return (
    <div className="group/point">
      <div className="flex items-start gap-2">
        {showProgress ? <Caret open={open} onToggle={onToggle} /> : <span className="w-4 shrink-0" />}
        <span className="mt-0.5 shrink-0 rounded-md border border-blue-200 bg-blue-50 px-2 py-0.5 text-xs font-semibold text-blue-600">KR{index + 1}</span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start gap-x-2 gap-y-1">
            <span className="min-w-60 flex-1">
              <Text value={point.title} onChange={(value) => setPointTitle(objectiveId, krId, point.id, value)} placeholder="具体 KR 点" className="text-[15px] font-semibold leading-6 text-slate-800" readOnly={readOnly} commentTarget={{ type: 'point', id: point.id, title: point.title }} />
            </span>
            {showProgress && <span className="pt-1 text-xs text-slate-400">{doing} 进展 · {done} 已完成</span>}
          </div>
          {(point.meegoWorkItemId || editingMeego) && (
            <div className="mt-1.5 flex flex-wrap items-center gap-1 text-[11px] text-slate-400">
              <span>Meego</span>
              {!readOnly ? (
                <>
                  <input value={point.meegoWorkItemId ?? ''} onChange={(event) => setPointMeegoLink(krId, point.id, { meegoWorkItemId: event.target.value, meegoUrl: point.meegoUrl })} placeholder="工作项 ID" className="w-24 rounded border border-slate-200 bg-white px-1.5 py-0.5 outline-none focus:border-blue-400" />
                  <input value={point.meegoUrl ?? ''} onChange={(event) => setPointMeegoLink(krId, point.id, { meegoWorkItemId: point.meegoWorkItemId, meegoUrl: event.target.value })} placeholder="链接（可选）" className="min-w-32 flex-1 rounded border border-slate-200 bg-white px-1.5 py-0.5 outline-none focus:border-blue-400" />
                  {!point.meegoWorkItemId && <button type="button" onClick={() => setEditingMeego(false)} className="rounded px-1.5 py-0.5 hover:bg-slate-100 hover:text-slate-600">收起</button>}
                </>
              ) : point.meegoUrl ? <a href={point.meegoUrl} target="_blank" rel="noreferrer" className="text-blue-500 hover:underline">{point.meegoWorkItemId}</a> : <span>{point.meegoWorkItemId}</span>}
              {showProgress && point.meegoWorkItemId && <button type="button" onClick={() => void loadPreview()} disabled={previewing} className="rounded px-1.5 py-0.5 text-blue-500 hover:bg-blue-50 disabled:text-slate-300">{previewing ? '读取中' : '对比'}</button>}
            </div>
          )}
          {!readOnly && !point.meegoWorkItemId && !editingMeego && (
            <button type="button" onClick={() => setEditingMeego(true)} className="mt-1 text-[11px] text-slate-300 opacity-0 transition-opacity hover:text-blue-500 group-hover/point:opacity-100">+ 关联 Meego</button>
          )}
        </div>
        {!readOnly && <button type="button" onClick={() => removePoint(objectiveId, krId, point.id)} title="删除这个 KR 点" className="text-slate-300 opacity-0 transition-opacity group-hover/point:opacity-100 hover:text-red-500">×</button>}
      </div>
      {showProgress && (preview || previewError) && (
        <div className="mt-2 ml-8 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2 text-xs leading-5 text-slate-500">
          {previewError ? previewError : preview && (
            <>
              <div className="flex items-center gap-2"><span className={preview.needsReview ? 'font-medium text-amber-600' : 'font-medium text-emerald-600'}>{preview.needsReview ? '待确认差异' : '进度一致'}</span><span>只读预览 · 不会覆盖页面</span>{preview.url && <a href={preview.url} target="_blank" rel="noreferrer" className="ml-auto text-blue-500 hover:underline">打开工作项</a>}</div>
              <div className="mt-1 grid grid-cols-[2.5rem_1fr] gap-x-2"><span className="text-slate-400">页面</span><span>{preview.local.status || '无状态'} · {preview.local.progress || '暂无本周进展'}</span><span className="text-slate-400">Meego</span><span>{preview.remote.status || '无状态'} · {preview.remote.progress || preview.remote.title || '暂无进展'}</span></div>
            </>
          )}
        </div>
      )}
    </div>
  )
}

function PointBlock({ objectiveId, krId, point, index, open, onToggle, definitionReadOnly, progressReadOnly, showProgress }: { objectiveId: string; krId: string; point: Point; index: number; open: boolean; onToggle: () => void; definitionReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean }) {
  return (
    <article id={`point-${point.id}`} className="scroll-mt-5 border-l-2 border-slate-200 pl-3 sm:pl-4">
      <PointHeader objectiveId={objectiveId} krId={krId} point={point} index={index} open={open} onToggle={onToggle} readOnly={definitionReadOnly} showProgress={showProgress} />
      {open && showProgress && (
        <div className="mt-2 grid grid-cols-1 gap-2 pl-7 md:grid-cols-2">
          <section className="rounded-xl border border-slate-200 bg-white p-2.5">
            <h4 className="mb-2 text-xs font-semibold text-slate-500">进展</h4>
            <EntryList point={point} done={false} readOnly={progressReadOnly} />
            <HistoryPreview point={point} />
          </section>
          <section className="rounded-xl border border-slate-200 bg-white p-2.5">
            <h4 className="mb-2 text-xs font-semibold text-slate-500">已完成</h4>
            <EntryList point={point} done readOnly={progressReadOnly} />
          </section>
        </div>
      )}
    </article>
  )
}

function PointGroup({ objectiveId, kr, kind, closed, toggle, definitionReadOnly, progressReadOnly, showProgress }: { objectiveId: string; kr: Kr; kind: PointKind; closed: Set<string>; toggle: (id: string) => void; definitionReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean }) {
  const { addPoint } = useBoard()
  const points = kr.points.filter((point) => point.kind === kind)
  if (points.length === 0 && definitionReadOnly) return null

  return (
    <section className={`rounded-xl border-l-[3px] p-2.5 ${kind === 'strategy' ? 'border-l-violet-500 bg-violet-50/35' : 'border-l-teal-500 bg-teal-50/35'}`}>
      <div className="mb-2 flex items-center gap-2">
        <span className={`flex size-5 items-center justify-center rounded text-[10px] font-bold text-white ${kind === 'strategy' ? 'bg-violet-500' : 'bg-teal-500'}`}>{kind === 'strategy' ? '策' : '产'}</span>
        <h3 className={`text-[12px] font-semibold ${kind === 'strategy' ? 'text-violet-700' : 'text-teal-700'}`}>{KIND_LABEL[kind]}</h3>
        <span className="rounded-full bg-white/80 px-1.5 text-[10px] text-slate-400">{points.length} 条</span>
        {!definitionReadOnly && <button type="button" onClick={() => addPoint(objectiveId, kr.id, kind)} className="text-xs text-slate-400 hover:text-blue-600">+ 一项</button>}
      </div>
      <div className="space-y-4">
        {points.map((point, index) => <PointBlock key={point.id} objectiveId={objectiveId} krId={kr.id} point={point} index={index} open={!closed.has(point.id)} onToggle={() => toggle(point.id)} definitionReadOnly={definitionReadOnly} progressReadOnly={progressReadOnly} showProgress={showProgress} />)}
        {points.length === 0 && <Empty>暂无{KIND_LABEL[kind]}</Empty>}
      </div>
    </section>
  )
}

function KrCard({ objectiveId, kr, closed, toggle, readOnly, definitionsReadOnly, progressReadOnly, showProgress }: { objectiveId: string; kr: Kr; closed: Set<string>; toggle: (id: string) => void; readOnly: boolean; definitionsReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean }) {
  const open = !closed.has(kr.id)
  const definitionLocked = readOnly || definitionsReadOnly
  return (
    <article className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_2px_8px_rgba(31,35,40,0.035)]">
      <KrHeader objectiveId={objectiveId} kr={kr} open={open} onToggle={() => toggle(kr.id)} readOnly={definitionLocked} />
      {open && (
        <div className="space-y-5 px-4 py-4">
          <MetricBox kr={kr} readOnly={definitionLocked} />
          {KINDS.map((kind) => <PointGroup key={kind} objectiveId={objectiveId} kr={kr} kind={kind} closed={closed} toggle={toggle} definitionReadOnly={definitionLocked} progressReadOnly={readOnly || progressReadOnly} showProgress={showProgress} />)}
        </div>
      )}
    </article>
  )
}

function ObjectiveSection({ objective, closed, toggle, readOnly, definitionsReadOnly, progressReadOnly, showProgress, showTitle = true }: { objective: Objective; closed: Set<string>; toggle: (id: string) => void; readOnly: boolean; definitionsReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean; showTitle?: boolean }) {
  const open = showTitle ? !closed.has(objective.id) : true
  return (
    <section>
      {showTitle && <div className="mb-3 flex items-center gap-2 px-1">
        <Caret open={open} onToggle={() => toggle(objective.id)} />
        <h2 className="text-[17px] font-semibold text-slate-800">{objective.title}</h2>
        <span className="rounded-full border border-slate-200 bg-white px-2.5 py-0.5 text-xs text-slate-400">{objective.krs.length} 个 KR</span>
      </div>}
      {open && <div className="space-y-3">{objective.krs.map((kr) => <KrCard key={kr.id} objectiveId={objective.id} kr={kr} closed={closed} toggle={toggle} readOnly={readOnly} definitionsReadOnly={definitionsReadOnly} progressReadOnly={progressReadOnly} showProgress={showProgress} />)}</div>}
    </section>
  )
}

export function KrTable({ readOnly = false, definitionsReadOnly = false, progressReadOnly = false, showProgress = true }: { readOnly?: boolean; definitionsReadOnly?: boolean; progressReadOnly?: boolean; showProgress?: boolean }) {
  const { objectives } = useBoard()
  const [closed, setClosed] = useState<Set<string>>(new Set())
  const [ownerFilter, setOwnerFilter] = useState('')
  const [activeBusinessId, setActiveBusinessId] = useState('')
  const [activeSubgroupId, setActiveSubgroupId] = useState('')
  const [activeObjectiveId, setActiveObjectiveId] = useState('')
  const owners = useMemo(() => [...new Set(objectives.flatMap((objective) => objective.krs.flatMap((kr) => splitOwnerNames(kr.ownerName))))].sort(), [objectives])
  const visibleObjectives = useMemo(() => objectives.map((objective) => ({ ...objective, krs: objective.krs.filter((kr) => !ownerFilter || hasOwner(kr.ownerName, ownerFilter)) })).filter((objective) => objective.krs.length > 0), [objectives, ownerFilter])
  const navigation = useMemo(() => buildBusinessNavigation(visibleObjectives), [visibleObjectives])
  const firstBusiness = navigation.find((business) => businessKrCount(business) > 0) ?? navigation[0]
  const activeBusiness = navigation.find((business) => business.id === activeBusinessId) ?? firstBusiness
  const firstSubgroup = activeBusiness?.subgroups.find((subgroup) => subgroup.objectives.length > 0) ?? activeBusiness?.subgroups[0]
  const activeSubgroup = activeBusiness?.subgroups.find((subgroup) => subgroup.id === activeSubgroupId) ?? firstSubgroup
  const activeObjective = activeSubgroup?.objectives.find((objective) => objective.id === activeObjectiveId) ?? activeSubgroup?.objectives[0]

  const toggle = (id: string) => setClosed((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
  const collapseAll = () => setClosed(new Set(activeObjective?.krs.flatMap((kr) => [kr.id, ...kr.points.map((point) => point.id)]) ?? []))

  return (
    <div className={readOnly ? 'kr-table-readonly' : ''}>
      <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
        <span className="rounded-full border border-slate-200 bg-white px-2.5 py-1 text-[10px] text-slate-400">共 {krCount(objectives)} 条 KR，当前显示 {krCount(visibleObjectives)} 条</span>
        <span className="ml-auto text-slate-400">负责人</span>
        <select value={ownerFilter} onChange={(event) => { setOwnerFilter(event.target.value); setActiveBusinessId(''); setActiveSubgroupId(''); setActiveObjectiveId('') }} className="rounded-md border border-slate-200 bg-white px-2.5 py-1 text-slate-600 outline-none focus:border-blue-400">
          <option value="">全部负责人</option>{owners.map((owner) => <option key={owner} value={owner}>{owner}</option>)}
        </select>
        <span className="font-medium text-slate-500">层级</span>
        <div className="inline-flex overflow-hidden rounded-md border border-slate-200 bg-white">
          <button type="button" onClick={() => setClosed(new Set())} className="px-2.5 py-1 text-slate-500 hover:bg-slate-50 hover:text-slate-700">全部展开</button>
          <button type="button" onClick={collapseAll} className="border-l border-slate-200 px-2.5 py-1 text-slate-500 hover:bg-slate-50 hover:text-slate-700">全部折叠</button>
        </div>
      </div>
      <HierarchyNav
        navigation={navigation}
        activeBusiness={activeBusiness}
        activeSubgroup={activeSubgroup}
        activeObjectiveId={activeObjective?.id}
        onBusiness={(id) => { setActiveBusinessId(id); setActiveSubgroupId(''); setActiveObjectiveId('') }}
        onSubgroup={(id) => { setActiveSubgroupId(id); setActiveObjectiveId('') }}
        onObjective={setActiveObjectiveId}
      />
      <div>
        {activeObjective && <ObjectiveSection key={activeObjective.id} objective={activeObjective} closed={closed} toggle={toggle} readOnly={readOnly} definitionsReadOnly={definitionsReadOnly} progressReadOnly={progressReadOnly} showProgress={showProgress} showTitle={false} />}
        {!activeObjective && <Empty>没有符合筛选条件的 KR</Empty>}
      </div>
    </div>
  )
}
