import { useEffect, useState } from 'react'
import type { AppModulePageProps } from '../modules/registry'
import { usePageContext } from '../pageContext'
import AgentFlowsWorkspace from './emily/AgentFlowsApp'
import CoreWorkspace from './emily/CoreApp'
import WeeklyReportWorkspace from './emily/App'
import { useBoard } from './emily/board'
import { BoardProvider } from './emily/store'
import { PreviewReviewProvider } from './emily/aiReviewContext'
import type { AuthStatus } from './emily/types'
import IdentityBoundary from './IdentityBoundary'
import { isWeeklyWorkspaceTab, okrTabForWeeklyMode, resolveOKRTab, weeklyWorkspaceMode, type OKRTab } from './navigation'
import { withOKRScope, withOKRTarget } from './chatContext'
import { isWeeklyShareViewState, weeklyShareTab } from './emily/share'
import { templateKeyForWeeklyMode } from './emily/weekCatalog'
import './emily/index.css'

function PageContextSync({ surface }: { surface: 'okr' | 'weekly-report' }) {
  const { quarter, week, reset, syncState } = useBoard()
  const { context, setViewState } = usePageContext()

  useEffect(() => {
    if (!quarter) return
    const next = withOKRScope(context.view_state, surface, quarter, week)
    if (JSON.stringify(next) !== JSON.stringify(context.view_state)) setViewState(next, true)
  }, [context.view_state, quarter, setViewState, surface, week])

  useEffect(() => {
    const selectTarget = (event: PointerEvent) => {
      const element = event.target instanceof Element
        ? event.target.closest<HTMLElement>('[data-okr-target-kind]')
        : null
      if (!element) return
      const next = withOKRTarget(context.view_state, surface, quarter, week, {
        objectiveId: element.dataset.okrObjectiveId,
        krId: element.dataset.okrKrId,
        pointId: element.dataset.okrPointId,
        progressId: element.dataset.okrProgressId,
      })
      setViewState(next, true)
    }
    document.addEventListener('pointerdown', selectTarget, true)
    return () => document.removeEventListener('pointerdown', selectTarget, true)
  }, [context.view_state, quarter, setViewState, surface, week])

  useEffect(() => {
    const refresh = () => {
      if (syncState.kind === 'loading' || syncState.kind === 'saving' || syncState.kind === 'conflict') return
      reset()
    }
    window.addEventListener('jarvis:chat-completed', refresh)
    return () => window.removeEventListener('jarvis:chat-completed', refresh)
  }, [reset, syncState.kind])

  return null
}

function Workspace({
  auth,
  logoutUser,
  loginUser,
  moduleEnablement,
}: {
  auth: AuthStatus
  logoutUser: () => Promise<void>
  loginUser: () => Promise<void>
  moduleEnablement: Readonly<Record<string, boolean>>
}) {
  const { context, setViewState } = usePageContext()
  const [selectedQuarter, setSelectedQuarter] = useState('')
  const requestedTab = context.view_state.tab
  const weeklyShare = isWeeklyShareViewState(context.view_state)
  const visibleTab = weeklyShare ? weeklyShareTab(requestedTab) : resolveOKRTab(requestedTab, moduleEnablement)
  const weeklyEnabled = moduleEnablement['weekly-report'] === true

  useEffect(() => {
    if (requestedTab === visibleTab) return
    setViewState({ ...context.view_state, tab: visibleTab }, true)
  }, [context.view_state, requestedTab, setViewState, visibleTab])

  const changeTab = (tab: OKRTab) => setViewState({ ...context.view_state, tab }, false)

	const surface = isWeeklyWorkspaceTab(visibleTab) ? 'weekly-report' : 'okr'
	const weeklyMode = isWeeklyWorkspaceTab(visibleTab) ? weeklyWorkspaceMode(visibleTab) : undefined
	const weekTemplateKey = weeklyMode ? templateKeyForWeeklyMode(weeklyMode) : undefined
	const boardKey = weekTemplateKey ? `${surface}:${weekTemplateKey}` : surface

	return (
		<div id="okr-workspace-root" className="okr-workspace-root">
			<BoardProvider key={boardKey} surface={surface} weekTemplateKey={weekTemplateKey} initialQuarter={selectedQuarter} onQuarterChange={setSelectedQuarter}>
				<PageContextSync surface={surface} />
				{visibleTab === 'agent-flows' ? (
					<AgentFlowsWorkspace
						weeklyEnabled={weeklyEnabled}
          />
        ) : surface === 'okr' ? (
			<CoreWorkspace />
			) : (
			<PreviewReviewProvider>
				<WeeklyReportWorkspace
					auth={auth}
					onLogout={() => void logoutUser()}
					onLogin={() => void loginUser()}
						mode={weeklyMode!}
						onModeChange={(mode) => changeTab(okrTabForWeeklyMode(mode))}
					shared={weeklyShare}
				/>
			</PreviewReviewProvider>
			)}
      </BoardProvider>
    </div>
  )
}

// Emily is one Jarvis product module in the navigation. OKR definitions and
// weekly reporting remain separate backend domains behind the same directory.
export default function OKRModule({ moduleEnablement }: AppModulePageProps) {
	return (
		<IdentityBoundary>{(auth, logoutUser, loginUser) => (
			<Workspace auth={auth} logoutUser={logoutUser} loginUser={loginUser} moduleEnablement={moduleEnablement} />
		)}</IdentityBoundary>
	)
}
