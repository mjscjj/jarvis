import { useEffect, useState, type ReactNode } from 'react'
import { useBoard } from './board'
import { MeetingView } from './components/MeetingView'
import { KrTable } from './components/Table'
import { CommentDrawer } from './components/CommentDrawer'
import { WeeklyFocus } from './components/WeeklyFocus'
import { WeeklyTools } from './components/WeeklyTools'
import { PAGE_TITLE } from './seed'
import { CommentInteractionProvider } from './commenting'
import type { PendingCommentSelection } from './commenting'
import { commentTargetFromThread } from './comments'
import type { AuthStatus, CommentTarget, PageComment } from './types'
import { openWeeklyReportWeek } from './api'

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
  moduleTabs,
}: {
  auth: AuthStatus
  onLogout: () => void
  mode: 'fill' | 'meeting'
  onModeChange: (mode: 'fill' | 'meeting') => void
  moduleTabs: ReactNode
}) {
  const { reset, syncState, quarter, week, availableWeeks, setWeek } = useBoard()
  const [commentsOpen, setCommentsOpen] = useState(false)
  const [commentCount, setCommentCount] = useState(0)
  const [commentCounts, setCommentCounts] = useState<Record<string, number>>({})
  const [comments, setComments] = useState<PageComment[]>([])
  const [commentTarget, setCommentTarget] = useState<CommentTarget>()
  const [pendingCommentSelection, setPendingCommentSelection] = useState<PendingCommentSelection>()
  const [openingWeek, setOpeningWeek] = useState(false)
  const [newWeek, setNewWeek] = useState(currentISOWeek)
  const [weekNotice, setWeekNotice] = useState('')
  const busy = syncState.kind === 'loading'
  const tone = syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : 'text-slate-400'

  const submitWeek = async () => {
    const target = newWeek.trim()
    if (!quarter || !target) return
    setWeekNotice('')
    try {
      const result = await openWeeklyReportWeek({ quarter, week: target })
      setOpeningWeek(false)
      setWeekNotice(result.created ? `${target} 已开启` : `${target} 已经开启`)
      reset()
    } catch (cause) {
      setWeekNotice(cause instanceof Error ? cause.message : '开启周次失败。')
    }
  }

  useEffect(() => {
		if (mode !== 'meeting') {
			setCommentTarget(undefined)
		}
		setPendingCommentSelection(undefined)
	}, [mode])

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

  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur-md">
			<div className={`mx-auto flex min-h-14 max-w-[1320px] flex-wrap items-center gap-2.5 px-4 py-2 transition-[padding] sm:flex-nowrap sm:px-6 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
          <div className="mr-1 flex min-w-fit items-center gap-2">
					<span className="flex size-7 items-center justify-center rounded-lg bg-gradient-to-br from-blue-600 to-violet-600 text-[11px] font-bold text-white shadow-sm">E</span>
            <div className="leading-tight">
						<h1 className="text-[14px] font-semibold tracking-tight text-slate-900">{PAGE_TITLE.replace('OKR 协作台', '周报协作台')}</h1>
              <div className="mt-1 flex items-center gap-1.5 text-[10px] text-slate-400">
                <span>{quarter ? quarter.replace('-', ' ') : 'OKR'}</span><span className="text-slate-300">·</span>
							<span>{mode === 'fill' ? '周报填写' : '周报会议'}</span>
              </div>
            </div>
          </div>

	          <div className="flex h-8 items-center rounded-full border border-slate-200 bg-white px-2.5 text-[11px] shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
            <select aria-label="周次" value={week} onChange={(event) => setWeek(event.target.value)} className="bg-transparent font-medium text-slate-600 outline-none">
              {availableWeeks.map((item) => <option key={item} value={item}>{weekLabel(item)}</option>)}
            </select>
	          </div>
	          {mode === 'fill' && <button type="button" onClick={() => { setNewWeek(currentISOWeek()); setOpeningWeek((value) => !value); setWeekNotice('') }} className="h-8 rounded-lg border border-blue-200 bg-blue-50 px-2.5 text-[10px] font-medium text-blue-700 hover:bg-blue-100">开启新周</button>}

          <span className={`hidden min-w-16 text-[10px] sm:inline ${tone}`} aria-live="polite">{syncState.message}</span>

			<div className="ml-auto flex flex-wrap items-center justify-end gap-2.5">
			{moduleTabs}

			<>
                <span aria-hidden className="hidden h-5 w-px bg-slate-200 sm:block" />
                <button
                  type="button"
                  onClick={toggleComments}
                  aria-label={commentsOpen ? '关闭评论' : '打开全部评论'}
                  className={`relative flex h-9 items-center gap-1.5 rounded-xl border px-2.5 text-[11px] font-medium transition-colors ${commentsOpen ? 'border-indigo-200 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-600 shadow-[0_1px_2px_rgba(15,23,42,0.03)] hover:border-slate-300 hover:bg-slate-50'}`}
                >
                  <svg aria-hidden viewBox="0 0 20 20" className="size-4 fill-none stroke-current" strokeWidth="1.6">
                    <path d="M4.25 3.75h11.5A1.75 1.75 0 0 1 17.5 5.5v6.25a1.75 1.75 0 0 1-1.75 1.75H9l-4.5 3v-3h-.25a1.75 1.75 0 0 1-1.75-1.75V5.5a1.75 1.75 0 0 1 1.75-1.75Z" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                  <span>评论</span>
                  {commentCount > 0 && <span className="min-w-4 rounded-full bg-indigo-600 px-1 text-center text-[9px] leading-4 text-white">{commentCount}</span>}
                </button>
			</>
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
					<input value={quarter} readOnly aria-label="季度" className="h-9 w-28 rounded-lg border border-slate-200 bg-slate-100 px-3 text-xs text-slate-500" />
					<input value={newWeek} onChange={(event) => setNewWeek(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submitWeek() }} placeholder="2026-W36" aria-label="新周次" className="h-9 w-32 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-blue-400" />
					<button type="button" onClick={() => void submitWeek()} disabled={!newWeek.trim()} className="h-9 rounded-lg bg-blue-600 px-4 text-xs font-medium text-white disabled:opacity-40">确认开启</button>
					<button type="button" onClick={() => setOpeningWeek(false)} className="h-9 px-2 text-xs text-slate-400">取消</button>
				</section>}
				{weekNotice && <div className="mb-3 rounded-lg border border-blue-100 bg-blue-50 px-3 py-2 text-[10px] text-blue-700">{weekNotice}</div>}
				<SyncNotice />
			<WeeklyTools onOpenPoint={openPoint} />
			<>
            <CommentInteractionProvider value={{ selected: commentTarget, comments, counts: commentCounts, pendingSelection: pendingCommentSelection, setPendingSelection: setPendingCommentSelection, select: openComments }}>
              <WeeklyFocus comments={comments} onOpenComment={(comment) => openComments(commentTargetFromThread(comment))} />
              <div className={`transition-opacity ${busy ? 'pointer-events-none opacity-55' : ''}`}>
                {mode === 'meeting' ? <MeetingView /> : <KrTable definitionsReadOnly />}
              </div>
            </CommentInteractionProvider>
            <div className="mt-3 px-1 text-[11px] text-slate-400">
              {mode === 'fill' ? '停止输入后自动保存；多人修改同一条 KR 时会先请你确认。' : '会议模式沿用同一份数据，只读投屏并保留评论与飞书导出。'}
              <button type="button" onClick={reset} className="ml-1 underline hover:text-slate-600">重新载入</button>
            </div>
			</>
		</main>
		<CommentDrawer open={commentsOpen} quarter={quarter} week={week} target={commentTarget} meetingMode={mode === 'meeting'} canComment onSignIn={() => undefined} onShowAll={() => setCommentTarget(undefined)} onClose={() => setCommentsOpen(false)} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />
    </div>
  )
}
