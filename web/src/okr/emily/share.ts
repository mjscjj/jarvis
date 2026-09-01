import type { WeeklyWorkspaceMode } from '../navigation.ts'

export type WeeklyShareMode = WeeklyWorkspaceMode
export type WeeklyShareTab = 'weekly-fill' | 'weekly-meeting' | 'okr-review'

export const WEEKLY_SHARE_SCOPE = 'weekly'

export function isWeeklyShareViewState(viewState: Record<string, unknown>): boolean {
  return viewState.share === WEEKLY_SHARE_SCOPE
}

export function weeklyShareTab(requested: unknown): WeeklyShareTab {
  if (requested === 'weekly-meeting') return 'weekly-meeting'
  if (requested === 'okr-review') return 'okr-review'
  return 'weekly-fill'
}

export function weeklyShareURL(currentURL: string, mode: WeeklyShareMode): string {
  const url = new URL(currentURL)
	const tab = mode === 'meeting' ? 'weekly-meeting' : mode === 'review' ? 'okr-review' : 'weekly-fill'
	url.hash = `/weekly-report?tab=${tab}`
  return url.toString()
}
