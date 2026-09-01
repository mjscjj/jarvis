export type WeeklyShareMode = 'fill' | 'meeting'
export type WeeklyShareTab = 'weekly-fill' | 'weekly-meeting'

export const WEEKLY_SHARE_SCOPE = 'weekly'

export function isWeeklyShareViewState(viewState: Record<string, unknown>): boolean {
  return viewState.share === WEEKLY_SHARE_SCOPE
}

export function weeklyShareTab(requested: unknown): WeeklyShareTab {
  return requested === 'weekly-meeting' ? 'weekly-meeting' : 'weekly-fill'
}

export function weeklyShareURL(currentURL: string, mode: WeeklyShareMode): string {
  const url = new URL(currentURL)
  url.hash = `/weekly-report?tab=${mode === 'meeting' ? 'weekly-meeting' : 'weekly-fill'}`
  return url.toString()
}
