import { Modal } from 'antd'
import { useEffect, useState } from 'react'
import { useBoard } from './board'
import { MeetingView } from './components/MeetingView'
import { KrTable } from './components/Table'
import { CommentDrawer } from './components/CommentDrawer'
import { HelpFab } from './components/HelpFab'
import { WeeklyFocus } from './components/WeeklyFocus'
import { WeeklyTools } from './components/WeeklyTools'
import { QuarterSelect } from './components/QuarterSelect'
import { CommentInteractionProvider } from './commenting'
import type { PendingCommentSelection } from './commenting'
import { commentTargetFromThread } from './comments'
import type { CommentTarget, PageComment } from './types'
import { openWeeklyReportWeek } from './api'
import { getWebConfig } from '../../api'
import { weeklyShareURL, type WeeklyShareTab } from './share'
import { okrTabForWeeklyWorkspace, weeklyDatasetLabel, weeklyViewLabel, weeklyWorkspace, type WeeklyWorkspace } from '../navigation'
import { WeeklyShareNav } from './components/WeeklyShareNav'
import { templateKeyForDataset } from './weekCatalog'
import { ActivityLogButton } from './components/ActivityLogButton'

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
  workspace,
  onWorkspaceChange,
  onShareTabChange,
  shared = false,
}: {
	workspace: WeeklyWorkspace
	onWorkspaceChange: (workspace: WeeklyWorkspace) => void
	onShareTabChange?: (tab: WeeklyShareTab) => void
  shared?: boolean
}) {
	const { dataset, view } = workspace
	const { reset, syncState, quarter, week, availableWeeks, setWeek, setWeeklyScope, deleteWeeklyScope } = useBoard()
  const [commentsOpen, setCommentsOpen] = useState(false)
  const [commentCount, setCommentCount] = useState(0)
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({})
  const [comments, setComments] = useState<PageComment[]>([])
  const [commentTarget, setCommentTarget] = useState<CommentTarget>()
  const [pendingCommentSelection, setPendingCommentSelection] = useState<PendingCommentSelection>()
  const [openingWeek, setOpeningWeek] = useState(false)
  const [newQuarter, setNewQuarter] = useState(currentQuarter)
  const [newWeek, setNewWeek] = useState(currentISOWeek)
  const [weekNotice, setWeekNotice] = useState('')
  const [shareNotice, setShareNotice] = useState('')
  const [shareLink, setShareLink] = useState('')
	const [confirmDeleteWeek, setConfirmDeleteWeek] = useState(false)
	const [deletingWeek, setDeletingWeek] = useState(false)
  const busy = syncState.kind === 'loading'
	const deleteBlocked = deletingWeek || syncState.kind === 'loading' || syncState.kind === 'saving' || syncState.kind === 'conflict'
	// The dataset drives page chrome; it cannot read the board's template key
	// because an empty quarter has no board yet and would fall back to classic.
	// Content format (score, AI review, single vs split progress lanes) reads the
	// loaded week's template instead, inside the views that render it.
	const reviewDataset = dataset === 'review'
	const meetingLike = view === 'meeting'
	const managesWeeks = view === 'fill'
	const lifecycleName = weeklyDatasetLabel(dataset)
	const pageTitle = reviewDataset ? 'Emily · Biz OKR Review' : 'Emily · Biz OKR 周报协作台'
	const shareLabel = `${lifecycleName}${weeklyViewLabel(view)}`

  const submitWeek = async () => {
    const target = newWeek.trim()
    const targetQuarter = newQuarter.trim()
    if (!targetQuarter || !target) return
    setWeekNotice('')
    try {
      const result = await openWeeklyReportWeek({ quarter: targetQuarter, week: target, templateKey: templateKeyForDataset(dataset) })
      setOpeningWeek(false)
			const switched = setWeeklyScope(targetQuarter, target)
			const opened = result.created ? `${target} 已开启` : `${target} 已经开启`
			setWeekNotice(switched ? opened : `${opened}；当前修改保存后请重新选择该周。`)
    } catch (cause) {
      setWeekNotice(cause instanceof Error ? cause.message : `新建${lifecycleName}失败。`)
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
				: `${deletedWeek} 已删除；当前季度暂无${lifecycleName}，请先新建。`)
		} catch (cause) {
			setWeekNotice(cause instanceof Error ? cause.message : `删除${lifecycleName}失败。`)
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
		onWorkspaceChange({ dataset, view: 'fill' })
	}

	useEffect(() => {
		const pointId = sessionStorage.getItem('jarvis.weekly-report.focus-point')
		if (!pointId || busy || view !== 'fill') return
		sessionStorage.removeItem('jarvis.weekly-report.focus-point')
		window.setTimeout(() => document.getElementById(`point-${pointId}`)?.scrollIntoView({ behavior: 'smooth', block: 'center' }), 0)
	}, [busy, view])

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
    let link: string
    try {
      const config = await getWebConfig()
      link = weeklyShareURL(window.location.href, workspace, config.public_base_url)
    } catch (cause) {
      setShareNotice(cause instanceof Error ? `读取分享地址失败：${cause.message}` : '读取分享地址失败')
      return
    }
    if (!navigator.clipboard) {
      setShareLink(link)
      setShareNotice('当前是 HTTP 页面，请复制下面的分享链接')
      return
    }
    try {
      await navigator.clipboard.writeText(link)
      setShareNotice(`${shareLabel}页面链接已复制`)
    } catch (cause) {
      setShareLink(link)
      setShareNotice(cause instanceof Error ? `自动复制失败：${cause.message}；请复制下面的链接` : '自动复制失败，请复制下面的链接')
    }
  }

  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur-md">
			{/* The chrome wraps onto a second row when it runs out of width; squeezing
			    it onto one line instead breaks the button labels mid-word. */}
			<div className={`mx-auto flex min-h-14 max-w-[1320px] flex-wrap items-center gap-2.5 px-4 py-2 transition-[padding] sm:px-6 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
          <div className="mr-1 flex min-w-fit items-center gap-2">
					<span className="flex size-7 items-center justify-center rounded-lg bg-gradient-to-br from-blue-600 to-violet-600 text-[11px] font-bold text-white shadow-sm">E</span>
						<h1 className="text-[14px] font-semibold tracking-tight text-slate-900">{pageTitle}</h1>
          </div>

		          {shared && <WeeklyShareNav currentTab={okrTabForWeeklyWorkspace(workspace)} onChange={(tab) => {
					if (onShareTabChange) {
						onShareTabChange(tab)
						return
					}
					if (tab !== 'okr-plan') onWorkspaceChange(weeklyWorkspace(tab))
				}} />}
		          <QuarterSelect />
		          <div className="flex h-8 items-center rounded-full border border-slate-200 bg-white px-2.5 text-[11px] shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
            <select aria-label="周次" value={week} disabled={availableWeeks.length === 0} onChange={(event) => { setConfirmDeleteWeek(false); setWeek(event.target.value) }} className="bg-transparent font-medium text-slate-600 outline-none disabled:text-slate-400">
						{availableWeeks.length === 0 && <option value="">暂无周次</option>}
              {availableWeeks.map((item) => <option key={item} value={item}>{weekLabel(item)}</option>)}
            </select>
	          </div>
		          {reviewDataset && <span className="rounded-full border border-violet-200 bg-violet-50 px-2 py-1 text-[9px] font-semibold uppercase tracking-wide text-violet-700">Preview</span>}
	          {managesWeeks && <button type="button" onClick={() => { setConfirmDeleteWeek(false); setNewQuarter(quarter || currentQuarter()); setNewWeek(currentISOWeek()); setOpeningWeek((value) => !value); setWeekNotice('') }} className={`h-8 whitespace-nowrap rounded-lg border px-2.5 text-[10px] font-medium ${reviewDataset ? 'border-violet-200 bg-violet-50 text-violet-700 hover:bg-violet-100' : 'border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100'}`}>新建{lifecycleName}</button>}
	          {managesWeeks && <button type="button" disabled={!week || deleteBlocked} onClick={() => { setConfirmDeleteWeek(true); setOpeningWeek(false); setWeekNotice('') }} className="h-8 whitespace-nowrap rounded-lg border border-red-200 bg-red-50 px-2.5 text-[10px] font-medium text-red-700 hover:bg-red-100 disabled:opacity-40">删除{lifecycleName}</button>}
			<div className="ml-auto flex flex-wrap items-center justify-end gap-2.5">
              <ActivityLogButton surface="weekly" quarter={quarter} week={week} disabled={!week} />
              <button type="button" onClick={() => void copyShareLink()} className="flex h-9 items-center whitespace-nowrap rounded-xl border border-slate-200 bg-white px-2.5 text-[11px] font-medium text-slate-600 shadow-[0_1px_2px_rgba(15,23,42,0.03)] hover:border-slate-300 hover:bg-slate-50">
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
        </div>
      </header>

			<main className={`mx-auto max-w-[1320px] px-4 py-4 transition-[padding] sm:px-6 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
				{openingWeek && <section className={`mb-3 flex flex-wrap items-center gap-2 rounded-xl border p-3 ${reviewDataset ? 'border-violet-100 bg-violet-50/60' : 'border-blue-100 bg-blue-50/60'}`}>
					<div className="mr-2"><div className="text-xs font-semibold text-slate-700">新建{lifecycleName}周次</div><div className="mt-0.5 text-[10px] text-slate-400">{reviewDataset ? '只创建空 Review 周，不会影响普通周报。' : '只创建空周，不复制进展，也不会立即发送提醒。'}</div></div>
					<input value={newQuarter} onChange={(event) => setNewQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-9 w-28 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-blue-400" />
					<input value={newWeek} onChange={(event) => setNewWeek(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submitWeek() }} placeholder="2026-W36" aria-label="新周次" className="h-9 w-32 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-blue-400" />
					<button type="button" onClick={() => void submitWeek()} disabled={!newQuarter.trim() || !newWeek.trim()} className={`h-9 rounded-lg px-4 text-xs font-medium text-white disabled:opacity-40 ${reviewDataset ? 'bg-violet-600' : 'bg-blue-600'}`}>确认新建</button>
					<button type="button" onClick={() => setOpeningWeek(false)} className="h-9 px-2 text-xs text-slate-400">取消</button>
				</section>}
				{/* 删除周次不可撤销，而分享链接的收件人也能点到这个按钮，所以确认
				    走模态弹窗而不是行内提示条：弹窗挡住页面，点不穿过去。 */}
				<Modal
					title={`确认删除 ${quarter} / ${week} 的${lifecycleName}？`}
					open={confirmDeleteWeek && Boolean(week)}
					onCancel={() => setConfirmDeleteWeek(false)}
					closable={!deletingWeek}
					maskClosable={!deletingWeek}
					keyboard={!deletingWeek}
					footer={null}
					centered
					width={460}
				>
					<p className="mt-2 text-sm leading-6 text-slate-600">会删除这个周次的周度数据；不会删除 O、KR、指标定义和拆解，也不会影响其他周。删除后无法恢复。</p>
					<div className="mt-4 flex justify-end gap-2">
						<button type="button" onClick={() => setConfirmDeleteWeek(false)} disabled={deletingWeek} className="rounded-lg px-4 py-2 text-sm font-medium text-slate-500 hover:bg-slate-50 disabled:opacity-40">取消</button>
						<button type="button" onClick={() => void removeWeek()} disabled={deletingWeek} className="rounded-lg bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-40">{deletingWeek ? '正在删除…' : `确认删除 ${week}`}</button>
					</div>
				</Modal>
				{weekNotice && <div className="mb-3 rounded-lg border border-blue-100 bg-blue-50 px-3 py-2 text-[10px] text-blue-700">{weekNotice}</div>}
				{shareNotice && <div className="mb-3 rounded-lg border border-blue-100 bg-blue-50 px-3 py-2 text-[10px] text-blue-700">
					<div>{shareNotice}</div>
					{shareLink && <input aria-label="分享链接" value={shareLink} readOnly autoFocus onFocus={(event) => event.currentTarget.select()} onClick={(event) => event.currentTarget.select()} className="mt-2 h-8 w-full rounded-md border border-blue-200 bg-white px-2 text-[11px] text-slate-700 outline-none" />}
				</div>}
				<SyncNotice />
				{week ? <>
					{/* Meego 差异和催办预览是维护动作，分享链接的收件人只负责填写，
					    不该看到它们。 */}
					{!shared && <WeeklyTools onOpenPoint={openPoint} readOnly={view !== 'fill'} />}
            <CommentInteractionProvider value={{ selected: commentTarget, comments, counts: commentCounts, pendingSelection: pendingCommentSelection, setPendingSelection: setPendingCommentSelection, select: openComments }}>
              <WeeklyFocus comments={comments} readOnly={view !== 'fill'} statusEditable={reviewDataset && meetingLike} onOpenComment={(comment) => openComments(commentTargetFromThread(comment))} />
              <div className={`transition-opacity ${busy ? 'pointer-events-none opacity-55' : ''}`}>
						{view === 'meeting' ? <MeetingView /> : <KrTable definitionsReadOnly showObjectiveHeader />}
              </div>
            </CommentInteractionProvider>
            <div className="mt-3 px-1 text-[11px] text-slate-400">
							{view === 'fill'
								? '停止输入后自动保存；多人修改同一条 KR 时会先请你确认。'
								: reviewDataset
										? 'Review 会议中可以直接修改评分和 Todo 状态；AI 评审在“Review 填写”里。'
									: '会议模式沿用同一份数据，只读投屏并保留评论与飞书导出。'}
              <button type="button" onClick={reset} className="ml-1 underline hover:text-slate-600">重新载入</button>
            </div>
			</> : <section className="rounded-2xl border border-dashed border-slate-300 bg-white px-6 py-12 text-center"><div className="text-sm font-semibold text-slate-700">当前季度暂无{lifecycleName}</div><div className="mt-1 text-xs text-slate-400">{managesWeeks ? `点击顶部“新建${lifecycleName}”创建一个空周。` : `请先在“${lifecycleName}填写”中新建一个空周。`}</div></section>}
		</main>
			{week && <CommentDrawer open={commentsOpen} quarter={quarter} week={week} target={commentTarget} meetingMode={meetingLike} onShowAll={() => setCommentTarget(undefined)} onClose={() => setCommentsOpen(false)} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />}
			<HelpFab />
    </div>
  )
}
