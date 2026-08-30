import type { Objective } from '../types'

interface ObjectiveNavProps {
  objectives: Objective[]
  activeObjectiveId?: string
  overview?: boolean
  showOverview?: boolean
  onOverview?: () => void
  onObjective: (id: string) => void
}

function countKRs(objectives: Objective[]) {
  return objectives.reduce((sum, objective) => sum + objective.krs.length, 0)
}

const tabBase = 'flex min-w-0 items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left text-[12px] font-semibold transition-colors'

export function ObjectiveNav({
  objectives,
  activeObjectiveId,
  overview = false,
  showOverview = false,
  onOverview,
  onObjective,
}: ObjectiveNavProps) {
  return (
    <section aria-label="目标导航" className="mb-3 rounded-xl border border-slate-200 bg-gradient-to-b from-slate-50/90 to-slate-100/65 p-3 shadow-[0_3px_12px_rgba(31,35,40,0.035)]">
      <div className="grid gap-1.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
        <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">目标</span>
        <nav aria-label="目标" role="tablist" className="flex gap-1.5 overflow-x-auto pb-0.5">
          {showOverview && (
            <button type="button" role="tab" aria-selected={overview} onClick={onOverview} className={`${tabBase} max-w-64 shrink-0 border-dashed ${overview ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb]' : 'border-slate-200 bg-white/70 text-slate-600 hover:border-blue-200 hover:bg-white'}`}>
              <span className="truncate">全部 OKR</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{countKRs(objectives)}</b>
            </button>
          )}
          {objectives.map((objective) => {
            const selected = !overview && objective.id === activeObjectiveId
            return (
              <button key={objective.id} type="button" role="tab" aria-selected={selected} title={objective.title} onClick={() => onObjective(objective.id)} className={`${tabBase} max-w-64 shrink-0 ${selected ? 'border-blue-300 bg-white text-blue-700 shadow-[inset_0_-3px_0_#2563eb,0_2px_6px_rgba(37,99,235,0.07)]' : 'border-slate-200 bg-white/60 text-slate-600 hover:border-slate-300 hover:bg-white'}`}>
                <span className="truncate">{objective.title || '未命名目标'}</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{objective.krs.length}</b>
              </button>
            )
          })}
        </nav>
      </div>
    </section>
  )
}
