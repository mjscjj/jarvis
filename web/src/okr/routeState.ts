export function quarterFromViewState(viewState: Readonly<Record<string, unknown>>): string {
  const value = viewState.quarter
  return typeof value === 'string' ? value.trim() : ''
}

export function quarterForDate(date: Date): string {
  const year = date.getFullYear()
  const quarter = Math.floor(date.getMonth() / 3) + 1
  return `${year}-Q${quarter}`
}

export function okrPlanDefaultQuarter(date = new Date()): string {
  const month = date.getMonth()
  const quarter = Math.floor(month / 3) + 1
  const lastMonthOfQuarter = month % 3 === 2
  if (!lastMonthOfQuarter) return `${date.getFullYear()}-Q${quarter}`
  if (quarter === 4) return `${date.getFullYear() + 1}-Q1`
  return `${date.getFullYear()}-Q${quarter + 1}`
}

export function previousQuarter(quarter: string): string {
  const matched = /^(\d{4})-Q([1-4])$/.exec(quarter.trim())
  if (!matched) return ''
  const year = Number(matched[1])
  const number = Number(matched[2])
  return number === 1 ? `${year - 1}-Q4` : `${year}-Q${number - 1}`
}

export function activeQuarterForViewState(
  viewState: Readonly<Record<string, unknown>>,
  fallbackQuarter: string,
  date = new Date(),
): string {
  const routeQuarter = quarterFromViewState(viewState)
  if (routeQuarter) return routeQuarter
  return viewState.tab === 'okr-plan' ? okrPlanDefaultQuarter(date) : fallbackQuarter
}
