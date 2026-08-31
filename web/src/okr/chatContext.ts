export type OKRSurface = 'okr' | 'weekly-report'

const TARGET_KEYS = ['objective_id', 'kr_id', 'point_id', 'progress_id'] as const

export interface OKRChatTarget {
  objectiveId?: string
  krId?: string
  pointId?: string
  progressId?: string
}

export function withOKRScope(
  current: Readonly<Record<string, string>>,
  surface: OKRSurface,
  quarter: string,
  week: string,
): Record<string, string> {
  const next: Record<string, string> = { ...current, quarter }
  const expectedWeek = surface === 'weekly-report' && week ? week : undefined
  if (expectedWeek) next.week = expectedWeek
  else delete next.week
  if (current.quarter !== quarter || current.week !== expectedWeek) {
    for (const key of TARGET_KEYS) delete next[key]
  }
  return next
}

export function withOKRTarget(
  current: Readonly<Record<string, string>>,
  surface: OKRSurface,
  quarter: string,
  week: string,
  target: OKRChatTarget,
): Record<string, string> {
  const next = withOKRScope(current, surface, quarter, week)
  for (const key of TARGET_KEYS) delete next[key]
  if (target.objectiveId) next.objective_id = target.objectiveId
  if (target.krId) next.kr_id = target.krId
  if (target.pointId) next.point_id = target.pointId
  if (target.progressId) next.progress_id = target.progressId
  return next
}
