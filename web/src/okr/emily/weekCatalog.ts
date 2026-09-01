import type { WeeklyWorkspaceMode } from '../navigation'
import type { WeeklyReportWeek } from './api'
import type { WeekTemplateKey } from './types'

export function templateKeyForWeeklyMode(mode: WeeklyWorkspaceMode): WeekTemplateKey {
  return mode === 'review' ? 'okr_weekly_preview_v1' : 'classic'
}

export function filterWeekCatalog(weeks: WeeklyReportWeek[], templateKey: WeekTemplateKey): string[] {
  return weeks.filter((week) => week.templateKey === templateKey).map((week) => week.week)
}

export function previousWeekInCatalog(weeks: string[], selected: string): string | undefined {
  return weeks.find((week) => week < selected)
}
