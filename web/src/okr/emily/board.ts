import { createContext, useContext } from 'react'
import type { Entry, EnumValues, Kr, KrOwner, MetricLine, Objective, Point } from './types'

export type SyncState =
  | { kind: 'loading'; message: string }
  | { kind: 'ready'; message: string }
  | { kind: 'saving'; message: string }
  | { kind: 'saved'; message: string }
  | { kind: 'error'; message: string }
  | { kind: 'conflict'; message: string; krId: string; local: Kr; remote: Kr }

export interface BoardApi {
  objectives: Objective[]
  quarter: string
  availableQuarters: string[]
  setQuarter: (quarter: string) => void
  week: string
  previousWeek?: string
  availableWeeks: string[]
  setWeek: (week: string) => void
  enums: EnumValues
  syncState: SyncState
  setKrTitle: (objId: string, krId: string, title: string) => void
  setKrOwner: (krId: string, ownerName: string, ownerOpenId?: string, owners?: KrOwner[]) => void
  setKrPriority: (krId: string, priority: NonNullable<Kr['priority']>) => void
  createObjective: (input: { quarter: string; title: string }) => Promise<void>
  createKr: (objectiveId: string, input: { title: string; ownerName?: string; priority?: NonNullable<Kr['priority']> }) => Promise<void>
  deleteKr: (krId: string) => Promise<void>
  addTag: (krId: string, value: string, type?: string) => void
  removeTag: (krId: string, type: string, value: string) => void
  setMetricNote: (krId: string, note: string) => void
  patchMetric: (krId: string, metricId: string, patch: Partial<MetricLine>) => void
  addMetric: (krId: string) => void
  removeMetric: (krId: string, metricId: string) => void
  setPointTitle: (objId: string, krId: string, pointId: string, title: string) => void
  setPointMeegoLink: (krId: string, pointId: string, patch: Pick<Point, 'meegoWorkItemId' | 'meegoUrl'>) => void
  addPoint: (objId: string, krId: string, kind: Point['kind']) => void
  removePoint: (objId: string, krId: string, pointId: string) => void
  patchEntry: (pointId: string, entryId: string, patch: Partial<Entry>) => void
  addEntry: (pointId: string) => void
  removeEntry: (pointId: string, entryId: string) => void
  reset: () => void
  retry: () => void
  resolveConflict: (choice: 'remote' | 'local') => void
  applySavedKr: (kr: Kr) => void
}

export const BoardContext = createContext<BoardApi | null>(null)

export function useBoard() {
  const ctx = useContext(BoardContext)
  if (!ctx) throw new Error('useBoard 必须在 BoardProvider 内使用')
  return ctx
}

export function uid(prefix: string) {
  return `${prefix}_${Math.random().toString(36).slice(2, 8)}`
}
