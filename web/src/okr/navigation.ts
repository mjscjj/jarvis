export const DEFAULT_OKR_TAB = 'manage'

export const OKR_TAB_DEFINITIONS = [
  { key: 'manage', label: '管理与打标', group: 'okr' },
  { key: 'okr-plan', label: 'Biz OKR Plan', group: 'okr' },
  { key: 'agent-flows', label: 'OKR Agent', group: 'okr' },
  { key: 'review-fill', label: 'Review 填写', group: 'review', requiresModule: 'biz-okr' },
  { key: 'review-meeting', label: 'Review 会议', group: 'review', requiresModule: 'biz-okr' },
  { key: 'weekly-fill', label: '周报填写', group: 'weekly', requiresModule: 'biz-okr' },
  { key: 'weekly-meeting', label: '周报会议', group: 'weekly', requiresModule: 'biz-okr' },
] as const

export type OKRTab = typeof OKR_TAB_DEFINITIONS[number]['key']

// A weekly workspace tab is two independent choices, not one flat mode: which
// set of weeks it reads (dataset) and what the page is for (view). Keeping them
// separate is what makes all four combinations exist without a fourth page.
const WEEKLY_WORKSPACE_TABS = {
  'review-fill': { dataset: 'review', view: 'fill' },
  'review-meeting': { dataset: 'review', view: 'meeting' },
  'weekly-fill': { dataset: 'weekly', view: 'fill' },
  'weekly-meeting': { dataset: 'weekly', view: 'meeting' },
} as const

export type WeeklyWorkspaceTab = keyof typeof WEEKLY_WORKSPACE_TABS
export type WeeklyDataset = typeof WEEKLY_WORKSPACE_TABS[WeeklyWorkspaceTab]['dataset']
export type WeeklyView = typeof WEEKLY_WORKSPACE_TABS[WeeklyWorkspaceTab]['view']
export interface WeeklyWorkspace {
  dataset: WeeklyDataset
  view: WeeklyView
}

const weeklyWorkspaceTabs = Object.keys(WEEKLY_WORKSPACE_TABS) as WeeklyWorkspaceTab[]
const validTabs = new Set<string>(OKR_TAB_DEFINITIONS.map((item) => item.key))

export function isOKRTab(value: unknown): value is OKRTab {
  return typeof value === 'string' && validTabs.has(value)
}

export function isOKRTabEnabled(tab: OKRTab, moduleEnablement: Readonly<Record<string, boolean>>): boolean {
  const definition = OKR_TAB_DEFINITIONS.find((item) => item.key === tab)
  if (!definition) throw new Error(`unknown OKR tab: ${tab}`)
  return !('requiresModule' in definition) || moduleEnablement[definition.requiresModule] === true
}

export function resolveOKRTab(
  requested: unknown,
  moduleEnablement: Readonly<Record<string, boolean>>,
): OKRTab {
  const tab = isOKRTab(requested) ? requested : DEFAULT_OKR_TAB
  return isOKRTabEnabled(tab, moduleEnablement) ? tab : DEFAULT_OKR_TAB
}

export function isWeeklyWorkspaceTab(tab: OKRTab): tab is WeeklyWorkspaceTab {
  return tab in WEEKLY_WORKSPACE_TABS
}

export function weeklyWorkspace(tab: OKRTab): WeeklyWorkspace {
  if (!isWeeklyWorkspaceTab(tab)) throw new Error(`OKR tab is not a weekly workspace: ${tab}`)
  return WEEKLY_WORKSPACE_TABS[tab]
}

export function okrTabForWeeklyWorkspace({ dataset, view }: WeeklyWorkspace): WeeklyWorkspaceTab {
  const tab = weeklyWorkspaceTabs.find((key) => {
    const workspace = WEEKLY_WORKSPACE_TABS[key]
    return workspace.dataset === dataset && workspace.view === view
  })
  if (!tab) throw new Error(`unknown weekly workspace: ${dataset}/${view}`)
  return tab
}

export function weeklyDatasetLabel(dataset: WeeklyDataset): string {
  return dataset === 'review' ? 'Review' : '周报'
}

export function weeklyViewLabel(view: WeeklyView): string {
  return view === 'fill' ? '填写' : '会议'
}
