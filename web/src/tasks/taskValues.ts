import type { Effect, RunEnrichment } from '../types'

export function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

export function printableValue(value: unknown): string {
  if (typeof value === 'string') return value
  if (value == null) return ''
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function enrichmentItems(value: unknown): RunEnrichment[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    const record = asRecord(item)
    if (!record || typeof record.kind !== 'string' || typeof record.label !== 'string' || !('content' in record)) return []
    return [{ kind: record.kind, label: record.label, content: record.content }]
  })
}

export function effectItems(value: unknown): Effect[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    const record = asRecord(item)
    if (!record) return []
    return [{ ...record, kind: typeof record.kind === 'string' ? record.kind : '' } as Effect]
  })
}

export function stringValue(value: unknown): string | null {
  return typeof value === 'string' && value.trim() ? value : null
}

export function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
