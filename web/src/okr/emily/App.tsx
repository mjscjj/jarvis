import { useEffect, useState } from 'react'
import { useBoard } from './board'
import { MeetingView } from './components/MeetingView'
import { KrTable } from './components/Table'
import { CommentDrawer } from './components/CommentDrawer'
import { WeeklyFocus } from './components/WeeklyFocus'
import { WeeklyTools } from './components/WeeklyTools'
import { QuarterSelect } from './components/QuarterSelect'
import { PAGE_TITLE } from './seed'
import { CommentInteractionProvider } from './commenting'
import type { PendingCommentSelection } from './commenting'
import { commentTargetFromThread } from './comments'
import type { AuthStatus, CommentTarget, PageComment } from './types'
import { openWeeklyReportWeek } from './api'
import { weeklyShareURL } from './share'
import type { WeeklyWorkspaceMode } from '../navigation'

function weekLabel(week: string): string {
  const matched = /^(\d{4})-W(\d{2})$/.exec(week)
  if (!matched) return week
  const [, yearText, weekText] = matched
  const jan4 = new Date(Date.UTC(Number(yearText), 0, 4))
  const monday = new Date(jan4)
  monday.setUTCDate(jan4.getUTCDate() - (jan4.getUTCDay() || 7) + 1 + (Number(weekText) - 1) * 7)
  const sunday = new Date(monday)
  sunday.setUTCDate(monday.getUTCDate() + 6)
  const short = (date: Date) => `${date.getUTCMonth() + 1}.${date.getUTCDate()}`
  return `W${weekText} · ${short(monday)} – ${short(sunday)}`
}

function currentISOWeek(): string {
  const now = new Date()
  const date = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()))
  const weekday = date.getUTCDay() || 7
  date.setUTCDate(date.getUTCDate() + 4 - weekday)
  const yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1))
  const number = Math.ceil((((date.getTime() - yearStart.getTime()) / 86400000) + 1) / 7)
  return `${date.getUTCFullYear()}-W${String(number).padStart(2, '0')}`
}

function currentQuarter(): string {
  const now = new Date()
  return `${now.getFullYear()}-Q${Math.floor(now.getMonth() / 3) + 1}`
}

function SyncNotice() {
  const { syncState, retry, resolveConflict } = useBoard()

  if (syncState.kind === 'conflict') {
    return (
      <div className="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-900">
        <span className="min-w-64 flex-1">{syncState.message}</span>
        <button type="button" onClick={() => resolveConflict('remote')} className="rounded-md border border-amber-300 bg-white px-2.5 py-1 hover:bg-amber-100">载入他人版本</button>
        <button type="button" onClick={() => resolveConflict('local')} className="rounded-md bg-amber-700 px-2.5 py-1 text-white hover:bg-amber-800">保留我的修改</button>
      </div>
    )
  }

  if (syncState.kind === 'error') {
    return (
      <div className="mb-3 flex items-center gap-2 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-800">
        <span className="flex-1">{syncState.message}</span>
        <button type="button" onClick={retry} className="rounded-md border border-red-200 bg-white px-2.5 py-1 hover:bg-red-100">重试</button>
      </div>
    )
  }
  return null
}

export default function App({
  auth,
  onLogout,
  mode,
  onModeChange,
  shared = false,
}: {
  auth: AuthStatus
  onLogout: () => void
	mode: WeeklyWorkspaceMode
	onModeChange: (mode: WeeklyWorkspaceMode) => void
  shared?: boolean
}) {
	const { reset, syncState, quarter, week, templateKey, availableWeeks, setWeek, setWeeklyScope, deleteWeeklyScope } = useBoard()
  const [commentsOpen, setCommentsOpen] = useState(false)
  const [commentCount, setCommentCount] = useState(0)
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({})
  const [comments, setComments] = useState<PageComment[]>([])
  const [commentTarget, setCommentTarget] = useState<CommentTarget>()
  const [pendingCommentSelection, setPendingCommentSelection] = useState<PendingCommentSelection>()
  const [openingWeek, setOpeningWeek] = useState(false)
  const [newQuarter, setNewQuarter] = useState(currentQuarter)
  const [newWeek, setNewWeek] = useState(currentISOWeek)
  const [newWeekPreview, setNewWeekPreview] = useState(false)
  const [weekNotice, setWeekNotice] = useState('')
  const [shareNotice, setShareNotice] = useState('')
  const [shareLink, setShareLink] = useState('')
	const [confirmDeleteWeek, setConfirmDeleteWeek] = useState(false)
	const [deletingWeek, setDeletingWeek] = useState(false)
  const busy = syncState.kind === 'loading'
	const deleteBlocked = deletingWeek || syncState.kind === 'loading' || syncState.kind === 'saving' || syncState.kind === 'conflict'
	const previewWeek = templateKey === 'okr_weekly_preview_v1'
	const meetingLike = mode === 'meeting' || mode === 'review'
	const pageTitle = mode === 'review' ? PAGE_TITLE.replace('OKR 协作台', 'OKR Review') : PAGE_TITLE.replace('OKR 协作台', '周报协作台')
	const shareLabel = mode === 'review' ? 'Review' : mode === 'meeting' ? '会议' : '填写'

  const submitWeek = async () => {
    const target = newWeek.trim()
    const targetQuarter = newQuarter.trim()
    if (!targetQuarter || !target) return
    setWeekNotice('')
    try {
      const result = await openWeeklyReportWeek({ quarter: targetQuarter, week: target, templateKey: newWeekPreview ? 'okr_weekly_preview_v1' : 'classic' })
      setOpeningWeek(false)
			const switched = setWeeklyScope(targetQuarter, target)
			const opened = result.created ? `${target} 已开启` : `${target} 已经开启`
			setWeekNotice(switched ? opened : `${opened}；当前修改保存后请重新选择该周。`)
    } catch (cause) {
      setWeekNotice(cause instanceof Error ? cause.message : '开启周次失败。')
    }
  }

	const removeWeek = async () => {
		if (!week) return
		setDeletingWeek(true)
		setWeekNotice('')
		try {
			const deletedWeek = week
			const result = await deleteWeeklyScope()
			setConfirmDeleteWeek(false)
			setCommentsOpen(false)
			setComments([])
			setCommentCount(0)
			setCommentCounts({})
			setWeekNotice(result.nextWeek
				? `${deletedWeek} 已删除，已切换到 ${result.nextWeek}。`
				: `${deletedWeek} 已删除；当前季度暂无周报，请先开启新周。`)
		} catch (cause) {
			setWeekNotice(cause instanceof Error ? cause.message : '删除本周失败。')
		} finally {
			setDeletingWeek(false)
		}
	}

  useEffect(() => {
			if (!meetingLike) {
				setCommentTarget(undefined)
			}
			setPendingCommentSelection(undefined)
		}, [meetingLike])

	useEffect(() => setConfirmDeleteWeek(false), [quarter, week])

  useEffect(() => {
    const clearPendingSelection = (event: Event) => {
      const target = event.target
      if (target instanceof Element && target.closest('[data-comment-selection-trigger]')) return
      setPendingCommentSelection(undefined)
    }
    const clearOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setPendingCommentSelection(undefined)
    }
    const clearOnBlur = () => setPendingCommentSelection(undefined)
    document.addEventListener('pointerdown', clearPendingSelection)
    document.addEventListener('focusin', clearPendingSelection)
    document.addEventListener('keydown', clearOnEscape)
    window.addEventListener('blur', clearOnBlur)
    return () => {
      document.removeEventListener('pointerdown', clearPendingSelection)
      document.removeEventListener('focusin', clearPendingSelection)
      document.removeEventListener('keydown', clearOnEscape)
      window.removeEventListener('blur', clearOnBlur)
    }
  }, [])

	const openPoint = (pointId: string) => {
		sessionStorage.setItem('jarvis.weekly-report.focus-point', pointId)
		onModeChange('fill')
	}

	useEffect(() => {
		const pointId = sessionStorage.getItem('jarvis.weekly-report.focus-point')
		if (!pointId || busy || mode !== 'fill') return
		sessionStorage.removeItem('jarvis.weekly-report.focus-point')
		window.setTimeout(() => document.getElementById(`point-${pointId}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 0)
	}, [busy, mode])

  const openComments = (target?: CommentTarget) => {
    setPendingCommentSelection(undefined)
    window.getSelection()?.removeAllRanges()
    setCommentTarget(target)
    setCommentsOpen(true)
  }

  const toggleComments = () => {
    if (commentsOpen) setCommentsOpen(false)
    else openComments()
  }

  const copyShareLink = async () => {
    setShareNotice('')
    setShareLink('')
    const link = weeklyShareURL(window.location.href, mode)
    if (!navigator.clipboard) {
      setShareLink(link)
      setShareNotice('当前是 HTTP 页面，请复制下面的分享链接')
      return
    }
    try {
      await navigator.clipboard.writeText(link)
      setShareNotice(`${mode === 'meeting' ? '会议' : '填写'}页面链接已复制`)
    } catch (cause) {
      setShareLink(link)
      setShareNotice(cause instanceof Error ? `自动复制失败：${cause.message}；请复制下面的链接` : '自动复制失败，请复制下面的链接')
    }
  }

  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur-md">
			<div className={`mx-auto flex min-h-14 max-w-[1320px] flex-wrap items-center gap-2.5 px-4 py-2 transition-[padding] sm:flex-nowrap sm:px-6 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
          <div className="mr-1 flex min-w-fit items-center gap-2">
					<span className="flex size-7 items-center justify-center rounded-lg bg-gradient-to-br from-blue-600 to-violet-600 text-[11px] font-bold text-white shadow-sm">E</span>
						<h1 className="text-[14px] font-semibold tracking-tight text-slate-900">{pageTitle}</h1>
          </div>

		          {shared && <nav aria-label="周报页面" className="flex h-9 items-center rounded-xl border border-slate-200 bg-slate-50 p-1">
					<button type="button" aria-current={mode === 'fill' ? 'page' : undefined} onClick={() => onModeChange('fill')} className={`h-7 rounded-lg px-3 text-[11px] font-medium ${mode === 'fill' ? 'bg-white text-blue-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'}`}>周报填写</button>
						<button type="button" aria-current={mode === 'meeting' ? 'page' : undefined} onClick={() => onModeChange('meeting')} className={`h-7 rounded-lg px-3 text-[11px] font-medium ${mode === 'meeting' ? 'bg-white text-blue-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'}`}>周报会议</button>
						<button type="button" aria-current={mode === 'review' ? 'page' : undefined} onClick={() => onModeChange('review')} className={`h-7 rounded-lg px-3 text-[11px] font-medium ${mode === 'review' ? 'bg-white text-violet-700 shadow-sm' : 'text-slate-500 hover:text-slate-700'}`}>OKR Review</button>
		          </nav>}
		          <QuarterSelect />
		          <div className="flex h-8 items-center rounded-full border border-slate-200 bg-white px-2.5 text-[11px] shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
            <select aria-label="周次" value={week} disabled={availableWeeks.length === 0} onChange={(event) => { setConfirmDeleteWeek(false); setWeek(event.target.value) }} className="bg-transparent font-medium text-slate-600 outline-none disabled:text-slate-400">
						{availableWeeks.length === 0 && <option value="">暂无周次</option>}
              {availableWeeks.map((item) => <option key={item} value={item}>{weekLabel(item)}</option>)}
            </select>
	          </div>
		          {mode === 'review' && previewWeek && <span className="rounded-full border border-violet-200 bg-violet-50 px-2 py-1 text-[9px] font-semibold uppercase tracking-wide text-violet-700">Preview</span>}
	          {mode === 'fill' && <button type="button" onClick={() => { setConfirmDeleteWeek(false); setNewQuarter(quarter || currentQuarter()); setNewWeek(currentISOWeek()); setNewWeekPreview(false); setOpeningWeek((value) => !value); setWeekNotice('') }} className="h-8 rounded-lg border border-blue-200 bg-blue-50 px-2.5 text-[10px] font-medium text-blue-700 hover:bg-blue-100">开启新周</button>}
	          {mode === 'fill' && <button type="button" disabled={!week || deleteBlocked} onClick={() => { setConfirmDeleteWeek(true); setOpeningWeek(false); setWeekNotice('') }} className="h-8 rounded-lg border border-red-200 bg-red-50 px-2.5 text-[10px] font-medium text-red-700 hover:bg-red-100 disabled:opacity-40">删除本周</button>}
			<div className="ml-auto flex flex-wrap items-center justify-end gap-2.5">
              <button type="button" onClick={() => void copyShareLink()} className="flex h-9 items-center rounded-xl border border-slate-200 bg-white px-2.5 text-[11px] font-medium text-slate-600 shadow-[0_1px_2px_rgba(15,23,42,0.03)] hover:border-slate-300 hover:bg-slate-50">
                分享{shareLabel}页
              </button>
              <span aria-hidden className="hidden h-5 w-px bg-slate-200 sm:block" />
              <button
                type="button"
                onClick={toggleComments}
				disabled={!week}
                aria-label={commentsOpen ? '关闭评论' : '打开全部评论'}
				className={`relative flex h-9 items-center gap-1.5 rounded-xl border px-2.5 text-[11px] font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-40 ${commentsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600 shadow-[0_1px_2px_rgba(15,23,42,0.03)] hover:border-slate-300 hover:bg-slate-50'}`}
              >
                <svg aria-hidden viewBox="0 0 20 20" className="size-4 fill-none stroke-current" strokeWidth="1.6">
                  <path d="M4.25 3.75h11.5A1.75 1.75 0 0 1 17.5 5.5v6.25a1.75 1.75 0 0 1-1.75 1.75H9l-4.5 3v-3h-.25a1.75 1.75 0 0 1-1.75-1.75V5.5a1.75 1.75 0 0 1 1.75-1.75Z" strokeLinecap="round" strokeLinejoin="round" />
                </svg>
                <span>评论</span>
                {commentCount > 0 && <span className="min-w-4 rounded-full bg-indigo-600 px-1 text-center text-[9px] leading-4 text-white">{commentCount}</span>}
              </button>
          </div>
          {auth.user && <div className="hidden items-center gap-1.5 text-[11px] text-slate-500 xl:flex">
            {auth.user.avatarUrl ? <img src={auth.user.avatarUrl} alt="" className="size-6 rounded-full" /> : <span className="flex size-6 items-center justify-center rounded-full bg-slate-100 text-[10px]">{auth.user.name.slice(0, 1)}</span>}
            <span>{auth.user.name}</span>
            {auth.configured && <button type="button" onClick={onLogout} className="ml-1 text-slate-400 hover:text-slate-700">退出</button>}
          </div>}
        </div>
      </header>

			<main className={`mx-auto max-w-[1320px] px-4 py-4 transition-[padding] sm:px-6 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
				{openingWeek && <section className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-blue-100 bg-blue-50/60 p-3">
					<div className="mr-2"><div className="text-xs font-semibold text-slate-700">开启周报周次</div><div className="mt-0.5 text-[10px] text-slate-400">只创建空周，不复制进展，也不会立即发送提醒。</div></div>
					<input value={newQuarter} onChange={(event) => setNewQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-9 w-28 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-blue-400" />
					<input value={newWeek} onChange={(event) => setNewWeek(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submitWeek() }} placeholder="2026-W36" aria-label="新周次" className="h-9 w-32 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-blue-400" />
					<label className="flex h-9 items-center gap-2 rounded-lg border border-violet-200 bg-white px-3 text-[11px] text-slate-600"><input type="checkbox" checked={newWeekPreview} onChange={(event) => setNewWeekPreview(event.target.checked)} className="size-4 accent-violet-600" /><span><b className="font-medium text-violet-700">OKR 周度 Preview</b><span className="ml-1 text-[9px] text-slate-400">评分 + 合并进展</span></span></label>
					<button type="button" onClick={() => void submitWeek()} disabled={!newQuarter.trim() || !newWeek.trim()} className="h-9 rounded-lg bg-blue-600 px-4 text-xs font-medium text-white disabled:opacity-40">确认开启</button>
					<button type="button" onClick={() => setOpeningWeek(false)} className="h-9 px-2 text-xs text-slate-400">取消</button>
				</section>}
				{confirmDeleteWeek && week && <section className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-red-200 bg-red-50 p-3">
					<div className="mr-2 flex-1"><div className="text-xs font-semibold text-red-800">确认删除 {quarter} / {week} 的整周周报？</div><div className="mt-0.5 text-[10px] text-red-600">会删除本周进展、周度指标、评论、Meego 快照和催填批次；不会删除 O、KR、指标定义和拆解，也不会影响其他周。</div></div>
					<button type="button" onClick={() => void removeWeek()} disabled={deletingWeek} className="h-9 rounded-lg bg-red-600 px-4 text-xs font-medium text-white disabled:opacity-40">{deletingWeek ? '正在删除…' : `确认删除 ${week}`}</button>
					<button type="button" onClick={() => setConfirmDeleteWeek(false)} disabled={deletingWeek} className="h-9 px-2 text-xs text-slate-500 disabled:opacity-40">取消</button>
				</section>}
				{weekNotice && <div className="mb-3 rounded-lg border border-blue-100 bg-blue-50 px-3 py-2 text-[10px] text-blue-700">{weekNotice}</div>}
				{shareNotice && <div className="mb-3 rounded-lg border border-blue-100 bg-blue-50 px-3 py-2 text-[10px] text-blue-700">
					<div>{shareNotice}</div>
					{shareLink && <input aria-label="分享链接" value={shareLink} readOnly autoFocus onFocus={(event) => event.currentTarget.select()} onClick={(event) => event.currentTarget.select()} className="mt-2 h-8 w-full rounded-md border border-blue-200 bg-white px-2 text-[11px] text-slate-700 outline-none" />}
				</div>}
				<SyncNotice />
				{week ? <>
					<WeeklyTools onOpenPoint={openPoint} readOnly={mode !== 'fill'} />
            <CommentInteractionProvider value={{ selected: commentTarget, comments, counts: commentCounts, pendingSelection: pendingCommentSelection, setPendingSelection: setPendingCommentSelection, select: openComments }}>
              <WeeklyFocus comments={comments} onOpenComment={(comment) => openComments(commentTargetFromThread(comment))} />
              <div className={`transition-opacity ${busy ? 'pointer-events-none opacity-55' : ''}`}>
						{mode === 'review'
							? previewWeek
								? <MeetingView reviewMode />
								: <section className="rounded-2xl border border-dashed border-violet-200 bg-violet-50/40 px-6 py-12 text-center"><div className="text-sm font-semibold text-violet-800">当前周次未开启 OKR Preview</div><div className="mt-1 text-xs text-violet-500">请在“周报填写”的“开启新周”中勾选 OKR 周度 Preview。</div></section>
							: mode === 'meeting'
								? <MeetingView />
								: <KrTable definitionsReadOnly showObjectiveHeader />}
              </div>
            </CommentInteractionProvider>
            <div className="mt-3 px-1 text-[11px] text-slate-400">
							{mode === 'fill'
								? '停止输入后自动保存；多人修改同一条 KR 时会先请你确认。'
								: mode === 'review'
									? 'Review 沿用同一份周报数据；评分独立保存，进度只读展示。'
									: '会议模式沿用同一份数据，只读投屏并保留评论与飞书导出。'}
              <button type="button" onClick={reset} className="ml-1 underline hover:text-slate-600">重新载入</button>
            </div>
			</> : <section className="rounded-2xl border border-dashed border-slate-300 bg-white px-6 py-12 text-center"><div className="text-sm font-semibold text-slate-700">当前季度暂无周报</div><div className="mt-1 text-xs text-slate-400">点击顶部“开启新周”创建一个空周后即可开始填写。</div></section>}
		</main>
			{week && <CommentDrawer open={commentsOpen} quarter={quarter} week={week} target={commentTarget} meetingMode={meetingLike} canComment onSignIn={() => undefined} onShowAll={() => setCommentTarget(undefined)} onClose={() => setCommentsOpen(false)} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />}
    </div>
  )
}
