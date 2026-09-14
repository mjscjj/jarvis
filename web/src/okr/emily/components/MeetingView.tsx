import { useEffect, useMemo, useRef, useState } from 'react'
import type { MouseEvent, ReactNode } from 'react'
import { createFeishuDocument } from '../api'
import { useBoard } from '../board'
import { commentTargetFromThread, commentTargetKey, findCommentTargetLocation } from '../comments'
import { commentSelectionElementId, commentTargetElementId, scrollToCommentSource, useCommentInteraction } from '../commenting'
import { buildAllBusinessNavigation, buildKRHierarchy, businessCategoryOf, priorityLabel, priorityOf } from '../hierarchy'
import { hasOwner, splitOwnerNames } from '../people'
import { collapseAllIds, KINDS } from '../rows'
import { buildFullMeetingMarkdown } from '../meetingMarkdown'
import { KIND_LABEL, isDone } from '../template'
import { isReviewTemplate } from '../weekCatalog'
import type { CommentTarget, Entry, KrPriority, Objective, Point, PointKind, TextSelection } from '../types'
import { HierarchyNav } from './HierarchyNav'
import { PersonAvatar } from './PersonAvatar'
import { Images, Links, StatusSelect } from './ui'
import { WeeklyScoreControl } from './WeeklyScoreControl'
function FoldButton({ open, onToggle, label }: { open: boolean; onToggle: () => void; label: string }) {
  return (
    <button
      type="button"
      onClick={(event) => { event.stopPropagation(); onToggle() }}
      title={open ? `折叠${label}` : `展开${label}`}
      aria-label={open ? `折叠${label}` : `展开${label}`}
      aria-expanded={open}
      className="flex size-5 shrink-0 items-center justify-center rounded text-slate-400 hover:bg-slate-100 hover:text-slate-700"
    >
      <svg viewBox="0 0 12 12" aria-hidden className={`size-3 transition-transform ${open ? 'rotate-90' : ''}`}>
        <path d="M4 2.2 L8.8 6 L4 9.8 Z" fill="currentColor" />
      </svg>
    </button>
  )
}

function priorityTone(priority: KrPriority | '') {
  if (priority === 'p0') return 'border-red-200 bg-red-50 text-red-700'
  if (priority === 'p1') return 'border-amber-200 bg-amber-50 text-amber-700'
  return 'border-slate-200 bg-white text-slate-500'
}

function priorityRail(priority: KrPriority | '') {
  if (priority === 'p0') return 'border-l-red-500'
  if (priority === 'p1') return 'border-l-amber-400'
  return 'border-l-slate-300'
}

function compactTitle(value: string, length = 90) {
  const clean = value.replace(/\s+/g, ' ').trim()
  return clean.length > length ? `${clean.slice(0, length)}…` : clean
}

function Commentable({ target, children, className = '' }: { target: CommentTarget; children: ReactNode; className?: string }) {
  const interaction = useCommentInteraction()
  const count = interaction.counts[commentTargetKey(target)] ?? 0
  const selected = !interaction.focused && interaction.selected && commentTargetKey(interaction.selected) === commentTargetKey(target)
  const focused = interaction.focused && commentTargetKey(interaction.focused) === commentTargetKey(target)
  const select = () => {
    interaction.select(target)
  }
  return (
    <div
      id={commentTargetElementId(target)}
      onClick={(event) => {
        event.stopPropagation()
        if (window.getSelection()?.toString().trim()) return
        select()
      }}
      className={`group/commentable relative cursor-pointer transition-[background-color,box-shadow] hover:bg-indigo-50/70 ${selected || focused ? 'bg-indigo-50/80 ring-2 ring-inset ring-indigo-500' : ''} ${className}`}
    >
      {children}
      {count > 0 && <span className="ml-auto shrink-0 rounded-full bg-indigo-50 px-1.5 py-0.5 text-[9px] font-medium text-indigo-600">{count} 条评论</span>}
      {count === 0 && <span className="ml-auto shrink-0 text-[9px] font-medium text-indigo-400 opacity-0 transition-opacity group-hover/commentable:opacity-100">点击评论</span>}
    </div>
  )
}

function selectionWithin(root: HTMLElement, event: MouseEvent<HTMLElement>, text: string): TextSelection | undefined {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return undefined
  const range = selection.getRangeAt(0)
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return undefined
  const before = document.createRange()
  before.selectNodeContents(root)
  before.setEnd(range.startContainer, range.startOffset)
  let start = before.toString().length
  let end = start + range.toString().length
  const raw = text.slice(start, end)
  const leading = raw.match(/^\s*/)?.[0].length ?? 0
  const trailing = raw.match(/\s*$/)?.[0].length ?? 0
  start += leading
  end -= trailing
  if (end <= start) return undefined
  event.stopPropagation()
  return {
    text: text.slice(start, end),
    start,
    end,
    prefix: text.slice(Math.max(0, start - 48), start),
    suffix: text.slice(end, Math.min(text.length, end + 48)),
  }
}

function HighlightedText({ target, text }: { target: CommentTarget; text: string }) {
  const interaction = useCommentInteraction()
  const ref = useRef<HTMLSpanElement>(null)
  const selectionTargetKey = commentTargetKey(target)
  const pending = interaction.pendingSelection?.targetKey === selectionTargetKey ? interaction.pendingSelection.selection : undefined
  const ranges = useMemo(() => interaction.comments
    .filter((comment) => commentTargetKey(comment) === commentTargetKey(target) && comment.selectedText)
    .map((comment) => {
      let start = comment.selectionStart ?? -1
      let end = comment.selectionEnd ?? -1
      if (start < 0 || end <= start || text.slice(start, end) !== comment.selectedText) {
        start = text.indexOf(comment.selectedText ?? '')
        end = start + (comment.selectedText?.length ?? 0)
      }
      return { comment, start, end }
    })
    .filter((item) => item.start >= 0 && item.end > item.start)
    .sort((left, right) => left.start - right.start || left.end - right.end)
    .filter((item, index, items) => index === 0 || item.start >= items[index - 1].end), [interaction.comments, target, text])

  const parts: ReactNode[] = []
  let cursor = 0
  for (const range of ranges) {
    if (range.start > cursor) parts.push(text.slice(cursor, range.start))
    parts.push(
      <mark
        id={commentSelectionElementId(range.comment.id)}
        key={range.comment.id}
        title={`${1 + range.comment.replies.length} 条讨论`}
        onClick={(event) => { event.stopPropagation(); interaction.select(commentTargetFromThread(range.comment)) }}
        className={`cursor-pointer rounded-sm px-0.5 text-inherit ring-1 hover:bg-amber-200 ${interaction.focused?.id === range.comment.id ? 'bg-indigo-200 ring-indigo-400' : 'bg-amber-100 ring-amber-200'}`}
      >{text.slice(range.start, range.end)}</mark>,
    )
    cursor = range.end
  }
  if (cursor < text.length) parts.push(text.slice(cursor))

  return (
    <span
      className="relative cursor-text"
      ref={ref}
      onClick={(event) => event.stopPropagation()}
      onMouseUp={(event) => {
        const next = ref.current ? selectionWithin(ref.current, event, text) : undefined
        if (next) interaction.setPendingSelection({ targetKey: selectionTargetKey, selection: next })
        else if (interaction.pendingSelection?.targetKey === selectionTargetKey) interaction.setPendingSelection(undefined)
      }}
    >
      {parts.length > 0 ? parts : text}
      {pending && (
        <button
          type="button"
          data-comment-selection-trigger
          onMouseDown={(event) => event.preventDefault()}
          onClick={(event) => {
            event.stopPropagation()
            interaction.select({ ...target, selection: pending })
            interaction.setPendingSelection(undefined)
            window.getSelection()?.removeAllRanges()
          }}
          className="absolute -top-8 right-0 z-20 whitespace-nowrap rounded-md bg-slate-900 px-2 py-1 text-[10px] font-medium text-white shadow-lg hover:bg-indigo-700"
        >评论选中文字</button>
      )}
    </span>
  )
}

function MeetingEntry({ entry }: { entry: Entry }) {
  const target: CommentTarget = { type: 'entry', id: entry.id, title: compactTitle(entry.text) }
  return (
    <Commentable target={target} className="flex items-start gap-2 px-2.5 py-1.5">
      <span className="pt-px"><StatusSelect value={entry.status} onChange={() => undefined} readOnly /></span>
      <div className="min-w-0 flex-1 text-[13px] leading-[19px] text-slate-700">
        <div><HighlightedText target={target} text={entry.text} /></div>
        {entry.docs.length > 0 && (
          <div className="mt-1">
            <Links value={entry.docs} onChange={() => undefined} readOnly />
          </div>
        )}
        {entry.images.length > 0 && (
          <div className="mt-1 flex flex-wrap items-start gap-1">
            <Images value={entry.images} onChange={() => undefined} readOnly maxDisplayWidth={280} />
          </div>
        )}
      </div>
    </Commentable>
  )
}

function MeetingLane({ entries }: { entries: Entry[] }) {
  return (
    <section className="min-w-0">
      <div className="divide-y divide-slate-100">
        {entries.map((entry) => <MeetingEntry key={entry.id} entry={entry} />)}
        {entries.length === 0 && <div className="px-2.5 py-2 text-[11px] text-slate-300">暂无</div>}
      </div>
    </section>
  )
}

function MeetingPoint({ objectiveId, krId, point, index, open, onToggle, reviewMode }: { objectiveId: string; krId: string; point: Point; index: number; open: boolean; onToggle: () => void; reviewMode: boolean }) {
  const { setPointScore } = useBoard()
  const doing = point.entries.filter((entry) => !isDone(entry.status))
  const done = point.entries.filter((entry) => isDone(entry.status))
  const target: CommentTarget = { type: 'point', id: point.id, title: point.title }
  return (
    <article className="border-l border-slate-200 pl-2">
      <header className="flex items-start">
        <span className="pt-1.5"><FoldButton open={open} onToggle={onToggle} label="具体 KR" /></span>
        <Commentable target={target} className="flex min-w-0 flex-1 items-start gap-2 px-1.5 py-1.5 pr-2.5">
          <span className="mt-px shrink-0 rounded border border-blue-200 bg-blue-50 px-1.5 py-px text-[10px] font-semibold text-blue-600">KR{index + 1}</span>
          <div className="min-w-0 flex-1">
            <h4 className="text-[13px] font-semibold leading-[19px] text-slate-800"><HighlightedText target={target} text={point.title} /></h4>
          </div>
          {reviewMode && <WeeklyScoreControl score={point.score} onChange={(score) => setPointScore(krId, point.id, score)} label="具体 KR 评分" />}
          <span className="shrink-0 pt-0.5 text-[10px] text-slate-400">{doing.length} 进展 · {done.length} 完成</span>
        </Commentable>
      </header>
      {open && reviewMode && <div className="ml-6"><MeetingLane entries={point.entries} /></div>}
      {open && !reviewMode && <div className="ml-6 grid grid-cols-1 divide-y divide-slate-100 md:grid-cols-2 md:divide-x md:divide-y-0"><MeetingLane entries={doing} /><MeetingLane entries={done} /></div>}
    </article>
  )
}

function KindGroup({ objectiveId, krId, kind, points, closed, toggle, reviewMode }: { objectiveId: string; krId: string; kind: PointKind; points: Point[]; closed: Set<string>; toggle: (id: string) => void; reviewMode: boolean }) {
  const tone = kind === 'strategy' ? 'border-violet-200 bg-violet-50 text-violet-700' : 'border-teal-200 bg-teal-50 text-teal-700'
  return (
    <section className={`border-l-[3px] pl-2.5 ${kind === 'strategy' ? 'border-violet-500' : 'border-teal-500'}`}>
      <div className="mb-1.5 flex items-center gap-2">
        <span className={`rounded-md border px-2 py-0.5 text-[10px] font-semibold ${tone}`}>{KIND_LABEL[kind]}</span>
        <span className="text-[10px] text-slate-400">{points.length} 项</span>
        <span className="h-px flex-1 bg-slate-100" />
      </div>
	  <div className="space-y-2">{points.map((point, index) => <MeetingPoint key={point.id} objectiveId={objectiveId} krId={krId} point={point} index={index} open={!closed.has(point.id)} onToggle={() => toggle(point.id)} reviewMode={reviewMode} />)}</div>
    </section>
  )
}

function MeetingObjectiveSection({ objective, closed, toggle, reviewMode }: { objective: Objective; closed: Set<string>; toggle: (id: string) => void; reviewMode: boolean }) {
  const { setKrScore } = useBoard()
  const commentInteraction = useCommentInteraction()
  const target: CommentTarget = { type: 'objective', id: objective.id, title: objective.title }
  const focused = commentInteraction.focused && commentTargetKey(commentInteraction.focused) === commentTargetKey(target)
  const objectiveOpen = !closed.has(objective.id)
  return (
    <section className="space-y-1.5">
      <div id={commentTargetElementId(target)} className={`flex items-center gap-2 rounded-r-lg border-l-4 border-blue-600 bg-blue-50 px-2.5 py-1.5 ${focused ? 'ring-2 ring-inset ring-indigo-500' : ''}`}>
        <FoldButton open={objectiveOpen} onToggle={() => toggle(objective.id)} label="目标" />
        <h2 className="min-w-0 flex-1 text-[14px] font-bold text-blue-800">{objective.title}</h2>
        <span className="rounded-full border border-blue-100 bg-white/80 px-2 py-0.5 text-[10px] text-blue-600">{objective.krs.length} 条 KR</span>
      </div>

      {objectiveOpen && <div className="overflow-hidden rounded-lg border border-slate-200 bg-white">
		{objective.krs.map((kr) => {
			const krTarget: CommentTarget = { type: 'kr', id: kr.id, title: kr.title }
			const krOpen = !closed.has(kr.id)
			const priority = priorityOf(kr)
			return (
				<article key={kr.id} className={`border-b border-l-[3px] border-b-slate-100 bg-white last:border-b-0 ${priorityRail(priority)}`}>
              <header className="flex items-start pl-1.5">
                <span className="pt-1.5"><FoldButton open={krOpen} onToggle={() => toggle(kr.id)} label="KR" /></span>
                <Commentable target={krTarget} className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2 gap-y-1 px-1.5 py-1.5 pr-3">
                  <h3 className="min-w-0 text-[13px] font-bold leading-5 text-slate-900"><HighlightedText target={krTarget} text={kr.title} /></h3>
                  <div className="flex flex-wrap items-center gap-1">
                    {/* 用结构化的 owners 而不是拆 ownerName：头像按 email 命中，
                        名字是英文别名（Hsiangfu Kuo 之于郭祥莆）时也能对上人。 */}
                    {(kr.owners ?? []).map((owner) => <span key={owner.email || owner.name} className="inline-flex items-center gap-1 text-[10px] text-slate-500"><PersonAvatar name={owner.name} email={owner.email} />{owner.name}</span>)}
						<span className={`rounded border px-1.5 py-px text-[9px] font-semibold ${priorityTone(priority)}`}>{priority === 'p0' ? 'Focus · P0' : priorityLabel(priority)}</span>
					{reviewMode && <WeeklyScoreControl score={kr.score} onChange={(score) => setKrScore(kr.id, score)} label="一级 KR 评分" />}
                  </div>
                </Commentable>
              </header>

              {krOpen && <div className="space-y-2 px-3 pb-2.5">
                {kr.metrics.length > 0 && (
                  <section className="rounded-lg border-l-[3px] border-blue-400 bg-blue-50/70 px-2.5 py-2">
                    <div className="mb-1 flex items-center gap-2">
                      <span className="text-[10px] font-bold text-blue-700">核心数据</span>
                      {kr.metricNote && <span className="truncate text-[10px] text-slate-400">{kr.metricNote}</span>}
                    </div>
                    <div className="grid gap-x-4 gap-y-0.5 md:grid-cols-2">
                      {kr.metrics.map((metric) => {
                        const target: CommentTarget = { type: 'metric', id: metric.id, title: compactTitle(metric.text) }
                        return (
                          <Commentable key={metric.id} target={target} className="flex min-h-7 items-start gap-2 px-1.5 py-0.5 text-[12px] leading-[18px] text-slate-800">
                            <div className="min-w-0 flex-1">
                              <div className="font-medium"><HighlightedText target={target} text={metric.text} /></div>
                              {(metric.images?.length ?? 0) > 0 && (
                                <div className="mt-1 flex flex-wrap items-start gap-1">
                                  <Images value={metric.images ?? []} onChange={() => undefined} readOnly maxDisplayWidth={360} />
                                </div>
                              )}
                            </div>
                          </Commentable>
                        )
                      })}
                    </div>
                  </section>
                )}
                {KINDS.map((kind) => {
                  const points = kr.points.filter((point) => point.kind === kind)
				  return points.length > 0 ? <KindGroup key={kind} objectiveId={objective.id} krId={kr.id} kind={kind} points={points} closed={closed} toggle={toggle} reviewMode={reviewMode} /> : null
                })}
              </div>}
            </article>
          )
        })}
      </div>}
    </section>
  )
}

export function MeetingView() {
  const { objectives, quarter, week, templateKey } = useBoard()
  const commentInteraction = useCommentInteraction()
  // Review-only affordances follow the loaded week's template, not the tab, so
  // a week can never be rendered in the other ceremony's format.
  const reviewMode = isReviewTemplate(templateKey)
  const [ownerFilter, setOwnerFilter] = useState('')
  const [closed, setClosed] = useState<Set<string>>(new Set())
  const [activeBusinessValue, setActiveBusinessValue] = useState<string>()
  const [activePriorityValue, setActivePriorityValue] = useState<string>()
  const [activeObjectiveId, setActiveObjectiveId] = useState('')
  const [exporting, setExporting] = useState(false)
  const [exportResult, setExportResult] = useState<{ url?: string; message?: string }>({})
  const owners = useMemo(() => [...new Set(objectives.flatMap((objective) => objective.krs.flatMap((kr) => splitOwnerNames(kr.ownerName))))].sort(), [objectives])
  const filteredObjectives = useMemo(() => objectives.map((objective) => ({
    ...objective,
    krs: objective.krs.filter((kr) => !ownerFilter || hasOwner(kr.ownerName, ownerFilter)),
  })).filter((objective) => objective.krs.length > 0), [objectives, ownerFilter])
  const navigation = useMemo(() => buildKRHierarchy(filteredObjectives), [filteredObjectives])
  const meetingOverview = activeBusinessValue === undefined
  const allBusiness = useMemo(() => buildAllBusinessNavigation(navigation), [navigation])
  const activeBusiness = meetingOverview ? allBusiness : navigation.find((business) => business.value === activeBusinessValue) ?? navigation[0]
  const activePriority = activeBusiness?.priorities.find((priority) => priority.value === activePriorityValue) ?? activeBusiness?.priorities[0]
	const activeObjective = activePriority?.objectives.find((objective) => objective.id === activeObjectiveId) ?? activePriority?.objectives[0]
  const visible = activeObjective ? [activeObjective] : []

  useEffect(() => {
    const comment = commentInteraction.focused
    if (!comment) return
    const location = findCommentTargetLocation(objectives, comment)
    if (!location) return
    const navigationKr = location.kr ?? location.objective.krs[0]
    setOwnerFilter('')
    if (navigationKr) {
      setActiveBusinessValue(businessCategoryOf(navigationKr))
      setActivePriorityValue(priorityOf(navigationKr))
    }
    setActiveObjectiveId(location.objective.id)
    setClosed((previous) => {
      const next = new Set(previous)
      next.delete(location.objective.id)
      if (location.kr) next.delete(location.kr.id)
      if (location.point) next.delete(location.point.id)
      return next
    })
    const timeout = window.setTimeout(() => scrollToCommentSource(comment), 0)
    return () => window.clearTimeout(timeout)
  }, [commentInteraction.focused, objectives])
  const toggle = (id: string) => setClosed((previous) => {
    const next = new Set(previous)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    return next
  })
  const collapseAll = () => setClosed(collapseAllIds(visible))
  const exportToFeishu = async () => {
    if (objectives.length === 0 || exporting) return
    setExporting(true)
    setExportResult({})
    try {
	  const output = buildFullMeetingMarkdown(objectives, quarter, week, templateKey)
      const result = await createFeishuDocument(output.title, output.content)
      setExportResult({ url: result.url, message: result.warnings.length > 0 ? `飞书文档已生成，另有 ${result.warnings.length} 条转换提示。` : '飞书文档已生成。' })
    } catch (error) {
      setExportResult({ message: error instanceof Error ? error.message : '飞书文档生成失败。' })
    } finally {
      setExporting(false)
    }
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <span className="ml-1 text-[11px] text-slate-400">层级</span>
        <div className="inline-flex overflow-hidden rounded-md border border-slate-200 bg-white text-[11px]">
          <button type="button" onClick={() => setClosed(new Set())} className="px-2 py-1 text-slate-500 hover:bg-slate-50 hover:text-slate-700">全部展开</button>
          <button type="button" onClick={collapseAll} className="border-l border-slate-200 px-2 py-1 text-slate-500 hover:bg-slate-50 hover:text-slate-700">折叠到 KR</button>
        </div>
        <span className="ml-auto text-[11px] text-slate-400">负责人</span>
        <select value={ownerFilter} onChange={(event) => { setOwnerFilter(event.target.value); setActiveBusinessValue(undefined); setActivePriorityValue(undefined); setActiveObjectiveId('') }} className="rounded-md border border-slate-200 bg-white px-2 py-1 text-[11px] text-slate-600 outline-none focus:border-blue-400">
          <option value="">全部负责人</option>{owners.map((owner) => <option key={owner} value={owner}>{owner}</option>)}
        </select>
        <span className="h-4 w-px bg-slate-200" />
		<button type="button" disabled={exporting || objectives.length === 0} onClick={() => void exportToFeishu()} className="rounded-md bg-blue-600 px-2.5 py-1 text-[11px] font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">{exporting ? '导出中…' : reviewMode ? '导出 OKR Review' : '导出全部 OKR'}</button>
        {exportResult.url && <a href={exportResult.url} target="_blank" rel="noreferrer" className="text-[11px] font-medium text-blue-600 hover:text-blue-700 hover:underline">打开文档</a>}
        {exportResult.message && <span className={`max-w-sm whitespace-normal text-[10px] ${exportResult.url ? 'text-emerald-600' : 'text-red-500'}`} title={exportResult.message}>{exportResult.message}</span>}
      </div>

      <HierarchyNav
        navigation={navigation}
        activeBusiness={activeBusiness}
        activePriority={activePriority}
        activeObjectiveId={activeObjective?.id}
        overview={meetingOverview}
        showOverview
        onOverview={() => { setActiveBusinessValue(undefined); setActivePriorityValue(undefined); setActiveObjectiveId('') }}
        onBusiness={(value) => { setActiveBusinessValue(value); setActivePriorityValue(undefined); setActiveObjectiveId('') }}
        onPriority={(value) => { setActivePriorityValue(value); setActiveObjectiveId('') }}
        onObjective={setActiveObjectiveId}
      />

      {activeObjective ? (
		<MeetingObjectiveSection objective={activeObjective} closed={closed} toggle={toggle} reviewMode={reviewMode} />
      ) : <div className="rounded-xl border border-dashed border-slate-200 py-10 text-center text-xs text-slate-400">当前分类尚无已接入的方向</div>}
    </div>
  )
}
