export type MonitoringRange = 'today' | '24h' | '7d'

const DAY_MS = 24 * 60 * 60 * 1000

export function monitoringRangeBounds(range: MonitoringRange, now = new Date()): { from: Date; until: Date } {
  const until = new Date(now)
  if (range === 'today') {
    const from = new Date(now)
    from.setHours(0, 0, 0, 0)
    return { from, until }
  }
  return {
    from: new Date(now.getTime() - (range === '24h' ? DAY_MS : 7 * DAY_MS)),
    until,
  }
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
