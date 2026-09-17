import { useCallback, useEffect, useState } from 'react'
import type { AppModulePageProps } from '../modules/registry'
import { usePageContext } from '../pageContext'
import AgentFlowsWorkspace from './emily/AgentFlowsApp'
import CoreWorkspace from './emily/CoreApp'
import PlanWorkspace from './emily/PlanApp'
import RegionalAlignmentWorkspace from './emily/RegionalAlignmentApp'
import WeeklyReportWorkspace from './emily/App'
import { useBoard } from './emily/board'
import { BoardProvider } from './emily/store'
import { PreviewReviewProvider } from './emily/aiReviewContext'
import IdentityBoundary from './IdentityBoundary'
import { ProductFeedbackProvider } from './emily/components/ProductFeedbackCenter'
import { isWeeklyWorkspaceTab, okrTabForWeeklyWorkspace, resolveOKRTab, weeklyWorkspace, type OKRTab } from './navigation'
import { withOKRScope } from './pageScope'
import { isWeeklyShareViewState, weeklyShareTab, weeklyShareWorkspaceTab, type WeeklyShareTab } from './emily/share'
import { templateKeyForDataset } from './emily/weekCatalog'
import type { AuthStatus } from './emily/types'
import { activeQuarterForViewState, okrPlanDefaultQuarter, previousQuarter, quarterFromViewState } from './routeState'
import './emily/index.css'

function PageContextSync({ surface }: { surface: 'okr' | 'weekly-report' }) {
  const { quarter, week } = useBoard()
  const { context, setViewState } = usePageContext()

  useEffect(() => {
    if (!quarter) return
    const next = withOKRScope(context.view_state, surface, quarter, week)
    if (JSON.stringify(next) !== JSON.stringify(context.view_state)) setViewState(next, true)
  }, [context.view_state, quarter, setViewState, surface, week])

  return null
}

function Workspace({ moduleEnablement, auth }: {
  moduleEnablement: Readonly<Record<string, boolean>>
	auth: AuthStatus
}) {
  const { context, setViewState } = usePageContext()
  const requestedTab = context.view_state.tab
  const weeklyShare = isWeeklyShareViewState(context.view_state)
  const [selectedQuarter, setSelectedQuarter] = useState(() => {
    const routeQuarter = quarterFromViewState(context.view_state)
    return weeklyShare && requestedTab === 'okr-plan' ? previousQuarter(routeQuarter) : routeQuarter
  })
  const resolvedTab = weeklyShare ? weeklyShareTab(requestedTab) : resolveOKRTab(requestedTab, moduleEnablement)
	const visibleTab = !weeklyShare && resolvedTab === 'manage' && !auth.managementAccess ? 'okr-plan' : resolvedTab
  const weeklyEnabled = moduleEnablement['biz-okr'] === true
  const planVisible = visibleTab === 'okr-plan' || visibleTab === 'regional-alignment'
  const activeQuarter = planVisible
    ? activeQuarterForViewState(context.view_state, selectedQuarter)
    : selectedQuarter || activeQuarterForViewState(context.view_state, selectedQuarter)

  // selectedQuarter 记的是「有数据的看板停在哪个季度」，切回 Review 时要还原成它。
  // Plan 页显示的是下季度草稿，不能让它写进来，否则这份记忆就被覆盖了。
  useEffect(() => {
    if (planVisible) return
    if (!activeQuarter || activeQuarter === selectedQuarter) return
    setSelectedQuarter(activeQuarter)
  }, [activeQuarter, planVisible, selectedQuarter])

  useEffect(() => {
    if (requestedTab === visibleTab) return
    setViewState({ ...context.view_state, tab: visibleTab }, true)
  }, [context.view_state, requestedTab, setViewState, visibleTab])

  const changeTab = useCallback((tab: OKRTab) => {
    if (tab === 'okr-plan') {
      setViewState(withOKRScope({ ...context.view_state, tab }, 'okr', okrPlanDefaultQuarter(), ''), false)
      return
    }
    const next = planVisible && selectedQuarter
      ? withOKRScope({ ...context.view_state, tab }, 'okr', selectedQuarter, '')
      : { ...context.view_state, tab }
    setViewState(next, false)
  }, [context.view_state, planVisible, selectedQuarter, setViewState])
  const changeShareTab = useCallback((tab: WeeklyShareTab) => {
    if (tab === 'okr-plan') {
      changeTab(tab)
      return
    }
    const workspace = weeklyShareWorkspaceTab(tab)
    if (workspace) changeTab(workspace)
  }, [changeTab])
  // 只发布给 chat 上下文，不写 selectedQuarter：Plan 翻到哪个季度都不该改变
  // Review 和周报回来时落在哪。
  const syncPlanQuarter = useCallback((quarter: string) => {
    const next = withOKRScope(context.view_state, 'okr', quarter, '')
    if (JSON.stringify(next) !== JSON.stringify(context.view_state)) setViewState(next, true)
  }, [context.view_state, setViewState])

	const surface = isWeeklyWorkspaceTab(visibleTab) ? 'weekly-report' : 'okr'
	const workspace = isWeeklyWorkspaceTab(visibleTab) ? weeklyWorkspace(visibleTab) : undefined
	const weekTemplateKey = workspace ? templateKeyForDataset(workspace.dataset) : undefined
	const boardKey = weekTemplateKey ? `${surface}:${weekTemplateKey}` : surface

	if (visibleTab === 'okr-plan') {
		return (
			<div id="okr-workspace-root" className="okr-workspace-root">
				<PlanWorkspace initialQuarter={activeQuarter} initialPlanId={context.view_state.plan_id} initialCommentId={context.view_state.comment_id} onQuarterChange={syncPlanQuarter} shared={weeklyShare} onShareTabChange={changeShareTab} />
			</div>
		)
	}

	if (visibleTab === 'regional-alignment') {
		return (
			<div id="okr-workspace-root" className="okr-workspace-root">
				<RegionalAlignmentWorkspace
					initialQuarter={activeQuarter}
					initialRegion={typeof context.view_state.region === 'string' ? context.view_state.region : ''}
					initialCommentId={typeof context.view_state.comment_id === 'string' ? context.view_state.comment_id : ''}
					autoMatchAccess={auth.regionalAutoMatchAccess}
					onScopeChange={(quarter, region) => {
						const next = withOKRScope({ ...context.view_state, tab: 'regional-alignment', region }, 'okr', quarter, '')
						if (JSON.stringify(next) !== JSON.stringify(context.view_state)) setViewState(next, true)
					}}
				/>
			</div>
		)
	}

	return (
		<div id="okr-workspace-root" className="okr-workspace-root">
			<BoardProvider key={boardKey} surface={surface} weekTemplateKey={weekTemplateKey} initialQuarter={activeQuarter} initialWeek={context.view_state.week} onQuarterChange={setSelectedQuarter}>
				<PageContextSync surface={surface} />
				{visibleTab === 'agent-flows' ? (
					<AgentFlowsWorkspace
						weeklyEnabled={weeklyEnabled}
          />
        ) : surface === 'okr' ? (
			<CoreWorkspace />
			) : (
			<PreviewReviewProvider reviewType="progress">
				<WeeklyReportWorkspace
						workspace={workspace!}
						onWorkspaceChange={(next) => changeTab(okrTabForWeeklyWorkspace(next))}
						onShareTabChange={changeShareTab}
					initialCommentId={context.view_state.comment_id}
					shared={weeklyShare}
				/>
			</PreviewReviewProvider>
			)}
      </BoardProvider>
    </div>
  )
}

// Biz OKR is the product shell. It composes the generic OKR definition and
// formal-progress APIs with Biz-owned plans, tags, review and automation.
export default function BizOKRModule({ moduleEnablement }: AppModulePageProps) {
	return (
		<IdentityBoundary>
			{(auth) => <ProductFeedbackProvider key={auth.user?.unionId || auth.user?.openId || 'anonymous'}><Workspace moduleEnablement={moduleEnablement} auth={auth} /></ProductFeedbackProvider>}
		</IdentityBoundary>
	)
}
