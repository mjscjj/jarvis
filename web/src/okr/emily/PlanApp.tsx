import { useEffect, useRef, useState } from 'react'
import { getWebConfig } from '../../api'
import { useBoard } from './board'
import { PlanBoardProvider, usePlanBoard } from './planStore'
import { QuarterSelect } from './components/QuarterSelect'
import { ManagementView } from './components/ManagementView'
import { WeeklyShareNav } from './components/WeeklyShareNav'
import { ActivityLogButton } from './components/ActivityLogButton'
import { CommentDrawer } from './components/CommentDrawer'
import { CommentInteractionProvider, commentTargetElementId, scrollToCommentSource, type PendingCommentSelection } from './commenting'
import type { CommentTarget, PageComment } from './types'
import { weeklyShareURLForTab, type WeeklyShareTab } from './share'
import { planOptionLabel } from './planTitle'

const PLAN_SCROLL_KEY_PREFIX = 'jarvis-okr-plan-scroll'

function planScrollStorageKey(quarter: string, planId: string) {
  return `${PLAN_SCROLL_KEY_PREFIX}:${quarter}:${planId}`
}

function readPlanScrollPosition(key: string): number | undefined {
  try {
    const saved = window.localStorage.getItem(key)
    if (saved === null) return undefined
    const position = Number(saved)
    return Number.isFinite(position) && position >= 0 ? position : undefined
  } catch {
    return undefined
  }
}

function savePlanScrollPosition(key: string) {
  try {
    window.localStorage.setItem(key, String(window.scrollY))
  } catch {
    // 浏览位置只是体验增强，本地存储不可用时不影响 Plan 编辑。
  }
}

function SyncNotice() {
  const { syncState, retry } = useBoard()
  if (syncState.kind !== 'error') return null
  return (
    <div className="mb-3 flex items-center gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800">
      <span className="flex-1">{syncState.message}</span>
      <button type="button" onClick={retry} className="rounded-md border border-red-200 bg-white px-2.5 py-1 hover:bg-red-100">重试</button>
    </div>
  )
}

function NewPlanPanel({ onClose }: { onClose: () => void }) {
  const { quarter, createPlan, syncState } = usePlanBoard()
  const [targetQuarter, setTargetQuarter] = useState(quarter)
  const [title, setTitle] = useState('')

  useEffect(() => setTargetQuarter(quarter), [quarter])

  const submit = async () => {
    const cleanTitle = title.trim()
    const cleanQuarter = targetQuarter.trim()
    if (!cleanTitle || !cleanQuarter) return
    await createPlan({ quarter: cleanQuarter, title: cleanTitle })
    setTitle('')
    onClose()
  }

  return (
    <section className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-indigo-100 bg-indigo-50/60 p-3">
      <div className="mr-2">
        <div className="text-xs font-semibold text-slate-700">新建 Biz OKR Plan</div>
        <div className="mt-0.5 text-[10px] text-slate-400">创建独立草稿，不会改动正式 OKR。</div>
      </div>
      <input value={targetQuarter} onChange={(event) => setTargetQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-9 w-28 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-indigo-400" />
      <input value={title} onChange={(event) => setTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submit(); if (event.key === 'Escape') onClose() }} placeholder="Plan 名称" aria-label="Plan 名称" className="h-9 min-w-64 flex-1 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-indigo-400" />
      <button type="button" onClick={() => void submit()} disabled={!targetQuarter.trim() || !title.trim() || syncState.kind === 'saving'} className="h-9 rounded-lg bg-indigo-600 px-4 text-xs font-medium text-white disabled:opacity-40">确认新建</button>
      <button type="button" onClick={onClose} className="h-9 px-2 text-xs text-slate-400">取消</button>
    </section>
  )
}

function PlanCanvas({ initialCommentId = '', shared = false, readOnly = false, onShareTabChange }: { initialCommentId?: string; shared?: boolean; readOnly?: boolean; onShareTabChange?: (tab: WeeklyShareTab) => void }) {
  const { plan, plans, quarter, syncState, selectPlan, deleteCurrentPlan } = usePlanBoard()
  const [creatingPlan, setCreatingPlan] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const [commentsOpen, setCommentsOpen] = useState(Boolean(initialCommentId))
  const [reviewingComments, setReviewingComments] = useState(false)
  const [focusedComment, setFocusedComment] = useState<PageComment>()
  const [commentCount, setCommentCount] = useState(0)
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({})
  const [comments, setComments] = useState<PageComment[]>([])
  const [commentTarget, setCommentTarget] = useState<CommentTarget>()
  const [pendingCommentSelection, setPendingCommentSelection] = useState<PendingCommentSelection>()
	const [shareNotice, setShareNotice] = useState('')
	const [shareLink, setShareLink] = useState('')
  const attemptedScrollKey = useRef('')
  const readyToSaveScrollKey = useRef('')
  const saving = syncState.kind === 'saving' || syncState.kind === 'loading'
  const scrollStorageKey = plan ? planScrollStorageKey(quarter, plan.id) : ''

  useEffect(() => {
    setCommentTarget(undefined)
    setReviewingComments(false)
    setFocusedComment(undefined)
    setPendingCommentSelection(undefined)
    setComments([])
    setCommentCount(0)
    setCommentCounts({})
    if (!plan && !initialCommentId) setCommentsOpen(false)
  }, [plan?.id])

  useEffect(() => {
    const clearPendingSelection = (event: Event) => {
      const target = event.target
      if (target instanceof Element && target.closest('[data-comment-selection-trigger]')) return
      setPendingCommentSelection(undefined)
    }
    const clearOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setPendingCommentSelection(undefined)
    }
    document.addEventListener('pointerdown', clearPendingSelection)
    document.addEventListener('focusin', clearPendingSelection)
    document.addEventListener('keydown', clearOnEscape)
    return () => {
      document.removeEventListener('pointerdown', clearPendingSelection)
      document.removeEventListener('focusin', clearPendingSelection)
      document.removeEventListener('keydown', clearOnEscape)
    }
  }, [])

  const openComments = (target?: CommentTarget) => {
    setPendingCommentSelection(undefined)
    window.getSelection()?.removeAllRanges()
    setReviewingComments(false)
    setFocusedComment(undefined)
    setCommentTarget(target)
    setCommentsOpen(true)
  }

  const toggleComments = () => {
    if (commentsOpen) {
      setCommentsOpen(false)
      setReviewingComments(false)
      setFocusedComment(undefined)
      return
    }
    openComments()
  }

	const copyShareLink = async () => {
		setShareNotice('')
		setShareLink('')
		let link: string
		try {
			const config = await getWebConfig()
			link = weeklyShareURLForTab(window.location.href, 'okr-plan', config.public_base_url)
		} catch (cause) {
			setShareNotice(cause instanceof Error ? `读取分享地址失败：${cause.message}` : '读取分享地址失败')
			return
		}
		if (!navigator.clipboard) {
			setShareLink(link)
			setShareNotice('当前页面无法自动复制，请复制下面的分享链接')
			return
		}
		try {
			await navigator.clipboard.writeText(link)
			setShareNotice('OKR Plan 只读链接已复制')
		} catch (cause) {
			setShareLink(link)
			setShareNotice(cause instanceof Error ? `自动复制失败：${cause.message}；请复制下面的链接` : '自动复制失败，请复制下面的链接')
		}
	}

  useEffect(() => {
    if (!focusedComment) return
    const timeout = window.setTimeout(() => scrollToCommentSource(focusedComment), 80)
    return () => window.clearTimeout(timeout)
  }, [focusedComment])

  useEffect(() => {
    if (!scrollStorageKey) return
    let pendingFrame = 0
    const savePosition = () => {
      if (readyToSaveScrollKey.current !== scrollStorageKey) return
      window.cancelAnimationFrame(pendingFrame)
      pendingFrame = window.requestAnimationFrame(() => savePlanScrollPosition(scrollStorageKey))
    }
    const savePositionNow = () => {
      if (readyToSaveScrollKey.current === scrollStorageKey) savePlanScrollPosition(scrollStorageKey)
    }
    window.addEventListener('scroll', savePosition, { passive: true })
    window.addEventListener('pagehide', savePositionNow)
    return () => {
      window.cancelAnimationFrame(pendingFrame)
      window.removeEventListener('scroll', savePosition)
      window.removeEventListener('pagehide', savePositionNow)
    }
  }, [scrollStorageKey])

  useEffect(() => {
    if (!scrollStorageKey || saving || attemptedScrollKey.current === scrollStorageKey) return
    attemptedScrollKey.current = scrollStorageKey
    if (initialCommentId) {
      readyToSaveScrollKey.current = scrollStorageKey
      return
    }
    const position = readPlanScrollPosition(scrollStorageKey)
    if (position === undefined) {
      readyToSaveScrollKey.current = scrollStorageKey
      return
    }
    let secondFrame = 0
    const firstFrame = window.requestAnimationFrame(() => {
      secondFrame = window.requestAnimationFrame(() => {
        window.scrollTo({ top: position, behavior: 'auto' })
        readyToSaveScrollKey.current = scrollStorageKey
      })
    })
    return () => {
      window.cancelAnimationFrame(firstFrame)
      window.cancelAnimationFrame(secondFrame)
      if (readyToSaveScrollKey.current !== scrollStorageKey && attemptedScrollKey.current === scrollStorageKey) {
        attemptedScrollKey.current = ''
      }
    }
  }, [initialCommentId, saving, scrollStorageKey])

  return (
    <>
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
        <div className={`mx-auto flex min-h-14 max-w-[1580px] flex-wrap items-center gap-3 px-4 py-2 transition-[padding] sm:px-6 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
          <span className="flex size-8 items-center justify-center rounded-lg bg-emerald-600 text-xs font-semibold text-white shadow-sm">P</span>
          <div className="leading-tight">
            <h1 className="text-[14px] font-semibold tracking-tight text-slate-900">Emily · Biz OKR Plan</h1>
            <div className="mt-1 text-[10px] text-slate-400">{quarter.replace('-', ' ')} · 计划草稿</div>
          </div>
          {shared && <WeeklyShareNav currentTab="okr-plan" onChange={(tab) => onShareTabChange?.(tab)} />}
			{readOnly && <span className="rounded-full border border-amber-200 bg-amber-50 px-2 py-0.5 text-[10px] font-medium text-amber-700">只读</span>}
          <span className={`text-[10px] ${syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : syncState.kind === 'error' ? 'text-red-600' : 'text-slate-400'}`} aria-live="polite">{syncState.message}</span>
          <div className="ml-auto flex flex-wrap items-center gap-2">
            <QuarterSelect />
            <select aria-label="选择 Plan" value={plan?.id ?? ''} disabled={plans.length === 0 || saving} onChange={(event) => { setConfirmDelete(false); selectPlan(event.target.value) }} className="h-8 min-w-40 rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] text-slate-600 outline-none disabled:text-slate-400">
              {plans.length === 0 && <option value="">暂无 Plan</option>}
              {plans.map((item) => <option key={item.id} value={item.id}>{planOptionLabel(item.title, quarter)}</option>)}
            </select>
			{!readOnly && <ActivityLogButton surface="plan" quarter={quarter} planId={plan?.id} disabled={!plan} />}
			{!shared && <button type="button" onClick={() => void copyShareLink()} className="h-8 whitespace-nowrap rounded-lg border border-slate-200 bg-white px-2.5 text-[10px] font-medium text-slate-600 hover:border-slate-300 hover:bg-slate-50">分享 Plan 页</button>}
            <button
              type="button"
              disabled={!plan}
              onClick={toggleComments}
              aria-label={commentsOpen ? '关闭评论' : '打开全部评论'}
              className={`relative flex h-8 items-center gap-1.5 rounded-lg border px-2.5 text-[10px] font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${commentsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600 hover:border-indigo-200 hover:text-indigo-600'}`}
            >
              <span aria-hidden>💬</span><span>评论</span>
              {commentCount > 0 && <span className="min-w-4 rounded-full bg-indigo-600 px-1 text-center text-[9px] leading-4 text-white">{commentCount}</span>}
            </button>
			{!readOnly && <button type="button" onClick={() => { setConfirmDelete(false); setCreatingPlan((value) => !value) }} className="h-8 rounded-lg bg-emerald-600 px-3 text-[10px] font-medium text-white hover:bg-emerald-700">新建 Plan</button>}
			{!readOnly && <button type="button" disabled={!plan || saving} onClick={() => { setCreatingPlan(false); setConfirmDelete(true) }} className="h-8 rounded-lg border border-red-200 bg-red-50 px-3 text-[10px] font-medium text-red-700 hover:bg-red-100 disabled:opacity-40">删除 Plan</button>}
          </div>
        </div>
      </header>
      <main id={plan ? commentTargetElementId({ type: 'page', id: plan.id }) : undefined} className={`mx-auto max-w-[1580px] px-4 py-3 transition-[padding] sm:px-6 sm:py-4 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
		{shareNotice && <div className="mb-3 rounded-lg border border-blue-100 bg-blue-50 px-3 py-2 text-[10px] text-blue-700"><div>{shareNotice}</div>{shareLink && <input aria-label="分享链接" value={shareLink} readOnly onFocus={(event) => event.currentTarget.select()} onClick={(event) => event.currentTarget.select()} className="mt-2 h-8 w-full rounded-md border border-blue-200 bg-white px-2 text-[11px] text-slate-700 outline-none" />}</div>}
		{!readOnly && creatingPlan && <NewPlanPanel onClose={() => setCreatingPlan(false)} />}
		{!readOnly && confirmDelete && plan && (
          <section className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-red-200 bg-red-50 p-3">
            <div className="mr-2 flex-1">
              <div className="text-xs font-semibold text-red-800">确认删除 {plan.title}？</div>
              <div className="mt-0.5 text-[10px] text-red-600">只删除这个 Plan 草稿，不会删除正式 O、KR 或周报。</div>
            </div>
            <button type="button" onClick={() => { void deleteCurrentPlan().then(() => setConfirmDelete(false)) }} disabled={saving} className="h-9 rounded-lg bg-red-600 px-4 text-xs font-medium text-white disabled:opacity-40">确认删除</button>
            <button type="button" onClick={() => setConfirmDelete(false)} disabled={saving} className="h-9 px-2 text-xs text-slate-500 disabled:opacity-40">取消</button>
          </section>
        )}
        <SyncNotice />
        {plan ? (
          <CommentInteractionProvider value={{ enabled: true, triggerMode: 'surface', selected: commentTarget, focused: focusedComment, comments, counts: commentCounts, pendingSelection: pendingCommentSelection, setPendingSelection: setPendingCommentSelection, select: openComments }}>
            <div className={`transition-opacity ${saving ? 'pointer-events-none opacity-55' : ''}`}>
			  <ManagementView
                title=""
                subtitle=""
                showTags
                deleteKrWarning="只删除这个 Plan 草稿里的 KR"
				  hierarchyNavigation
				  hierarchyScopeKey={`${quarter}:${plan.id}`}
				  cardHierarchy
				  defaultExpandDetails
				  compactEmptyPointGroups
				  objectiveDragReorder
				  objectiveBusinessCategoryEditing
				  readOnly={readOnly}
              />
            </div>
          </CommentInteractionProvider>
        ) : (
          <section className="rounded-2xl border border-dashed border-slate-300 bg-white px-6 py-12 text-center">
            <div className="text-sm font-semibold text-slate-700">当前季度暂无 Biz OKR Plan</div>
            <div className="mt-1 text-xs text-slate-400">点击顶部“新建 Plan”开始规划。</div>
          </section>
        )}
      </main>
      {plan && <CommentDrawer open={commentsOpen} reviewEnabled reviewing={reviewingComments} quarter={quarter} planId={plan.id} sourceTab="okr-plan" scopeLabel={plan.title} objectives={plan.objectives} target={commentTarget} focusCommentId={initialCommentId} onStartReview={() => { setCommentTarget(undefined); setReviewingComments(true) }} onShowAll={() => { setReviewingComments(false); setFocusedComment(undefined); setCommentTarget(undefined) }} onClose={() => { setCommentsOpen(false); setReviewingComments(false); setFocusedComment(undefined) }} onFocusCommentChange={setFocusedComment} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />}
    </>
  )
}

export default function PlanApp({
  initialQuarter = '',
  initialPlanId = '',
  initialCommentId = '',
  onQuarterChange,
  shared = false,
	readOnly = false,
  onShareTabChange,
}: {
  initialQuarter?: string
  initialPlanId?: string
  initialCommentId?: string
  onQuarterChange?: (quarter: string) => void
  shared?: boolean
	readOnly?: boolean
  onShareTabChange?: (tab: WeeklyShareTab) => void
}) {
  return (
    <PlanBoardProvider initialQuarter={initialQuarter} initialPlanId={initialPlanId} onQuarterChange={onQuarterChange}>
		<PlanCanvas initialCommentId={initialCommentId} shared={shared} readOnly={readOnly} onShareTabChange={onShareTabChange} />
    </PlanBoardProvider>
  )
}
