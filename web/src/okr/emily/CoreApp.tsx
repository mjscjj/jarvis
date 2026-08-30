import { useState } from 'react'
import { ManagementView } from './components/ManagementView'
import { QuarterSelect } from './components/QuarterSelect'
import { KrTable } from './components/Table'
import { useBoard } from './board'
import type { AuthStatus } from './types'

export default function CoreApp({
  auth,
  onLogout,
  view,
}: {
  auth: AuthStatus
  onLogout: () => void
  view: 'structure' | 'manage'
}) {
  const { quarter, syncState, reset, createObjective } = useBoard()
  const [creating, setCreating] = useState(false)
  const [objectiveTitle, setObjectiveTitle] = useState('')
  const now = new Date()
  const defaultQuarter = `${now.getFullYear()}-Q${Math.floor(now.getMonth() / 3) + 1}`
  const [objectiveQuarter, setObjectiveQuarter] = useState(quarter || defaultQuarter)
  const tone = syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : syncState.kind === 'error' || syncState.kind === 'conflict' ? 'text-red-600' : 'text-slate-400'

  const submitObjective = async () => {
    const title = objectiveTitle.trim()
    if (!title) return
    await createObjective({ quarter: objectiveQuarter.trim(), title })
    setObjectiveTitle('')
    setCreating(false)
  }

  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
        <div className="mx-auto flex min-h-14 max-w-[1580px] items-center gap-3 px-4 py-2 sm:px-6 lg:px-8">
          <span className="flex size-8 items-center justify-center rounded-lg bg-indigo-600 text-xs font-semibold text-white shadow-sm">O</span>
          <div className="leading-tight"><h1 className="text-[14px] font-semibold tracking-tight text-slate-900">Emily · OKR</h1><div className="mt-1 text-[10px] text-slate-400">{quarter ? quarter.replace('-', ' ') : 'OKR'} · 目标与拆解</div></div>
          <span className={`ml-3 text-[10px] ${tone}`} aria-live="polite">{syncState.message}</span>
          <div className="ml-auto flex items-center gap-2">
            <QuarterSelect />
            <button type="button" onClick={reset} className="h-8 rounded-lg border border-slate-200 bg-white px-3 text-[10px] text-slate-500 hover:bg-slate-50">重新载入</button>
            {auth.user && <div className="hidden items-center gap-1.5 text-[11px] text-slate-500 sm:flex">{auth.user.avatarUrl ? <img src={auth.user.avatarUrl} alt="" className="size-6 rounded-full" /> : <span className="flex size-6 items-center justify-center rounded-full bg-slate-100 text-[10px]">{auth.user.name.slice(0, 1)}</span>}<span>{auth.user.name}</span>{auth.configured && <button type="button" onClick={onLogout} className="ml-1 text-slate-400 hover:text-slate-700">退出</button>}</div>}
          </div>
        </div>
      </header>
      <main className="mx-auto max-w-[1580px] px-4 py-3 sm:px-6 sm:py-4 lg:px-8">
        {(syncState.kind === 'error' || syncState.kind === 'conflict') && <div className="mb-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{syncState.message}</div>}
        {view === 'structure' ? <div>
          <section className="mb-3 flex flex-wrap items-center gap-3 rounded-xl border border-slate-200 bg-white px-4 py-3 shadow-sm">
            <div><h2 className="text-xs font-semibold text-slate-800">OKR 结构</h2><p className="mt-0.5 text-[10px] text-slate-400">在这里建立 O，并查看 O、KR、指标与拆解关系。</p></div>
            <button type="button" onClick={() => { setObjectiveQuarter(quarter || defaultQuarter); setCreating((value) => !value) }} className="ml-auto h-8 rounded-lg bg-indigo-600 px-3 text-[10px] font-medium text-white hover:bg-indigo-700">+ 新建 O</button>
          </section>
          {creating && <div className="mb-3 flex flex-wrap items-center gap-2 rounded-xl border border-indigo-100 bg-indigo-50/60 p-3">
            <input value={objectiveQuarter} onChange={(event) => setObjectiveQuarter(event.target.value)} placeholder="2026-Q3" aria-label="季度" className="h-9 w-28 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-indigo-400" />
            <input value={objectiveTitle} onChange={(event) => setObjectiveTitle(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') void submitObjective() }} placeholder="目标名称" aria-label="目标名称" autoFocus className="h-9 min-w-64 flex-1 rounded-lg border border-slate-200 bg-white px-3 text-xs outline-none focus:border-indigo-400" />
            <button type="button" onClick={() => void submitObjective()} disabled={!objectiveTitle.trim() || syncState.kind === 'saving'} className="h-9 rounded-lg bg-indigo-600 px-4 text-xs font-medium text-white disabled:opacity-40">创建目标</button>
            <button type="button" onClick={() => setCreating(false)} className="h-9 px-2 text-xs text-slate-400">取消</button>
          </div>}
	          <KrTable progressReadOnly showProgress={false} manageObjectives />
        </div> : <ManagementView />}
      </main>
    </div>
  )
}
