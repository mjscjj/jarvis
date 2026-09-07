import type { BusinessCategoryOption } from '../hierarchy'

// The strip every OKR page puts above its KR list. The fill and meeting pages
// drill further into priority and direction below it; management filters a flat
// list with it. Both share this so the two pages cannot drift apart visually.
export function BusinessCategoryTabs({ options, activeValue, total, showOverview = false, onSelect }: {
  options: BusinessCategoryOption[]
  // undefined selects the overview instead of one category.
  activeValue?: string
  total: number
  showOverview?: boolean
  onSelect: (value?: string) => void
}) {
  const overview = activeValue === undefined
  const tabBase = 'flex min-w-0 max-w-64 shrink-0 items-center justify-between gap-2 rounded-lg border px-3 py-2 text-left text-[12px] font-semibold transition-colors'
  return (
    <div className="grid gap-1.5 lg:grid-cols-[5.25rem_minmax(0,1fr)] lg:gap-2.5">
      <span className="flex items-center text-[10px] font-bold tracking-[0.04em] text-slate-400">业务分类</span>
      <nav aria-label="业务分类" role="tablist" className="flex gap-1.5 overflow-x-auto pb-0.5">
        {showOverview && <button type="button" role="tab" aria-selected={overview} data-okr-target-kind="scope" onClick={() => onSelect(undefined)} className={`${tabBase} border-dashed ${overview ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb]' : 'border-slate-200 bg-white/70 text-slate-600 hover:border-blue-200 hover:bg-white'}`}><span className="truncate">全部 OKR</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{total}</b></button>}
        {options.map((option) => {
          const selected = !overview && option.value === activeValue
          return <button key={option.value || '__untagged__'} type="button" role="tab" aria-selected={selected} data-okr-target-kind="scope" onClick={() => onSelect(option.value)} className={`${tabBase} ${selected ? 'border-blue-300 bg-blue-50 text-blue-700 shadow-[inset_3px_0_0_#2563eb,0_2px_7px_rgba(37,99,235,0.08)]' : 'border-slate-200 bg-white/85 text-slate-600 hover:border-blue-200 hover:bg-white hover:text-slate-900'}`}><span className="truncate">{option.label}</span><b className="rounded-full bg-slate-200/70 px-1.5 py-0.5 text-[10px]">{option.count}</b></button>
        })}
      </nav>
    </div>
  )
}
