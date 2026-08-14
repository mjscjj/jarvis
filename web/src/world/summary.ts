export function countChars(text: string): number {
  return Array.from(text).length
}

export function summaryIndexLine(summary: string | null | undefined): string {
  if (!summary) return ''
  return summary.split(/\r?\n/, 1)[0]?.trim() ?? ''
}

export type SummaryMeterTone = 'ok' | 'warn' | 'danger'

export function summaryMeterTone(count: number, max: number): SummaryMeterTone {
  if (max <= 0 || count > max) return 'danger'
  if (count >= max * 0.9) return 'danger'
  if (count >= max * 0.75) return 'warn'
  return 'ok'
}
