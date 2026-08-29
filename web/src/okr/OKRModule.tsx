import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { listAppModules } from '../api'
import { usePageContext } from '../pageContext'
import AgentFlowsWorkspace from './emily/AgentFlowsApp'
import CoreWorkspace from './emily/CoreApp'
import WeeklyReportWorkspace from './emily/App'
import { BoardProvider } from './emily/store'
import type { AuthStatus } from './emily/types'
import IdentityBoundary from './IdentityBoundary'
import './emily/index.css'

type OKRTab = 'structure' | 'manage' | 'agent-flows' | 'weekly-fill' | 'weekly-meeting'

const validTabs = new Set<OKRTab>(['structure', 'manage', 'agent-flows', 'weekly-fill', 'weekly-meeting'])

function ModuleTabs({ active, weeklyEnabled, onChange }: { active: OKRTab; weeklyEnabled: boolean; onChange: (tab: OKRTab) => void }) {
  const items: Array<{ key: OKRTab; label: string; group: 'okr' | 'weekly' }> = [
    { key: 'structure', label: 'OKR 结构', group: 'okr' },
    { key: 'manage', label: '管理与打标', group: 'okr' },
    { key: 'agent-flows', label: 'Agent 流程', group: 'okr' },
    ...(weeklyEnabled ? [
      { key: 'weekly-fill' as const, label: '周报填写', group: 'weekly' as const },
      { key: 'weekly-meeting' as const, label: '周报会议', group: 'weekly' as const },
    ] : []),
  ]

  return (
    <nav aria-label="OKR 子页面" className="inline-flex h-9 max-w-[min(680px,68vw)] items-center overflow-x-auto rounded-xl border border-slate-200 bg-slate-100 p-1 text-[11px] shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
      {items.map((item, index) => (
        <span key={item.key} className="contents">
          {index > 0 && items[index - 1].group !== item.group && <span aria-hidden className="mx-1 h-4 w-px bg-slate-300" />}
          <button
            type="button"
            onClick={() => onChange(item.key)}
            aria-current={active === item.key ? 'page' : undefined}
            className={`h-7 rounded-lg px-3 transition-colors ${active === item.key ? 'bg-white font-semibold text-slate-900 shadow-sm ring-1 ring-slate-200/70' : 'font-medium text-slate-500 hover:text-slate-800'}`}
          >
            {item.label}
          </button>
        </span>
      ))}
    </nav>
  )
}

function Workspace({ auth, logoutUser }: { auth: AuthStatus; logoutUser: () => Promise<void> }) {
  const { context, setViewState } = usePageContext()
  const [weeklyEnabled, setWeeklyEnabled] = useState(true)
  const requestedTab = context.view_state.tab
  const active = validTabs.has(requestedTab as OKRTab) ? requestedTab as OKRTab : 'structure'
  const visibleTab = !weeklyEnabled && active.startsWith('weekly-') ? 'structure' : active

  useEffect(() => {
    const controller = new AbortController()
    listAppModules(controller.signal)
      .then(({ items }) => setWeeklyEnabled(items.find((item) => item.key === 'weekly-report')?.is_enabled ?? false))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setWeeklyEnabled(false)
      })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (requestedTab === visibleTab) return
    setViewState({ ...context.view_state, tab: visibleTab }, true)
  }, [context.view_state, requestedTab, setViewState, visibleTab])

  const changeTab = (tab: OKRTab) => setViewState({ ...context.view_state, tab }, false)
  const moduleTabs = useMemo<ReactNode>(() => (
    <ModuleTabs active={visibleTab} weeklyEnabled={weeklyEnabled} onChange={changeTab} />
  ), [visibleTab, weeklyEnabled, context.view_state])

  const surface = visibleTab.startsWith('weekly-') ? 'weekly-report' : 'okr'

  return (
    <div id="okr-workspace-root" className="okr-workspace-root">
      <BoardProvider key={surface} surface={surface}>
        {visibleTab === 'agent-flows' ? (
          <AgentFlowsWorkspace
            auth={auth}
            onLogout={() => void logoutUser()}
            moduleTabs={moduleTabs}
          />
        ) : surface === 'okr' ? (
          <CoreWorkspace
            auth={auth}
            onLogout={() => void logoutUser()}
            view={visibleTab === 'manage' ? 'manage' : 'structure'}
            moduleTabs={moduleTabs}
          />
        ) : (
          <WeeklyReportWorkspace
            auth={auth}
            onLogout={() => void logoutUser()}
            mode={visibleTab === 'weekly-meeting' ? 'meeting' : 'fill'}
            onModeChange={(mode) => changeTab(mode === 'meeting' ? 'weekly-meeting' : 'weekly-fill')}
            moduleTabs={moduleTabs}
          />
        )}
      </BoardProvider>
    </div>
  )
}

// Emily is one Jarvis product module in the navigation. OKR definitions and
// weekly reporting remain separate backend domains, exposed here as child tabs.
export default function OKRModule() {
	return (
		<IdentityBoundary>{(auth, logoutUser) => (
			<Workspace auth={auth} logoutUser={logoutUser} />
		)}</IdentityBoundary>
	)
}
