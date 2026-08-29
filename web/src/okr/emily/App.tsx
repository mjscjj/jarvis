import { useEffect, useState, type ReactNode } from 'react'
import { useBoard } from './board'
import { MeetingView } from './components/MeetingView'
import { KrTable } from './components/Table'
import { CommentDrawer } from './components/CommentDrawer'
import { WeeklyTools } from './components/WeeklyTools'
import { PAGE_TITLE } from './seed'
import { CommentInteractionProvider } from './commenting'
import type { AuthStatus, CommentTarget, PageComment } from './types'

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
  const busy = syncState.kind === 'loading'
  const tone = syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : 'text-slate-400'

  useEffect(() => {
		if (mode !== 'meeting') {
			setCommentTarget(undefined)
		}
	}, [mode])

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
    setCommentTarget(target)
    setCommentsOpen(true)
  }

  const toggleComments = () => {
    if (commentsOpen) setCommentsOpen(false)
    else openComments()
  }

  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
			<div className={`mx-auto flex min-h-14 max-w-[1580px] flex-wrap items-center gap-2 px-4 py-2 transition-[padding] sm:flex-nowrap sm:gap-3 sm:px-6 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
          <div className="mr-1 flex min-w-fit items-center gap-2.5">
					<span className="flex size-8 items-center justify-center rounded-lg bg-emerald-600 text-xs font-semibold text-white shadow-sm">周</span>
            <div className="leading-tight">
						<h1 className="text-[14px] font-semibold tracking-tight text-slate-900">{PAGE_TITLE.replace('OKR 协作台', '周报协作台')}</h1>
              <div className="mt-1 flex items-center gap-1.5 text-[10px] text-slate-400">
                <span>{quarter ? quarter.replace('-', ' ') : 'OKR'}</span><span className="text-slate-300">·</span>
							<span>{mode === 'fill' ? '周报填写' : '周报会议'}</span>
              </div>
            </div>
          </div>

          <div className="flex h-8 items-center gap-1.5 rounded-lg border border-slate-200 bg-white px-2.5 text-[11px] shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
            <span className="text-slate-400">周次</span>
            <select value={week} onChange={(event) => setWeek(event.target.value)} className="bg-transparent font-medium text-slate-700 outline-none">
              {availableWeeks.map((item) => <option key={item} value={item}>{item}</option>)}
            </select>
          </div>

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

		<main className={`mx-auto max-w-[1580px] px-4 py-3 transition-[padding] sm:px-6 sm:py-4 lg:px-8 ${commentsOpen ? 'lg:pr-[420px]' : ''}`}>
			<SyncNotice />
			<WeeklyTools onOpenPoint={openPoint} />
			<>
            <CommentInteractionProvider value={{ selected: commentTarget, comments, counts: commentCounts, select: openComments }}>
              <div className={`transition-opacity ${busy ? 'pointer-events-none opacity-55' : ''}`}>
                {mode === 'meeting' ? <MeetingView /> : <KrTable definitionsReadOnly />}
              </div>
            </CommentInteractionProvider>
            <div className="mt-3 px-1 text-[11px] text-slate-400">
              {mode === 'fill' ? '停止输入后自动保存；多人修改同一条 KR 时会先请你确认。' : '会议模式使用同一份数据，以紧凑只读方式展示。'}
              <button type="button" onClick={reset} className="ml-1 underline hover:text-slate-600">重新载入</button>
            </div>
			</>
		</main>
		<CommentDrawer open={commentsOpen} quarter={quarter} week={week} target={commentTarget} canComment onSignIn={() => undefined} onShowAll={() => setCommentTarget(undefined)} onClose={() => setCommentsOpen(false)} onCountChange={setCommentCount} onCountsChange={setCommentCounts} onCommentsChange={setComments} />
    </div>
  )
}
