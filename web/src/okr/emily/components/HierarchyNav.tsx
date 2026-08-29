import { businessKrCount, krCount, subgroupTone, type BusinessNavigation, type NavigationSubgroup } from '../hierarchy'

interface HierarchyNavProps {
  navigation: BusinessNavigation[]
  activeBusiness?: BusinessNavigation
  activeSubgroup?: NavigationSubgroup
  activeObjectiveId?: string
  overview?: boolean
  showOverview?: boolean
  onBusiness: (id: string) => void
  onSubgroup: (id: string) => void
  onObjective: (id: string) => void
}

const tabBase = 'flex min-w-0 items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left text-[12px] font-semibold transition-colors'

export function HierarchyNav({
  navigation, activeBusiness, activeSubgroup, activeObjectiveId, overview = false, showOverview = false,
  onBusiness, onSubgroup, onObjective,
}: HierarchyNavProps) {
  const total = navigation.reduce((sum, item) => sum + businessKrCount(item), 0)
  const businessTabs = navigation.filter((item) => businessKrCount(item) > 0 || item.id !== 'other')

  return (
    <section aria-label="KR 分类导航" className="mb-3 rounded-xl border border-slate-200 bg-gradient-to-b from-slate-50/90 to-slate-100/65 p-3 shadow-[0_3px_12px_rgba(31,35,40,0.035)]">
      <div className="grid gap-1.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
        <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">业务分类</span>
        <nav aria-label="业务分类" role="tablist" className="grid grid-cols-1 gap-1.5 sm:grid-cols-2 xl:grid-cols-5">
          {showOverview && (
            <button type="button" role="tab" aria-selected={overview} onClick={() => onBusiness('')} className={`${tabBase} border-dashed ${overview ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb]' : 'border-slate-200 bg-white/70 text-slate-600 hover:border-blue-200 hover:bg-white'}`}>
              <span className="truncate">全部 OKR</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{total}</b>
            </button>
          )}
          {businessTabs.map((business) => {
            const selected = !overview && business.id === activeBusiness?.id
            return (
              <button key={business.id} type="button" role="tab" aria-selected={selected} onClick={() => onBusiness(business.id)} className={`${tabBase} ${selected ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb,0_2px_7px_rgba(37,99,235,0.08)]' : 'border-slate-200 bg-white/85 text-slate-600 hover:border-blue-200 hover:bg-white hover:text-slate-900'}`}>
                <span className="truncate">{business.label}</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{businessKrCount(business)}</b>
              </button>
            )
          })}
        </nav>
      </div>

      {!overview && activeBusiness && activeBusiness.subgroups.length > 1 && (
        <div className="mt-2.5 grid gap-1.5 border-t border-slate-200/80 pt-2.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
          <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">Focus / P1</span>
          <nav aria-label={`${activeBusiness.label}分类`} role="tablist" className="inline-flex w-fit max-w-full items-center gap-1 overflow-x-auto rounded-lg bg-slate-200/60 p-1">
            {activeBusiness.subgroups.map((subgroup) => {
              const selected = subgroup.id === activeSubgroup?.id
              const tone = subgroupTone(subgroup.label)
              const selectedTone = tone === 'focus' ? 'border-orange-200 bg-orange-50 text-orange-700' : tone === 'p1' ? 'border-amber-200 bg-amber-50 text-amber-700' : 'border-blue-200 bg-white text-blue-700'
              return (
                <button key={subgroup.id} type="button" role="tab" aria-selected={selected} onClick={() => onSubgroup(subgroup.id)} className={`inline-flex shrink-0 items-center gap-1.5 rounded-md border px-2.5 py-1 text-[12px] font-semibold transition-colors ${selected ? `${selectedTone} shadow-sm` : 'border-transparent text-slate-600 hover:bg-white'}`}>
                  {subgroup.label}<b className="rounded-full bg-slate-200/70 px-1.5 text-[10px]">{krCount(subgroup.objectives)}</b>
                </button>
              )
            })}
          </nav>
        </div>
      )}

      {!overview && activeSubgroup && (
        <div className="mt-2.5 grid gap-1.5 border-t border-slate-200/80 pt-2.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
          <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">方向</span>
          <nav aria-label="方向" role="tablist" className="flex gap-1.5 overflow-x-auto pb-0.5">
            {activeSubgroup.objectives.map((objective) => {
              const selected = objective.id === activeObjectiveId
              return (
                <button key={objective.id} type="button" role="tab" aria-selected={selected} title={objective.title} onClick={() => onObjective(objective.id)} className={`inline-flex max-w-64 shrink-0 items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[12px] font-semibold transition-colors ${selected ? 'border-blue-300 bg-white text-blue-700 shadow-[inset_0_-3px_0_#2563eb,0_2px_6px_rgba(37,99,235,0.07)]' : 'border-slate-200 bg-white/60 text-slate-600 hover:border-slate-300 hover:bg-white'}`}>
                  <span className="truncate">{objective.title || '未命名方向'}</span><b className="rounded-full bg-slate-200/70 px-1.5 text-[10px]">{objective.krs.length}</b>
                </button>
              )
            })}
            {activeSubgroup.missing.map((slot) => (
              <span key={slot.label} title="当前数据中尚无该方向" className="inline-flex max-w-64 shrink-0 cursor-default items-center gap-1.5 rounded-lg border border-dashed border-slate-200 px-2.5 py-1.5 text-[12px] font-semibold text-slate-400">
                <span className="truncate">{slot.label}</span><b className="whitespace-nowrap text-[9px]">待接入</b>
              </span>
            ))}
          </nav>
        </div>
      )}
    </section>
  )
}
