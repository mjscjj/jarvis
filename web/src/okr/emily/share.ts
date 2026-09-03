import { isWeeklyWorkspaceTab, OKR_TAB_DEFINITIONS, okrTabForWeeklyWorkspace, type OKRTab, type WeeklyWorkspace, type WeeklyWorkspaceTab } from '../navigation.ts'

export const WEEKLY_SHARE_SCOPE = 'weekly'

export const WEEKLY_SHARE_TABS = ['okr-plan', 'review-fill', 'review-meeting', 'weekly-fill', 'weekly-meeting'] as const
export type WeeklyShareTab = typeof WEEKLY_SHARE_TABS[number]

export const WEEKLY_SHARE_NAV = WEEKLY_SHARE_TABS.map((key) => {
  const definition = OKR_TAB_DEFINITIONS.find((item) => item.key === key)
  if (!definition) throw new Error(`unknown weekly share tab: ${key}`)
  return { key, label: definition.label }
})

export function isWeeklyShareViewState(viewState: Record<string, unknown>): boolean {
  return viewState.share === WEEKLY_SHARE_SCOPE
}

export function isWeeklyShareTab(tab: OKRTab): tab is WeeklyShareTab {
  return WEEKLY_SHARE_TABS.includes(tab as WeeklyShareTab)
}

export function weeklyShareTab(requested: unknown): WeeklyShareTab {
  const tab = WEEKLY_SHARE_TABS.find((value) => value === requested)
  return tab ?? 'weekly-fill'
}

export function weeklyShareWorkspaceTab(tab: WeeklyShareTab): WeeklyWorkspaceTab | undefined {
  return isWeeklyWorkspaceTab(tab) ? tab : undefined
}

export function weeklyShareURLForTab(currentURL: string, tab: WeeklyShareTab, publicBaseURL: string): string {
  const url = new URL(publicBaseURL.trim() || currentURL)
  url.hash = `/weekly-report?tab=${tab}`
  return url.toString()
}

// A share link is for someone else's browser, so it starts from the address the
// deployment publishes rather than the one this browser happens to be using.
// Without a configured public address the current URL is all we know.
export function weeklyShareURL(currentURL: string, workspace: WeeklyWorkspace, publicBaseURL: string): string {
  return weeklyShareURLForTab(currentURL, okrTabForWeeklyWorkspace(workspace), publicBaseURL)
}
