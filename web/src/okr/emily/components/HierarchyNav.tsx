import { hierarchyKRCount, priorityKRCount, type BusinessNavigation, type PriorityNavigation } from '../hierarchy'

interface HierarchyNavProps {
  navigation: BusinessNavigation[]
  activeBusiness?: BusinessNavigation
  activePriority?: PriorityNavigation
  activeObjectiveId?: string
  overview?: boolean
  showOverview?: boolean
  onOverview?: () => void
  onBusiness: (value: string) => void
  onPriority: (value: string) => void
  onObjective: (id: string) => void
}

const tabBase = 'flex min-w-0 items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left text-[12px] font-semibold transition-colors'

function priorityTone(value: string, selected: boolean) {
  if (!selected) return 'border-transparent text-slate-600 hover:bg-white'
  if (value === 'p0') return 'border-orange-200 bg-orange-50 text-orange-700 shadow-sm'
  if (value === 'p1') return 'border-amber-200 bg-amber-50 text-amber-700 shadow-sm'
  if (value === 'p2') return 'border-blue-200 bg-white text-blue-700 shadow-sm'
  return 'border-slate-200 bg-white text-slate-700 shadow-sm'
}

export function HierarchyNav({
  navigation, activeBusiness, activePriority, activeObjectiveId, overview = false, showOverview = false,
  onOverview, onBusiness, onPriority, onObjective,
}: HierarchyNavProps) {
  const total = navigation.reduce((sum, item) => sum + hierarchyKRCount(item), 0)
  return (
    <section aria-label="KR 分类导航" className="mb-3 rounded-xl border border-slate-200 bg-gradient-to-b from-slate-50/90 to-slate-100/65 p-3 shadow-[0_3px_12px_rgba(31,35,40,0.035)]">
      <div className="grid gap-1.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
        <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">业务分类</span>
        <nav aria-label="业务分类" role="tablist" className="flex gap-1.5 overflow-x-auto pb-0.5">
          {showOverview && <button type="button" role="tab" aria-selected={overview} onClick={onOverview} className={`${tabBase} max-w-64 shrink-0 border-dashed ${overview ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb]' : 'border-slate-200 bg-white/70 text-slate-600 hover:border-blue-200 hover:bg-white'}`}><span className="truncate">全部 OKR</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{total}</b></button>}
          {navigation.map((business) => {
            const selected = !overview && business.value === activeBusiness?.value
            return <button key={business.value || '__untagged__'} type="button" role="tab" aria-selected={selected} onClick={() => onBusiness(business.value)} className={`${tabBase} max-w-64 shrink-0 ${selected ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb,0_2px_7px_rgba(37,99,235,0.08)]' : 'border-slate-200 bg-white/85 text-slate-600 hover:border-blue-200 hover:bg-white hover:text-slate-900'}`}><span className="truncate">{business.label}</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{hierarchyKRCount(business)}</b></button>
          })}
        </nav>
      </div>

      {activeBusiness && <div className="mt-2.5 grid gap-1.5 border-t border-slate-200/80 pt-2.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
        <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">Focus / P1 / P2</span>
        <nav aria-label={`${activeBusiness.label}优先级`} role="tablist" className="inline-flex w-fit max-w-full items-center gap-1 overflow-x-auto rounded-lg bg-slate-200/60 p-1">
          {activeBusiness.priorities.map((priority) => {
            const selected = priority.value === activePriority?.value
            return <button key={priority.value || '__untagged__'} type="button" role="tab" aria-selected={selected} onClick={() => onPriority(priority.value)} className={`inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-[12px] font-semibold transition-colors ${priorityTone(priority.value, selected)}`}>{priority.label}<b className="rounded-full bg-slate-200/70 px-1.5 text-[10px]">{priorityKRCount(priority)}</b></button>
          })}
        </nav>
      </div>}

      {activePriority && <div className="mt-2.5 grid gap-1.5 border-t border-slate-200/80 pt-2.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
        <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">方向</span>
        <nav aria-label="方向" role="tablist" className="flex gap-1.5 overflow-x-auto pb-0.5">
          {activePriority.objectives.map((objective) => {
            const selected = objective.id === activeObjectiveId
            return <button key={objective.id} type="button" role="tab" aria-selected={selected} title={objective.title} data-okr-target-kind="objective" data-okr-objective-id={objective.id} onClick={() => onObjective(objective.id)} className={`inline-flex max-w-64 shrink-0 items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[12px] font-semibold transition-colors ${selected ? 'border-blue-300 bg-white text-blue-700 shadow-[inset_0_-3px_0_#2563eb,0_2px_6px_rgba(37,99,235,0.07)]' : 'border-slate-200 bg-white/60 text-slate-600 hover:border-slate-300 hover:bg-white'}`}><span className="truncate">{objective.title || '未命名方向'}</span><b className="rounded-full bg-slate-200/70 px-1.5 text-[10px]">{objective.krs.length}</b></button>
          })}
        </nav>
      </div>}
    </section>
  )
}
