import { useBoard } from './board'
import { AgentFlowCenter } from './components/AgentFlowCenter'
import { QuarterSelect } from './components/QuarterSelect'
import type { AuthStatus } from './types'

export default function AgentFlowsApp({ auth, onLogout, weeklyEnabled }: { auth: AuthStatus; onLogout: () => void; weeklyEnabled: boolean }) {
  const { quarter, syncState, reset } = useBoard()
  const tone = syncState.kind === 'error' ? 'text-red-600' : 'text-slate-400'
  return (
    <div className="min-h-full">
      <header className="sticky top-0 z-40 border-b border-slate-200/80 bg-white/95 backdrop-blur">
        <div className="mx-auto flex min-h-14 max-w-[1580px] flex-wrap items-center gap-3 px-4 py-2 sm:px-6 lg:px-8">
          <span className="flex size-8 items-center justify-center rounded-lg bg-cyan-600 text-xs font-semibold text-white shadow-sm">A</span>
          <div className="leading-tight"><h1 className="text-[14px] font-semibold tracking-tight text-slate-900">Emily · 自动化流程</h1><div className="mt-1 text-[10px] text-slate-400">{quarter ? quarter.replace('-', ' ') : 'OKR'} · Prompt、Skill 与原子工具</div></div>
          <span className={`ml-3 hidden text-[10px] sm:inline ${tone}`} aria-live="polite">{syncState.message}</span>
          <div className="ml-auto flex items-center gap-2"><QuarterSelect /><button type="button" onClick={reset} className="h-8 rounded-lg border border-slate-200 bg-white px-3 text-[10px] text-slate-500 hover:bg-slate-50">重新载入</button>{auth.user && <div className="hidden items-center gap-1.5 text-[11px] text-slate-500 xl:flex"><span>{auth.user.name}</span>{auth.configured && <button type="button" onClick={onLogout} className="ml-1 text-slate-400 hover:text-slate-700">退出</button>}</div>}</div>
        </div>
      </header>
		<main className="mx-auto max-w-[1580px] px-4 py-3 sm:px-6 sm:py-4 lg:px-8"><AgentFlowCenter weeklyEnabled={weeklyEnabled} /></main>
    </div>
  )
}
