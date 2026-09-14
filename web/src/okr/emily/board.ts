import { createContext, useContext } from 'react'
import type { DeleteWeekResult } from './api'
import type { Entry, EnumValues, Kr, KrOwner, KrPriority, MetricLine, Objective, Point, WeekTemplateKey } from './types'

export type SyncState =
  | { kind: 'loading'; message: string }
  | { kind: 'ready'; message: string }
  | { kind: 'saving'; message: string }
  | { kind: 'saved'; message: string }
  | { kind: 'error'; message: string; title?: string }
  | { kind: 'conflict'; message: string; krId: string; local: Kr; remote: Kr }

export interface BoardApi {
  objectives: Objective[]
  quarter: string
  availableQuarters: string[]
  setQuarter: (quarter: string) => void
  week: string
  templateKey: WeekTemplateKey
  previousWeek?: string
  availableWeeks: string[]
  setWeek: (week: string) => void
	setWeeklyScope: (quarter: string, week: string) => boolean
  deleteWeeklyScope: () => Promise<DeleteWeekResult>
  enums: EnumValues
  syncState: SyncState
  hasPendingChanges: boolean
	setKrTitle: (objId: string, krId: string, title: string) => void
	setKrOwner: (krId: string, ownerName: string, ownerEmail?: string, owners?: KrOwner[]) => void
	setKrBusinessCategory: (krId: string, category: string) => void
	setKrPriority: (krId: string, priority: KrPriority | '') => void
	createObjective: (input: { quarter: string; title: string }) => Promise<void>
	updateObjective: (id: string, title: string) => Promise<void>
	deleteObjective: (id: string) => Promise<void>
	// 交换相邻两行的位置；targetId 是调用方看得见的那一行。
	swapObjectives: (id: string, targetId: string) => Promise<void>
	reorderObjectives: (ids: string[]) => Promise<void>
	swapKrs: (objectiveId: string, krId: string, targetId: string) => Promise<void>
	createKr: (objectiveId: string, input: { title: string; owners?: KrOwner[]; businessCategory: string; priority: KrPriority }) => Promise<void>
  deleteKr: (krId: string) => Promise<void>
  addTag: (krId: string, value: string, type?: string) => void
  removeTag: (krId: string, type: string, value: string) => void
  addPointTag: (krId: string, pointId: string, value: string, type?: string) => void
  removePointTag: (krId: string, pointId: string, type: string, value: string) => void
  setPointOwners: (krId: string, pointId: string, owners: KrOwner[]) => void
  setMetricNote: (krId: string, note: string) => void
  patchMetric: (krId: string, metricId: string, patch: Partial<MetricLine>) => void
  addMetric: (krId: string, initial?: Partial<Omit<MetricLine, 'id'>>) => string
  removeMetric: (krId: string, metricId: string) => void
  setPointTitle: (objId: string, krId: string, pointId: string, title: string) => void
  setPointMeegoLink: (krId: string, pointId: string, patch: Pick<Point, 'meegoWorkItemId' | 'meegoUrl'>) => void
  addPoint: (objId: string, krId: string, kind: Point['kind']) => void
  swapPoints: (krId: string, pointId: string, targetId: string) => void
  setPointKind: (krId: string, pointId: string, kind: Point['kind']) => void
  removePoint: (objId: string, krId: string, pointId: string) => void
  patchEntry: (pointId: string, entryId: string, patch: Partial<Entry>) => void
  addEntry: (pointId: string, text: string) => void
  removeEntry: (pointId: string, entryId: string) => void
  setKrScore: (krId: string, score?: number) => Promise<void>
  setPointScore: (krId: string, pointId: string, score?: number) => Promise<void>
  reset: () => void
  retry: () => void
  resolveConflict: (choice: 'remote' | 'local') => void
  applySavedKr: (kr: Kr, pointId?: string) => void
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
