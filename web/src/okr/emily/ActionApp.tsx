import type { ReactNode } from 'react'
import { useBoard } from './board'
import { ActionCenter } from './components/ActionCenter'
import type { AuthStatus } from './types'

export default function ActionApp({ auth, onLogout, moduleTabs }: { auth: AuthStatus; onLogout: () => void; moduleTabs: ReactNode }) {
  const { quarter, syncState, reset } = useBoard()
  const tone = syncState.kind === 'saving' ? 'text-blue-600' : syncState.kind === 'saved' ? 'text-emerald-600' : syncState.kind === 'error' || syncState.kind === 'conflict' ? 'text-red-600' : 'text-slate-400'
  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
        <div className="mx-auto flex min-h-14 max-w-[1580px] flex-wrap items-center gap-3 px-4 py-2 sm:px-6 lg:px-8">
          <span className="flex size-8 items-center justify-center rounded-lg bg-violet-600 text-xs font-semibold text-white shadow-sm">动</span>
          <div className="leading-tight"><h1 className="text-[14px] font-semibold tracking-tight text-slate-900">Emily · 动作管理</h1><div className="mt-1 text-[10px] text-slate-400">{quarter ? quarter.replace('-', ' ') : 'OKR'} · 催填、巡检、材料与提交</div></div>
          <span className={`ml-3 hidden text-[10px] sm:inline ${tone}`} aria-live="polite">{syncState.message}</span>
          <div className="ml-auto flex items-center gap-2">{moduleTabs}<button type="button" onClick={reset} className="h-8 rounded-lg border border-slate-200 bg-white px-3 text-[10px] text-slate-500 hover:bg-slate-50">重新载入</button>{auth.user && <div className="hidden items-center gap-1.5 text-[11px] text-slate-500 xl:flex">{auth.user.avatarUrl ? <img src={auth.user.avatarUrl} alt="" className="size-6 rounded-full" /> : <span className="flex size-6 items-center justify-center rounded-full bg-slate-100 text-[10px]">{auth.user.name.slice(0, 1)}</span>}<span>{auth.user.name}</span>{auth.configured && <button type="button" onClick={onLogout} className="ml-1 text-slate-400 hover:text-slate-700">退出</button>}</div>}</div>
        </div>
      </header>
      <main className="mx-auto max-w-[1580px] px-4 py-3 sm:px-6 sm:py-4 lg:px-8">
        {(syncState.kind === 'error' || syncState.kind === 'conflict') && <div className="mb-3 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-xs text-red-700">{syncState.message}</div>}
        <ActionCenter />
      </main>
    </div>
  )
}
