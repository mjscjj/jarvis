import type { WeeklyDataset } from '../navigation'
import type { WeeklyReportWeek } from './api'
import type { WeekTemplateKey } from './types'

export function templateKeyForDataset(dataset: WeeklyDataset): WeekTemplateKey {
  return dataset === 'review' ? 'okr_weekly_preview_v1' : 'classic'
}

export function isReviewTemplate(templateKey: WeekTemplateKey): boolean {
  return templateKey === 'okr_weekly_preview_v1'
}

export function filterWeekCatalog(weeks: WeeklyReportWeek[], templateKey: WeekTemplateKey): string[] {
  return weeks.filter((week) => week.templateKey === templateKey).map((week) => week.week)
}

export function previousWeekInCatalog(weeks: string[], selected: string): string | undefined {
  return weeks.find((week) => week < selected)
}
