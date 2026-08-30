import { useEffect, useState } from 'react'
import type { AppModulePageProps } from '../modules/registry'
import { usePageContext } from '../pageContext'
import AgentFlowsWorkspace from './emily/AgentFlowsApp'
import CoreWorkspace from './emily/CoreApp'
import WeeklyReportWorkspace from './emily/App'
import { BoardProvider } from './emily/store'
import type { AuthStatus } from './emily/types'
import IdentityBoundary from './IdentityBoundary'
import { resolveOKRTab, type OKRTab } from './navigation'
import './emily/index.css'

function Workspace({
  auth,
  logoutUser,
  moduleEnablement,
}: {
  auth: AuthStatus
  logoutUser: () => Promise<void>
  moduleEnablement: Readonly<Record<string, boolean>>
}) {
  const { context, setViewState } = usePageContext()
  const [selectedQuarter, setSelectedQuarter] = useState('')
  const requestedTab = context.view_state.tab
  const visibleTab = resolveOKRTab(requestedTab, moduleEnablement)
  const weeklyEnabled = moduleEnablement['weekly-report'] === true

  useEffect(() => {
    if (requestedTab === visibleTab) return
    setViewState({ ...context.view_state, tab: visibleTab }, true)
  }, [context.view_state, requestedTab, setViewState, visibleTab])

  const changeTab = (tab: OKRTab) => setViewState({ ...context.view_state, tab }, false)

  const surface = visibleTab.startsWith('weekly-') ? 'weekly-report' : 'okr'

	return (
		<div id="okr-workspace-root" className="okr-workspace-root">
			<BoardProvider key={surface} surface={surface} initialQuarter={selectedQuarter} onQuarterChange={setSelectedQuarter}>
				{visibleTab === 'agent-flows' ? (
					<AgentFlowsWorkspace
						auth={auth}
						onLogout={() => void logoutUser()}
						weeklyEnabled={weeklyEnabled}
          />
        ) : surface === 'okr' ? (
          <CoreWorkspace
            auth={auth}
            onLogout={() => void logoutUser()}
          />
        ) : (
          <WeeklyReportWorkspace
            auth={auth}
            onLogout={() => void logoutUser()}
            mode={visibleTab === 'weekly-meeting' ? 'meeting' : 'fill'}
            onModeChange={(mode) => changeTab(mode === 'meeting' ? 'weekly-meeting' : 'weekly-fill')}
          />
        )}
      </BoardProvider>
    </div>
  )
}

// Emily is one Jarvis product module in the navigation. OKR definitions and
// weekly reporting remain separate backend domains behind the same directory.
export default function OKRModule({ moduleEnablement }: AppModulePageProps) {
	return (
		<IdentityBoundary>{(auth, logoutUser) => (
			<Workspace auth={auth} logoutUser={logoutUser} moduleEnablement={moduleEnablement} />
		)}</IdentityBoundary>
	)
}
