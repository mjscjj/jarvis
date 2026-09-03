import { isWeeklyWorkspaceTab } from '../../navigation'
import { WEEKLY_SHARE_NAV, type WeeklyShareTab } from '../share'

function activeTone(tab: WeeklyShareTab): string {
  if (tab === 'okr-plan') return 'bg-white text-emerald-700 shadow-sm'
  if (isWeeklyWorkspaceTab(tab) && tab.startsWith('review-')) return 'bg-white text-violet-700 shadow-sm'
  return 'bg-white text-blue-700 shadow-sm'
}

export function WeeklyShareNav({ currentTab, onChange }: { currentTab: WeeklyShareTab; onChange: (tab: WeeklyShareTab) => void }) {
  return (
    <nav aria-label="共享页面" className="flex h-9 max-w-full shrink-0 items-center overflow-x-auto rounded-xl border border-slate-200 bg-slate-50 p-1">
      {WEEKLY_SHARE_NAV.map((item) => {
        const active = item.key === currentTab
        return (
          <button
            key={item.key}
            type="button"
            aria-current={active ? 'page' : undefined}
            onClick={() => onChange(item.key)}
            className={`h-7 shrink-0 whitespace-nowrap rounded-lg px-3 text-[11px] font-medium ${active ? activeTone(item.key) : 'text-slate-500 hover:text-slate-700'}`}
          >
            {item.label}
          </button>
        )
      })}
    </nav>
  )
}
