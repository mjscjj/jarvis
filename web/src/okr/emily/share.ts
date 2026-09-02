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

export function weeklyShareURL(currentURL: string, workspace: WeeklyWorkspace): string {
  const url = new URL(currentURL)
  url.hash = `/weekly-report?tab=${okrTabForWeeklyWorkspace(workspace)}`
  return url.toString()
}
