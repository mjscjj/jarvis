import { okrTabForWeeklyWorkspace, type WeeklyWorkspace, type WeeklyWorkspaceTab } from '../navigation.ts'

export const WEEKLY_SHARE_SCOPE = 'weekly'

const SHARE_TABS: WeeklyWorkspaceTab[] = ['review-fill', 'review-meeting', 'weekly-fill', 'weekly-meeting']

export function isWeeklyShareViewState(viewState: Record<string, unknown>): boolean {
  return viewState.share === WEEKLY_SHARE_SCOPE
}

export function weeklyShareTab(requested: unknown): WeeklyWorkspaceTab {
  const tab = SHARE_TABS.find((value) => value === requested)
  return tab ?? 'weekly-fill'
}

// A share link is for someone else's browser, so it starts from the address the
// deployment publishes rather than the one this browser happens to be using.
// Without a configured public address the current URL is all we know.
export function weeklyShareURL(currentURL: string, workspace: WeeklyWorkspace, publicBaseURL: string): string {
  const url = new URL(publicBaseURL.trim() || currentURL)
  url.hash = `/weekly-report?tab=${okrTabForWeeklyWorkspace(workspace)}`
  return url.toString()
}
