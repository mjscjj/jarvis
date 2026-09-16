import { useCallback, useEffect, useMemo, useState } from 'react'
import { getMeegoPreview } from '../api'
import { useBoard } from '../board'
import { buildAllBusinessNavigation, buildKRHierarchy, priorityOf } from '../hierarchy'
import { krHasAnyOwner, krOwnerCounts, krOwnerOptions, ownerIdentityKey, splitOwnerNames } from '../people'
import { canAddMetric } from '../metricEditing'
import { collapseAllIds, KINDS } from '../rows'
import { KIND_LABEL, isDone, statusOf } from '../template'
import type { Entry, ImageRef, Kr, KrOwner, KrPriority, KrTag, MeegoPreview, MetricLine, Objective, Point, PointKind } from '../types'
import { TagEditor } from './TagEditor'
import { PreviewReviewButton, PreviewReviewPanel } from '../aiReviewContext'
import { isReviewTemplate } from '../weekCatalog'
import { FeishuPeoplePicker, PointPeoplePicker } from './FeishuPeoplePicker'
import { PersonAvatar } from './PersonAvatar'
import { WeeklyScoreControl } from './WeeklyScoreControl'
import { HierarchyNav } from './HierarchyNav'
import { Images, Links, MoveButtons, StatusSelect, Text, usePastedImageUpload } from './ui'
import { OwnerFilterPicker } from './OwnerFilterPicker'
import { CommentSurfaceHint, commentTargetElementId, useCommentSurface } from '../commenting'

function Caret({ open, onToggle, label }: { open: boolean; onToggle: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      title={open ? `折叠${label}` : `展开${label}`}
      aria-label={open ? `折叠${label}` : `展开${label}`}
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

function priorityTone(priority: KrPriority | '') {
  if (priority === 'p0') return 'border-red-200 bg-red-50 font-semibold text-red-700'
  if (priority === 'p1') return 'border-amber-200 bg-amber-50 font-semibold text-amber-700'
  return 'border-slate-200 bg-white text-slate-500'
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

function EntryList({ point, done, readOnly }: { point: Point; done?: boolean; readOnly: boolean }) {
  const { addEntry } = useBoard()
  const [adding, setAdding] = useState(false)
  const [draft, setDraft] = useState('')
  const list = done === undefined ? point.entries : point.entries.filter((entry) => isDone(entry.status) === done)
  const commitDraft = () => {
    const clean = draft.trim()
    if (!clean) return
    addEntry(point.id, clean)
    setDraft('')
    setAdding(false)
  }

  return (
    <div className="space-y-1.5">
      {list.map((entry) => <EntryRow key={entry.id} pointId={point.id} entry={entry} readOnly={readOnly} />)}
      {list.length === 0 && (
        <div className="rounded-lg border border-dashed border-slate-200 px-3 py-4 text-xs text-slate-400">
          {done === true ? '状态改为「已完成」的条目会自动移到这里' : '暂无进展'}
        </div>
      )}
      {!readOnly && done !== true && (
        adding ? <div className="rounded-lg border border-blue-200 bg-white p-2">
          <textarea autoFocus rows={2} value={draft} onChange={(event) => setDraft(event.target.value)} onKeyDown={(event) => { if ((event.metaKey || event.ctrlKey) && event.key === 'Enter') commitDraft(); if (event.key === 'Escape') { setDraft(''); setAdding(false) } }} placeholder="填写本周进展" className="w-full resize-none rounded border border-slate-200 px-2 py-1.5 text-sm outline-none focus:border-blue-400" />
          <div className="mt-1.5 flex items-center gap-2"><button type="button" disabled={!draft.trim()} onClick={commitDraft} className="rounded-md bg-blue-600 px-2.5 py-1 text-[11px] font-medium text-white disabled:opacity-40">添加</button><button type="button" onClick={() => { setDraft(''); setAdding(false) }} className="px-1 text-[11px] text-slate-400">取消</button><span className="ml-auto text-[10px] text-slate-300">⌘ Enter 添加</span></div>
        </div> : <button type="button" onClick={() => setAdding(true)} className="text-xs text-slate-400 hover:text-blue-600">
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

function MetricRow({ compactPresentation = false, krId, metric, readOnly, onRemove }: { compactPresentation?: boolean; krId: string; metric: MetricLine; readOnly: boolean; onRemove: () => void }) {
  const { patchMetric } = useBoard()
  const commentTarget = { type: 'metric' as const, id: metric.id, title: metric.text }
  const commentSurface = useCommentSurface(commentTarget)
  const appendImages = useCallback((uploaded: ImageRef[]) => {
    patchMetric(krId, metric.id, { images: [...(metric.images ?? []), ...uploaded] })
  }, [krId, metric.id, metric.images, patchMetric])
  const paste = usePastedImageUpload(appendImages, readOnly)

  return (
    <div
      id={commentTargetElementId(commentTarget)}
      data-okr-metric-id={metric.id}
      onClick={commentSurface.onClick}
      onPaste={readOnly ? undefined : paste.onPaste}
      tabIndex={readOnly ? -1 : 0}
      aria-busy={paste.uploading}
      className={`group/metric group/commentable flex items-start gap-3 border-b border-slate-100 px-4 ${compactPresentation ? 'min-h-8 py-1' : 'min-h-12 py-2.5'} outline-none last:border-b-0 focus-within:bg-blue-50/30 focus:bg-blue-50/30 ${commentSurface.enabled ? 'cursor-pointer hover:bg-indigo-50/70' : ''} ${commentSurface.selected || commentSurface.focused ? 'bg-indigo-50/80 ring-2 ring-inset ring-indigo-500' : ''}`}
    >
      <div className="min-w-0 flex-1">
        <div className="flex items-start gap-3">
          <Text
            fit
            value={metric.text}
            onChange={(text) => patchMetric(krId, metric.id, { text })}
            placeholder="例：Q3 累计自然入驻 1,253 家，线索到入驻转化率 16.51%"
            className={`text-[16px] tracking-[0.005em] text-slate-800 ${compactPresentation ? 'leading-5' : 'leading-6'}`}
            readOnly={readOnly}
            commentTarget={{ type: 'metric', id: metric.id, title: metric.text }}
          />
        </div>
        {(!readOnly || (metric.images?.length ?? 0) > 0) && (
          <div className="mt-2 flex flex-wrap items-start gap-2">
            <Images value={metric.images ?? []} onChange={(images) => patchMetric(krId, metric.id, { images })} readOnly={readOnly} pasteEnabled={false} />
            {!readOnly && <span className={`text-[11px] ${paste.uploadError ? 'text-red-500' : 'text-slate-400'}`} title={paste.uploadError}>
              {paste.uploading ? '图片上传中…' : paste.uploadError || '在本行按 ⌘V / Ctrl+V 粘贴截图'}
              {paste.canRetry && <button type="button" onClick={paste.retry} className="ml-1 underline">重试</button>}
            </span>}
          </div>
        )}
      </div>
      {!readOnly && (
        <button type="button" onClick={onRemove} title="删除这条核心数据" className="ml-auto shrink-0 rounded px-1.5 py-1 text-[10px] text-slate-300 transition-colors hover:bg-red-50 hover:text-red-600">删除</button>
      )}
      <CommentSurfaceHint target={commentTarget} />
    </div>
  )
}

function EmptyMetric({ compactPresentation = false, disabled, pasteEnabled, onCreate }: { compactPresentation?: boolean; disabled: boolean; pasteEnabled: boolean; onCreate: (images?: ImageRef[]) => void }) {
  const paste = usePastedImageUpload((uploaded) => onCreate(uploaded), disabled || !pasteEnabled)
  return (
    <div onPaste={disabled || !pasteEnabled ? undefined : paste.onPaste} tabIndex={disabled || !pasteEnabled ? -1 : 0} className="outline-none focus:bg-blue-50/30">
      <button type="button" disabled={disabled || paste.uploading} onClick={() => onCreate()} className={`flex w-full items-center gap-3 border-b border-slate-100 px-4 text-left text-[16px] tracking-[0.005em] text-slate-400 last:border-b-0 enabled:hover:bg-slate-50 enabled:hover:text-slate-500 ${compactPresentation ? 'min-h-8 py-1 leading-5' : 'min-h-12 py-2.5 leading-6'}`}>
        <span>{paste.uploading ? '图片上传中…' : pasteEnabled ? '填写核心数据，或在此按 ⌘V / Ctrl+V 粘贴截图' : '例：Q3 累计自然入驻 1,253 家，线索到入驻转化率 16.51%'}</span>
      </button>
      {paste.uploadError && <div className="px-4 pb-2 text-[11px] text-red-500" title={paste.uploadError}>
        {paste.uploadError}{paste.canRetry && <button type="button" onClick={paste.retry} className="ml-1 underline">重试</button>}
      </div>}
    </div>
  )
}

function MetricBox({ compactPresentation = false, kr, readOnly }: { compactPresentation?: boolean; kr: Kr; readOnly: boolean }) {
  const { setMetricNote, addMetric, removeMetric, week } = useBoard()
  const metricCanBeAdded = canAddMetric(readOnly)
  const createMetric = (images: ImageRef[] = []) => {
    const metricId = addMetric(kr.id, { images })
    window.requestAnimationFrame(() => document.querySelector<HTMLTextAreaElement>(`[data-okr-metric-id="${metricId}"] textarea`)?.focus())
  }

  return (
    <section className={`rounded-xl border border-blue-100 bg-blue-50/55 ${compactPresentation ? 'px-2.5 py-1.5' : 'p-2.5'}`}>
      <div className={`flex items-center gap-3 ${compactPresentation ? 'mb-1' : 'mb-2'}`}>
        <h3 className="border-l-[3px] border-blue-500 pl-2 text-[12px] font-semibold text-blue-700">核心数据</h3>
        {(!readOnly || kr.metricNote) && (
          <span className="min-w-0 flex-1 text-right text-[11px] text-slate-400">
            <Text value={kr.metricNote} onChange={(value) => setMetricNote(kr.id, value)} placeholder="本周数据口径或更新时间" className="text-right text-[11px] text-slate-400" readOnly={readOnly} />
          </span>
        )}
      </div>
      <div className="group/metrics rounded-lg border border-blue-100 bg-white/90">
        {kr.metrics.map((metric) => (
          <MetricRow compactPresentation={compactPresentation} key={metric.id} krId={kr.id} metric={metric} readOnly={readOnly} onRemove={() => removeMetric(kr.id, metric.id)} />
        ))}
        {kr.metrics.length === 0 && (
          <EmptyMetric compactPresentation={compactPresentation} disabled={!metricCanBeAdded} pasteEnabled={Boolean(week)} onCreate={createMetric} />
        )}
        {metricCanBeAdded && kr.metrics.length > 0 && (
          <button type="button" onClick={() => createMetric()} className="w-full border-t border-slate-100 px-4 py-2 text-left text-xs text-slate-400 hover:bg-slate-50 hover:text-blue-600">+ 一条核心数据</button>
        )}
      </div>
    </section>
  )
}

const OWNER_TONES = [
  { badge: 'border-sky-200 bg-sky-50 text-sky-700', avatar: 'bg-sky-500' },
  { badge: 'border-violet-200 bg-violet-50 text-violet-700', avatar: 'bg-violet-500' },
  { badge: 'border-emerald-200 bg-emerald-50 text-emerald-700', avatar: 'bg-emerald-500' },
  { badge: 'border-amber-200 bg-amber-50 text-amber-700', avatar: 'bg-amber-500' },
  { badge: 'border-rose-200 bg-rose-50 text-rose-700', avatar: 'bg-rose-500' },
  { badge: 'border-cyan-200 bg-cyan-50 text-cyan-700', avatar: 'bg-cyan-500' },
] as const

function krOwners(kr: Kr): KrOwner[] {
  if (kr.owners?.length) return kr.owners.filter((owner) => owner.name.trim())
  return splitOwnerNames(kr.ownerName).map((name, index) => ({
    name,
    email: index === 0 ? (kr.ownerEmail ?? '') : '',
  }))
}

function ownerTone(owner: KrOwner) {
  const identity = owner.email || owner.name
  let hash = 0
  for (const character of identity) hash = ((hash << 5) - hash + character.codePointAt(0)!) | 0
  return OWNER_TONES[(hash >>> 0) % OWNER_TONES.length]
}

function KrOwnerBadge({ owner, compact = false }: { owner: KrOwner; compact?: boolean }) {
  const tone = ownerTone(owner)
  if (compact) return <span title={owner.name} aria-label={owner.name} className={`inline-flex max-w-full items-center gap-1 rounded px-0.5 py-0 text-[8px] leading-3 text-slate-500`}><PersonAvatar name={owner.name} email={owner.email} size="size-3 text-[7px]" tone={tone.avatar} /><span className="min-w-0 break-words">{owner.name}</span></span>
  return (
    <span title={`负责人：${owner.name}`} className={`inline-flex h-6 items-center gap-1 rounded-full border py-0.5 pr-2 pl-1 text-[10px] font-semibold shadow-sm ${tone.badge}`}>
      <PersonAvatar name={owner.name} email={owner.email} size="size-4 text-[8px]" tone={tone.avatar} />
      <span className="max-w-24 truncate">{owner.name}</span>
    </span>
  )
}

function KrHeader({ objectiveId, kr, open, onToggle, onMoveUp, onMoveDown, readOnly, structureReadOnly, showScore, scoreReadOnly }: { objectiveId: string; kr: Kr; open: boolean; onToggle: () => void; onMoveUp?: () => void; onMoveDown?: () => void; readOnly: boolean; structureReadOnly: boolean; showScore: boolean; scoreReadOnly: boolean }) {
	const { setKrTitle, setKrPriority, setKrScore } = useBoard()
	const priority = priorityOf(kr)

  return (
    <header className="group/kr border-b border-slate-100 px-3.5 py-2.5">
      <div className="flex items-start gap-2">
        <Caret open={open} onToggle={onToggle} label="KR" />
        <div className="min-w-0 flex-1">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0 flex-1">
              <Text value={kr.title} onChange={(value) => setKrTitle(objectiveId, kr.id, value)} placeholder="KR 标题" className="text-[14px] font-semibold leading-5 text-slate-900" readOnly={readOnly} commentTarget={{ type: 'kr', id: kr.id, title: kr.title }} />
            </div>
            <div aria-label="KR 负责人" className="flex max-w-[48%] shrink-0 flex-wrap items-center justify-end gap-1">
              {!readOnly && <FeishuPeoplePicker kr={kr} />}
              {readOnly && krOwners(kr).map((owner, index) => <KrOwnerBadge key={`${owner.email || owner.name}:${index}`} owner={owner} />)}
            </div>
            {!readOnly && <MoveButtons label="条 KR" onUp={onMoveUp} onDown={onMoveDown} />}
          </div>
          <div className="mt-1.5 flex flex-wrap items-center gap-1.5 pl-1">
			{!structureReadOnly ? (
				<select value={priority} onChange={(event) => setKrPriority(kr.id, event.target.value as KrPriority | '')} className={`rounded-md border px-2 py-1 text-xs outline-none focus:border-blue-400 ${priorityTone(priority)}`}>
					<option value="">未标注</option><option value="p0">Focus · P0</option><option value="p1">P1</option><option value="p2">P2</option>
				</select>
			) : <span title="业务分类和优先级只在「管理与打标」里维护" className={`rounded-md border px-2 py-1 text-xs uppercase ${priorityTone(priority)}`}>{priority === 'p0' ? 'Focus · P0' : priority || '未标注'}</span>}
			{showScore && <WeeklyScoreControl score={kr.score} readOnly={scoreReadOnly} onChange={(score) => setKrScore(kr.id, score)} label="一级 KR 评分" />}
          </div>
        </div>
      </div>
    </header>
  )
}

function PointHeader({ reviewUnderLabel = false, compactPresentation = false, objectiveId, krId, point, index, open, onToggle, onMoveUp, onMoveDown, readOnly, structureReadOnly, showProgress = true, showScore = false, scoreReadOnly = true, tagSuggestions, deleteWarning }: { reviewUnderLabel?: boolean; compactPresentation?: boolean; objectiveId: string; krId: string; point: Point; index: number; open: boolean; onToggle: () => void; onMoveUp?: () => void; onMoveDown?: () => void; readOnly: boolean; structureReadOnly: boolean; showProgress?: boolean; showScore?: boolean; scoreReadOnly?: boolean; tagSuggestions?: KrTag[]; deleteWarning: string }) {
  const { setPointTitle, setPointKind, setPointMeegoLink, removePoint, addPointTag, removePointTag, setPointScore, week } = useBoard()
  const commentTarget = { type: 'point' as const, id: point.id, title: point.title }
  const commentSurface = useCommentSurface(commentTarget)
  const [preview, setPreview] = useState<MeegoPreview>()
  const [previewError, setPreviewError] = useState('')
  const [previewing, setPreviewing] = useState(false)
  const [editingMeego, setEditingMeego] = useState(Boolean(point.meegoWorkItemId))
  const [confirmDelete, setConfirmDelete] = useState(false)
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

  const pointFooter = <>
        {!readOnly && <span className={compactPresentation ? 'flex min-w-0 max-w-full flex-wrap items-center gap-1' : 'flex max-w-[45%] shrink-0 flex-wrap items-center justify-end gap-1 pt-0.5'}><PointPeoplePicker krId={krId} point={point} small={compactPresentation} /></span>}
        {readOnly && (point.owners?.length ?? 0) > 0 && <span aria-label="具体 KR 负责人" className={compactPresentation ? 'flex min-w-0 max-w-full flex-wrap items-center gap-1' : 'flex max-w-[45%] shrink-0 flex-wrap items-center justify-end gap-1 pt-0.5'}>{point.owners?.map((owner, ownerIndex) => <KrOwnerBadge key={`${owner.email || owner.name}:${ownerIndex}`} owner={owner} compact={compactPresentation} />)}</span>}

        {!readOnly && <MoveButtons label="条具体 KR" onUp={onMoveUp} onDown={onMoveDown} />}
        {!structureReadOnly && (confirmDelete ? (
          <span className="flex shrink-0 items-center gap-1">
            <span className="text-[9px] leading-tight text-red-500">{deleteWarning}</span>
            <button type="button" onClick={() => removePoint(objectiveId, krId, point.id)} className="h-6 rounded-md bg-red-600 px-2 text-[9px] font-medium !text-white hover:bg-red-700">确认</button>
            <button type="button" onClick={() => setConfirmDelete(false)} className="h-6 px-1 text-[9px] text-slate-400 hover:text-slate-700">取消</button>
          </span>
        ) : (
          <button type="button" onClick={() => setConfirmDelete(true)} title="删除这个具体 KR" className="shrink-0 text-slate-300 opacity-0 transition-opacity group-hover/point:opacity-100 hover:text-red-500">×</button>
        ))}
        <CommentSurfaceHint target={commentTarget} />
  </>

  return (
    <div id={commentTargetElementId(commentTarget)} onClick={commentSurface.onClick} className={`group/point group/commentable rounded-md transition-[background-color,box-shadow] ${commentSurface.enabled ? 'cursor-pointer hover:bg-indigo-50/70' : ''} ${commentSurface.selected || commentSurface.focused ? 'bg-indigo-50/80 ring-2 ring-inset ring-indigo-500' : ''}`}>
      <div className="flex items-start gap-2">
        {showProgress ? <Caret open={open} onToggle={onToggle} label="具体 KR" /> : !compactPresentation && <span className="w-4 shrink-0" />}
        <span className="mt-0.5 flex shrink-0 flex-col items-center gap-1">
          <span className="rounded-md border border-blue-200 bg-blue-50 px-2 py-0.5 text-xs font-semibold text-blue-600">KR{index + 1}</span>
          {reviewUnderLabel && <PreviewReviewButton target={{ kind: 'point', objectiveId, krId, pointId: point.id, title: point.title }} label="AI评审" iconOnly />}
        </span>
        {!compactPresentation && !structureReadOnly && <select
          value={point.kind}
          onChange={(event) => setPointKind(krId, point.id, event.target.value as PointKind)}
          title="切换这条具体 KR 的分组"
          aria-label="具体 KR 分组"
          className="mt-0.5 h-6 shrink-0 rounded-md border border-slate-200 bg-white px-1.5 text-[10px] text-slate-600 outline-none focus:border-blue-400"
        >
          {KINDS.map((kind) => <option key={kind} value={kind}>{KIND_LABEL[kind]}</option>)}
        </select>}
        <div className="min-w-0 flex-1">
          <div className="flex items-start gap-2">
            <span className="flex min-w-0 flex-1 items-start gap-1">
              <Text value={point.title} onChange={(value) => setPointTitle(objectiveId, krId, point.id, value)} placeholder="具体 KR 点" className={`text-[15px] font-semibold text-slate-800 ${compactPresentation ? 'leading-5' : 'leading-6'}`} readOnly={readOnly} commentTarget={{ type: 'point', id: point.id, title: point.title }} />
            </span>
            {showScore && <span className="pt-0.5"><WeeklyScoreControl score={point.score} readOnly={scoreReadOnly} onChange={(score) => setPointScore(krId, point.id, score)} label="具体 KR 评分" /></span>}
            {showProgress && <span className="pt-1 text-xs text-slate-400">{doing} 进展 · {done} 已完成</span>}
          </div>
          {tagSuggestions && (compactPresentation && !point.tags?.length ? (
            <details className="mt-0.5 text-[10px] text-slate-400">
              <summary className="cursor-pointer">标签</summary>
              <TagEditor idPrefix={`point-tag-options-${point.id}`} tags={point.tags ?? []} suggestions={tagSuggestions} emptyLabel="+ 要点标签" onAdd={(value, type) => addPointTag(krId, point.id, value, type)} onRemove={(type, value) => removePointTag(krId, point.id, type, value)} />
            </details>
          ) : (
            <div className={compactPresentation ? 'mt-0.5 min-w-0' : 'mt-1.5 min-w-0'}>
              <TagEditor idPrefix={`point-tag-options-${point.id}`} tags={point.tags ?? []} suggestions={tagSuggestions} emptyLabel="+ 要点标签" onAdd={(value, type) => addPointTag(krId, point.id, value, type)} onRemove={(type, value) => removePointTag(krId, point.id, type, value)} />
            </div>
          ))}
          {(point.meegoWorkItemId || editingMeego) && (
            <div className="mt-1.5 flex flex-wrap items-center gap-1 text-[11px] text-slate-400">
              <span>Meego</span>
              {!structureReadOnly ? (
                <>
                  <input value={point.meegoWorkItemId ?? ''} onChange={(event) => setPointMeegoLink(krId, point.id, { meegoWorkItemId: event.target.value, meegoUrl: point.meegoUrl })} placeholder="工作项 ID" className="w-24 rounded border border-slate-200 bg-white px-1.5 py-0.5 outline-none focus:border-blue-400" />
                  <input value={point.meegoUrl ?? ''} onChange={(event) => setPointMeegoLink(krId, point.id, { meegoWorkItemId: point.meegoWorkItemId, meegoUrl: event.target.value })} placeholder="链接（可选）" className="min-w-32 flex-1 rounded border border-slate-200 bg-white px-1.5 py-0.5 outline-none focus:border-blue-400" />
                  {!point.meegoWorkItemId && <button type="button" onClick={() => setEditingMeego(false)} className="rounded px-1.5 py-0.5 hover:bg-slate-100 hover:text-slate-600">收起</button>}
                </>
              ) : point.meegoUrl ? <a href={point.meegoUrl} target="_blank" rel="noreferrer" className="text-blue-500 hover:underline">{point.meegoWorkItemId}</a> : <span>{point.meegoWorkItemId}</span>}
              {showProgress && point.meegoWorkItemId && <button type="button" onClick={() => void loadPreview()} disabled={previewing} className="rounded px-1.5 py-0.5 text-blue-500 hover:bg-blue-50 disabled:text-slate-300">{previewing ? '读取中' : '对比'}</button>}
            </div>
          )}
          {!structureReadOnly && !point.meegoWorkItemId && !editingMeego && (
            <button type="button" onClick={() => setEditingMeego(true)} className={compactPresentation ? "hidden text-[11px] text-slate-400 hover:text-blue-500 group-hover/point:inline-block group-focus-within/point:inline-block" : "mt-1 text-[11px] text-slate-300 opacity-0 transition-opacity hover:text-blue-500 group-hover/point:opacity-100"}>+ 关联 Meego</button>
          )}
          {compactPresentation && <div className="mt-1 flex flex-wrap items-center gap-1">{pointFooter}</div>}
        </div>
        {!compactPresentation && pointFooter}
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

function PointBlock({ compactPresentation = false, objectiveId, krId, point, index, open, onToggle, onMoveUp, onMoveDown, definitionReadOnly, structureReadOnly, progressReadOnly, showProgress, reviewEnabled = false, tagSuggestions, deleteWarning }: { compactPresentation?: boolean; objectiveId: string; krId: string; point: Point; index: number; open: boolean; onToggle: () => void; onMoveUp?: () => void; onMoveDown?: () => void; definitionReadOnly: boolean; structureReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean; reviewEnabled?: boolean; tagSuggestions?: KrTag[]; deleteWarning: string }) {
  const { templateKey } = useBoard()
  // Review reports one combined lane; the classic weekly report splits 进展 and
  // 已完成. Both formats read the loaded week's template, never the tab.
  const review = isReviewTemplate(templateKey)
  // AI 评审跟着评分走：属于某一周的 review 内容，只在那一周的进展可见时出现。
  const showReview = reviewEnabled || (showProgress && review)
  const reviewTarget = { kind: 'point' as const, objectiveId, krId, pointId: point.id, title: point.title }
  return (
    <article id={`point-${point.id}`} className="scroll-mt-5 border-l-2 border-slate-200 pl-3 sm:pl-4">
      <PointHeader reviewUnderLabel={compactPresentation && showReview} compactPresentation={compactPresentation} objectiveId={objectiveId} krId={krId} point={point} index={index} open={open} onToggle={onToggle} onMoveUp={onMoveUp} onMoveDown={onMoveDown} readOnly={definitionReadOnly} structureReadOnly={structureReadOnly} showProgress={showProgress} showScore={!compactPresentation && showReview} scoreReadOnly={progressReadOnly} tagSuggestions={tagSuggestions} deleteWarning={deleteWarning} />
      {showReview && !compactPresentation && <div className="mt-1.5 flex justify-end pl-7"><PreviewReviewButton target={reviewTarget} label="AI评审" /></div>}
      {showReview && <PreviewReviewPanel target={reviewTarget} className="mt-1.5 ml-7" />}
      {open && showProgress && (review
        ? <div className="mt-2 pl-7">
          <section className="rounded-xl border border-slate-200 bg-white p-2.5">
            <h4 className="mb-2 text-xs font-semibold text-slate-500">本周进展</h4>
            <EntryList point={point} readOnly={progressReadOnly} />
            <HistoryPreview point={point} />
          </section>
        </div>
        : <div className="mt-2 grid grid-cols-1 gap-2 pl-7 md:grid-cols-2">
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

function PointGroup({ compactPresentation = false, objectiveId, kr, kind, closed, toggle, definitionReadOnly, structureReadOnly, progressReadOnly, showProgress, reviewEnabled = false, tagSuggestions, deleteWarning }: { compactPresentation?: boolean; objectiveId: string; kr: Kr; kind: PointKind; closed: Set<string>; toggle: (id: string) => void; definitionReadOnly: boolean; structureReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean; reviewEnabled?: boolean; tagSuggestions?: KrTag[]; deleteWarning: string }) {
  const { addPoint, swapPoints } = useBoard()
  const points = kr.points.filter((point) => point.kind === kind)
  if (points.length === 0 && structureReadOnly && !compactPresentation) return null

  return (
    <section className={`min-w-0 rounded-xl border-l-[3px] ${compactPresentation ? 'px-2 py-1.5' : 'p-2.5'} ${kind === 'strategy' ? 'border-l-violet-500 bg-violet-50/35' : 'border-l-teal-500 bg-teal-50/35'}`}>
      <div className={`flex items-center gap-2 ${compactPresentation ? 'mb-1' : 'mb-2'}`}>
        <span className={`flex size-5 items-center justify-center rounded text-[10px] font-bold text-white ${kind === 'strategy' ? 'bg-violet-500' : 'bg-teal-500'}`}>{kind === 'strategy' ? '策' : '产'}</span>
        <h3 className={`text-[12px] font-semibold ${kind === 'strategy' ? 'text-violet-700' : 'text-teal-700'}`}>{KIND_LABEL[kind]}</h3>
        <span className="rounded-full bg-white/80 px-1.5 text-[10px] text-slate-400">{points.length} 条</span>
        {!structureReadOnly && <button type="button" onClick={() => addPoint(objectiveId, kr.id, kind)} className="ml-auto shrink-0 text-xs text-slate-400 hover:text-blue-600">+ 一项</button>}
      </div>
      <div className={compactPresentation ? 'space-y-1.5' : 'space-y-4'}>
        {points.map((point, index) => <PointBlock compactPresentation={compactPresentation} key={point.id} objectiveId={objectiveId} krId={kr.id} point={point} index={index} open={!closed.has(point.id)} onToggle={() => toggle(point.id)} onMoveUp={index > 0 ? () => swapPoints(kr.id, point.id, points[index - 1].id) : undefined} onMoveDown={index < points.length - 1 ? () => swapPoints(kr.id, point.id, points[index + 1].id) : undefined} definitionReadOnly={definitionReadOnly} structureReadOnly={structureReadOnly} progressReadOnly={progressReadOnly} showProgress={showProgress} reviewEnabled={reviewEnabled} tagSuggestions={tagSuggestions} deleteWarning={deleteWarning} />)}
        {points.length === 0 && <Empty>暂无{KIND_LABEL[kind]}</Empty>}
      </div>
    </section>
  )
}

export function KrDefinitionDetails({ compactPresentation = false, objectiveId, kr, tagSuggestions, deletePointWarning = '连同各周进展一起删除', compactEmptyPointGroups = false, cardBody = false, readOnly = false, reviewEnabled = false }: { compactPresentation?: boolean; objectiveId: string; kr: Kr; tagSuggestions?: KrTag[]; deletePointWarning?: string; compactEmptyPointGroups?: boolean; cardBody?: boolean; readOnly?: boolean; reviewEnabled?: boolean }) {
  const { addPoint } = useBoard()
  const visibleKinds = KINDS.filter((kind) => !compactEmptyPointGroups || kr.points.some((point) => point.kind === kind))
  const [closed, setClosed] = useState<Set<string>>(new Set())
  const toggle = (id: string) => setClosed((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })

  return (
    <div className={compactPresentation ? '@container space-y-2 border-t border-slate-100 pt-2' : cardBody ? 'space-y-5 border-t border-slate-100 pt-4' : 'space-y-4 rounded-xl border border-slate-200 bg-slate-50/55 p-3'}>
		<MetricBox compactPresentation={compactPresentation} kr={kr} readOnly={readOnly} />
      <div className={compactPresentation ? `grid grid-cols-1 items-stretch gap-3 ${visibleKinds.length > 1 ? '@[800px]:grid-cols-2' : ''}` : cardBody ? 'space-y-5' : 'space-y-4'}>
      {visibleKinds.map((kind) => (
        <PointGroup
          compactPresentation={compactPresentation}
          key={kind}
          objectiveId={objectiveId}
          kr={kr}
          kind={kind}
          closed={closed}
          toggle={toggle}
		  definitionReadOnly={readOnly}
		  structureReadOnly={readOnly}
          progressReadOnly
          showProgress={false}
          reviewEnabled={reviewEnabled}
          tagSuggestions={tagSuggestions}
          deleteWarning={deletePointWarning}
        />
      ))}
      </div>
		{!readOnly && compactEmptyPointGroups && KINDS.some((kind) => !kr.points.some((point) => point.kind === kind)) && <div className="flex flex-wrap gap-2">
        {KINDS.filter((kind) => !kr.points.some((point) => point.kind === kind)).map((kind) => <button key={kind} type="button" onClick={() => addPoint(objectiveId, kr.id, kind)} className="rounded-md border border-dashed border-slate-200 bg-white px-2.5 py-1.5 text-[10px] text-slate-400 hover:border-blue-300 hover:bg-blue-50 hover:text-blue-600">{`+ ${KIND_LABEL[kind]}`}</button>)}
      </div>}
    </div>
  )
}

function KrCard({ objectiveId, kr, closed, toggle, onMoveUp, onMoveDown, readOnly, definitionsReadOnly, progressReadOnly, showProgress }: { objectiveId: string; kr: Kr; closed: Set<string>; toggle: (id: string) => void; onMoveUp?: () => void; onMoveDown?: () => void; readOnly: boolean; definitionsReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean }) {
  const { templateKey } = useBoard()
  const open = !closed.has(kr.id)
  // Wording and people live on the shared definition but a filling week may
  // edit them. Decomposition rows and labels stay with 管理与打标; metric rows
  // are a week-scoped snapshot and remain structurally editable while filling.
  const structureLocked = readOnly || definitionsReadOnly
  // Scores belong to a reporting week, so they show wherever that week's
  // progress shows, and only for the template that scores.
  const showScore = showProgress && isReviewTemplate(templateKey)
  const reviewTarget = { kind: 'kr' as const, objectiveId, krId: kr.id, title: kr.title }
  return (
    <article className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_2px_8px_rgba(31,35,40,0.035)]">
	  <KrHeader objectiveId={objectiveId} kr={kr} open={open} onToggle={() => toggle(kr.id)} onMoveUp={onMoveUp} onMoveDown={onMoveDown} readOnly={readOnly} structureReadOnly={structureLocked} showScore={showScore} scoreReadOnly={readOnly || progressReadOnly} />
      {showScore && <div className="flex justify-end px-4 pt-2"><PreviewReviewButton target={reviewTarget} label="AI评审" /></div>}
      {showScore && <PreviewReviewPanel target={reviewTarget} className="mx-4 mt-2" />}
      {open && (
        <div className="space-y-5 px-4 py-4">
          <MetricBox kr={kr} readOnly={readOnly} />
          {KINDS.map((kind) => <PointGroup key={kind} objectiveId={objectiveId} kr={kr} kind={kind} closed={closed} toggle={toggle} definitionReadOnly={readOnly} structureReadOnly={structureLocked} progressReadOnly={readOnly || progressReadOnly} showProgress={showProgress} deleteWarning="连同各周进展一起删除" />)}
        </div>
      )}
    </article>
  )
}

function ObjectiveSection({ objective, closed, toggle, readOnly, definitionsReadOnly, progressReadOnly, showProgress, showTitle = true, showReview = false }: { objective: Objective; closed: Set<string>; toggle: (id: string) => void; readOnly: boolean; definitionsReadOnly: boolean; progressReadOnly: boolean; showProgress: boolean; showTitle?: boolean; showReview?: boolean }) {
  const { swapKrs } = useBoard()
  const open = showTitle ? !closed.has(objective.id) : true
  return (
    <section>
      {showTitle && <div className="mb-3 flex items-center gap-2 px-1">
        <Caret open={open} onToggle={() => toggle(objective.id)} label="目标" />
        <h2 className="text-[17px] font-semibold text-slate-800">{objective.title}</h2>
        <span className="rounded-full border border-slate-200 bg-white px-2.5 py-0.5 text-xs text-slate-400">{objective.krs.length} 个 KR</span>
        {showReview && <PreviewReviewButton target={{ kind: 'objective', objectiveId: objective.id, title: objective.title }} label="AI评审" />}
      </div>}
      {showReview && <PreviewReviewPanel target={{ kind: 'objective', objectiveId: objective.id, title: objective.title }} className="mb-3" />}
      {open && <div className="space-y-3">{objective.krs.map((kr, index) => <KrCard key={kr.id} objectiveId={objective.id} kr={kr} closed={closed} toggle={toggle} onMoveUp={index > 0 ? () => void swapKrs(objective.id, kr.id, objective.krs[index - 1].id) : undefined} onMoveDown={index < objective.krs.length - 1 ? () => void swapKrs(objective.id, kr.id, objective.krs[index + 1].id) : undefined} readOnly={readOnly} definitionsReadOnly={definitionsReadOnly} progressReadOnly={progressReadOnly} showProgress={showProgress} />)}</div>}
    </section>
  )
}

function ObjectiveControls({ objective }: { objective: Objective }) {
	const { updateObjective, deleteObjective } = useBoard()
	const [editing, setEditing] = useState(false)
	const [title, setTitle] = useState(objective.title)
	const [busy, setBusy] = useState(false)
	const [confirmDelete, setConfirmDelete] = useState(false)
	const save = async () => {
		const clean = title.trim()
		if (!clean || clean === objective.title) {
			setTitle(objective.title)
			setEditing(false)
			return
		}
		setBusy(true)
		try {
			await updateObjective(objective.id, clean)
			setEditing(false)
		} catch {
			// BoardProvider exposes the failure in the shared sync notice.
		} finally {
			setBusy(false)
		}
	}
	const remove = async () => {
		setBusy(true)
		try {
			await deleteObjective(objective.id)
		} catch {
			// BoardProvider exposes the failure in the shared sync notice.
		} finally {
			setBusy(false)
			setConfirmDelete(false)
		}
	}

	return <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-slate-200 bg-white px-3 py-2 text-[10px]">
		<span className="text-slate-400">当前方向</span>
		{editing ? <>
			<input autoFocus value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void save(); if (event.key === 'Escape') { setTitle(objective.title); setEditing(false) } }} className="h-7 min-w-64 flex-1 rounded-md border border-slate-200 px-2 text-[11px] font-medium text-slate-700 outline-none focus:border-blue-400" />
			<button type="button" disabled={busy || !title.trim()} onClick={() => void save()} className="h-7 rounded-md bg-blue-600 px-2.5 font-medium text-white disabled:opacity-40">保存</button>
			<button type="button" disabled={busy} onClick={() => { setTitle(objective.title); setEditing(false) }} className="h-7 px-1.5 text-slate-400">取消</button>
		</> : <>
			<strong className="min-w-0 flex-1 truncate text-[11px] text-slate-700">{objective.title}</strong>
			<button type="button" onClick={() => { setTitle(objective.title); setEditing(true) }} className="h-7 rounded-md border border-slate-200 px-2.5 text-slate-600 hover:border-blue-200 hover:text-blue-600">重命名</button>
			{objective.krs.length === 0 && (confirmDelete ? <>
				<button type="button" disabled={busy} onClick={() => void remove()} className="h-7 rounded-md bg-red-600 px-2.5 text-white disabled:opacity-40">确认删除</button>
				<button type="button" disabled={busy} onClick={() => setConfirmDelete(false)} className="h-7 px-1.5 text-slate-400">取消</button>
			</> : <button type="button" onClick={() => setConfirmDelete(true)} className="h-7 rounded-md px-2 text-red-500 hover:bg-red-50">删除空方向</button>)}
		</>}
	</div>
}

export function KrTable({ readOnly = false, definitionsReadOnly = false, progressReadOnly = false, showProgress = true, manageObjectives = false, showObjectiveHeader = false, collapsedToKR = false }: { readOnly?: boolean; definitionsReadOnly?: boolean; progressReadOnly?: boolean; showProgress?: boolean; manageObjectives?: boolean; showObjectiveHeader?: boolean; collapsedToKR?: boolean }) {
	const { objectives, templateKey } = useBoard()
	const showReview = showProgress && isReviewTemplate(templateKey)
	const [closed, setClosed] = useState<Set<string>>(new Set())
	const [contentQuery, setContentQuery] = useState('')
	const [ownerFilters, setOwnerFilters] = useState<string[]>([])
	const [activeBusinessValue, setActiveBusinessValue] = useState<string>()
	const [activePriorityValue, setActivePriorityValue] = useState<string>()
	const [activeObjectiveId, setActiveObjectiveId] = useState('')
	const owners = useMemo(() => krOwnerOptions(objectives), [objectives])
	const ownersByKey = useMemo(() => new Map(owners.map((owner) => [ownerIdentityKey(owner), owner])), [owners])
	const ownerKRCounts = useMemo(() => krOwnerCounts(objectives, owners), [objectives, owners])
	const selectedOwners = useMemo(() => ownerFilters.flatMap((key) => {
		const owner = ownersByKey.get(key)
		return owner ? [owner] : []
	}), [ownerFilters, ownersByKey])
	const hasOwnerFilter = ownerFilters.length > 0
	const normalizedQuery = contentQuery.trim().toLocaleLowerCase()
	const visibleObjectives = useMemo(() => objectives
		.map((objective) => ({
			...objective,
			krs: objective.krs.filter((kr) => {
				if (!krHasAnyOwner(kr, selectedOwners)) return false
				if (!normalizedQuery || objective.title.toLocaleLowerCase().includes(normalizedQuery)) return true
				const content = [
					kr.title,
					kr.metricNote,
					...kr.metrics.map((metric) => metric.text),
					...kr.points.flatMap((point) => [point.title, ...point.entries.map((entry) => entry.text), ...(point.previousEntries ?? []).map((entry) => entry.text)]),
				].join('\n').toLocaleLowerCase()
				return content.includes(normalizedQuery)
			}),
		}))
		.filter((objective) => (!hasOwnerFilter && !normalizedQuery) || objective.krs.length > 0), [hasOwnerFilter, normalizedQuery, objectives, selectedOwners])
	const navigation = useMemo(() => buildKRHierarchy(visibleObjectives), [visibleObjectives])
	const overview = activeBusinessValue === undefined
	const allBusiness = useMemo(() => buildAllBusinessNavigation(navigation), [navigation])
	const activeBusiness = overview ? allBusiness : navigation.find((business) => business.value === activeBusinessValue) ?? navigation[0]
	const activePriority = activeBusiness?.priorities.find((priority) => priority.value === activePriorityValue) ?? activeBusiness?.priorities[0]
	const activeObjective = activePriority?.objectives.find((objective) => objective.id === activeObjectiveId) ?? activePriority?.objectives[0]
  const totalKRCount = objectives.reduce((sum, objective) => sum + objective.krs.length, 0)
  const visibleKRCount = visibleObjectives.reduce((sum, objective) => sum + objective.krs.length, 0)
	const activeKRCount = activeObjective?.krs.length ?? 0

	useEffect(() => {
		setOwnerFilters((current) => {
			const valid = current.filter((key) => ownersByKey.has(key))
			return valid.length === current.length ? current : valid
		})
	}, [ownersByKey])

  const toggle = (id: string) => setClosed((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
  const collapseAll = () => setClosed(collapseAllIds(activeObjective ? [activeObjective] : []))

  useEffect(() => {
    if (!collapsedToKR) {
      setClosed(new Set())
      return
    }
    setClosed(collapseAllIds(activeObjective ? [activeObjective] : []))
  }, [activeObjective?.id, collapsedToKR])

	  return (
	    <div className={readOnly ? 'kr-table-readonly' : ''}>
	      <div className="mb-3 flex flex-wrap items-center gap-2 text-xs">
			<input value={contentQuery} onChange={(event) => { setContentQuery(event.target.value); setActiveBusinessValue(undefined); setActivePriorityValue(undefined); setActiveObjectiveId('') }} aria-label="搜索 OKR 内容" placeholder="搜索 O / KR / 进展内容" className="h-7 min-w-48 rounded-md border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none transition-colors placeholder:text-slate-300 focus:border-blue-300 focus:ring-2 focus:ring-blue-50 sm:w-60" />
			<OwnerFilterPicker
			options={owners}
			ownerCounts={ownerKRCounts}
			selectedKeys={ownerFilters}
			onChange={(keys) => {
				setOwnerFilters(keys)
				setActiveBusinessValue(undefined)
				setActivePriorityValue(undefined)
				setActiveObjectiveId('')
			}}
			/>
	        <div className="inline-flex overflow-hidden rounded-md border border-slate-200 bg-white">
	          <button type="button" onClick={() => setClosed(new Set())} className="px-2.5 py-1 text-slate-500 hover:bg-slate-50 hover:text-slate-700">全部展开</button>
	          <button type="button" onClick={collapseAll} className="border-l border-slate-200 px-2.5 py-1 text-slate-500 hover:bg-slate-50 hover:text-slate-700">折叠到 KR</button>
	        </div>
			<span className="ml-auto rounded-full border border-slate-200 bg-white px-2.5 py-1 text-[10px] text-slate-400">共 {totalKRCount} 条 KR{hasOwnerFilter || normalizedQuery ? `，筛选后 ${visibleKRCount} 条` : ''}，当前方向 {activeKRCount} 条</span>
			{showReview && <PreviewReviewButton target={{ kind: 'all', title: '全部 OKR' }} label="AI评审" className="px-3" />}
	      </div>
		{showReview && <PreviewReviewPanel target={{ kind: 'all', title: '全部 OKR' }} className="mb-3" />}
		<HierarchyNav
			navigation={navigation}
			activeBusiness={activeBusiness}
			activePriority={activePriority}
			activeObjectiveId={activeObjective?.id}
			overview={overview}
			showOverview
			onOverview={() => { setActiveBusinessValue(undefined); setActivePriorityValue(undefined); setActiveObjectiveId('') }}
			onBusiness={(value) => { setActiveBusinessValue(value); setActivePriorityValue(undefined); setActiveObjectiveId('') }}
			onPriority={(value) => { setActivePriorityValue(value); setActiveObjectiveId('') }}
				onObjective={setActiveObjectiveId}
      />
		{manageObjectives && activeObjective && <ObjectiveControls key={activeObjective.id} objective={activeObjective} />}
      <div>
        {activeObjective && <ObjectiveSection key={activeObjective.id} objective={activeObjective} closed={closed} toggle={toggle} readOnly={readOnly} definitionsReadOnly={definitionsReadOnly} progressReadOnly={progressReadOnly} showProgress={showProgress} showTitle={showObjectiveHeader} showReview={showReview} />}
        {!activeObjective && <Empty>没有符合筛选条件的 KR</Empty>}
      </div>
    </div>
  )
}
