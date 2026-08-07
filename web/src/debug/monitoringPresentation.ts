export type MonitoringRange = 'today' | '24h' | '7d'

const DAY_MS = 24 * 60 * 60 * 1000

export function monitoringRangeBounds(range: MonitoringRange, now = new Date()): { from: Date; until: Date } {
  const until = new Date(now)
  if (range === 'today') {
    const from = new Date(now)
    from.setHours(0, 0, 0, 0)
    return { from, until }
  }
  if (range === '24h') {
    const from = new Date(now.getTime() - DAY_MS)
    from.setMinutes(0, 0, 0)
    return { from, until }
  }
  const from = new Date(now)
  from.setHours(0, 0, 0, 0)
  from.setDate(from.getDate() - 6)
  return {
    from,
    until,
  }
}

export function formatMonitoringRate(value: number | null): string {
  return value == null ? '—' : `${(value * 100).toFixed(value < 0.1 ? 1 : 0)}%`
}

export function formatMonitoringDuration(value: number | null): string {
  if (value == null) return '—'
  if (value < 1000) return `${Math.round(value)} ms`
  if (value < 60_000) return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)} 秒`
  if (value < 3_600_000) return `${Math.floor(value / 60_000)} 分 ${Math.round((value % 60_000) / 1000)} 秒`
  return `${Math.floor(value / 3_600_000)} 小时 ${Math.round((value % 3_600_000) / 60_000)} 分`
}

export function formatMonitoringCount(value: number | null): string {
  return value == null ? '—' : new Intl.NumberFormat('zh-CN').format(value)
}
