export type OKRSurface = 'okr' | 'weekly-report'

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
  return next
}
