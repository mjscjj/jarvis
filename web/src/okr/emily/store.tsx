import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { APIError, createKR, createObjective as createObjectiveRequest, createProgress, deleteKR, deleteProgress, getBoard, getEnums, replaceKR, updateProgress, type BoardSurface } from './api'
import { BoardContext, uid, type BoardApi, type SyncState } from './board'
import { LIGHTS, STATUSES } from './template'
import type { Entry, EnumValues, Kr, Objective, Point } from './types'

const SAVE_DELAY_MS = 700

function currentISOWeek(now = new Date()): string {
  const date = new Date(Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()))
  const day = date.getUTCDay() || 7
  date.setUTCDate(date.getUTCDate() + 4 - day)
  const yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1))
  const week = Math.ceil(((date.getTime() - yearStart.getTime()) / 86_400_000 + 1) / 7)
  return `${date.getUTCFullYear()}-W${String(week).padStart(2, '0')}`
}

const DEFAULT_WEEK = currentISOWeek()

const DEFAULT_ENUMS: EnumValues = {
  statuses: STATUSES.map((item) => item.value),
  pointKinds: ['strategy', 'product'],
  lights: LIGHTS.map((item) => item.value),
  priorities: ['p0', 'p1', 'p2'],
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function findPoint(draft: Objective[], pointId: string): Point | undefined {
  for (const objective of draft) {
    for (const kr of objective.krs) {
      const point = kr.points.find((item) => item.id === pointId)
      if (point) return point
    }
  }
}

function findKr(draft: Objective[], krId: string): Kr | undefined {
  for (const objective of draft) {
    const kr = objective.krs.find((item) => item.id === krId)
    if (kr) return kr
  }
}

function replaceKrIn(objectives: Objective[], krId: string, replacement: Kr): Objective[] {
  const next = clone(objectives)
  for (const objective of next) {
    const index = objective.krs.findIndex((item) => item.id === krId)
    if (index >= 0) {
      objective.krs[index] = replacement
      break
    }
  }
  return next
}

function entriesById(kr: Kr): Map<string, { pointId: string; entry: Entry }> {
  const result = new Map<string, { pointId: string; entry: Entry }>()
  for (const point of kr.points) {
    for (const entry of point.entries) result.set(entry.id, { pointId: point.id, entry })
  }
  return result
}

function sameEntry(left: Entry, right: Entry): boolean {
  return left.status === right.status && left.text === right.text &&
    JSON.stringify(left.docs ?? []) === JSON.stringify(right.docs ?? []) &&
    JSON.stringify(left.images ?? []) === JSON.stringify(right.images ?? []) &&
    (left.source ?? 'manual') === (right.source ?? 'manual') &&
    (left.needsReview ?? false) === (right.needsReview ?? false)
}

function applyEntryVersions(local: Kr, remote: Kr): Kr {
  const merged = clone(local)
  const remoteEntries = entriesById(remote)
  merged.version = remote.version
  for (const point of merged.points) {
    for (const entry of point.entries) {
      const current = remoteEntries.get(entry.id)
      if (current) entry.version = current.entry.version
    }
  }
  return merged
}

async function syncWeeklyProgress(remote: Kr, local: Kr, week: string): Promise<Kr> {
  const before = entriesById(remote)
  const after = entriesById(local)
  let saved = remote
  for (const [id, current] of before) {
    if (!after.has(id)) saved = await deleteProgress(current.entry)
  }
  for (const [id, current] of after) {
    const previous = before.get(id)
    if (!previous) saved = await createProgress(current.pointId, current.entry, week)
    else if (!sameEntry(previous.entry, current.entry)) saved = await updateProgress(current.entry, week)
  }
  return saved
}

export function BoardProvider({ children, surface = 'okr' }: { children: ReactNode; surface?: BoardSurface }) {
  const [objectives, setObjectives] = useState<Objective[]>([])
  const [enums, setEnums] = useState<EnumValues>(DEFAULT_ENUMS)
  const [week, setWeekState] = useState(DEFAULT_WEEK)
  const [quarter, setQuarter] = useState('')
  const [previousWeek, setPreviousWeek] = useState<string>()
  const [availableWeeks, setAvailableWeeks] = useState<string[]>([DEFAULT_WEEK])
  const [syncState, setSyncState] = useState<SyncState>({ kind: 'loading', message: '正在读取本周进展…' })
  const objectivesRef = useRef(objectives)
  const weekRef = useRef(DEFAULT_WEEK)
  const quarterRef = useRef('')
  const remoteReady = useRef(false)
  const serverKrs = useRef(new Map<string, Kr>())
  const revisions = useRef(new Map<string, number>())
  const timers = useRef(new Map<string, number>())
  const lastFailedKr = useRef<string | null>(null)

  const publish = useCallback((next: Objective[]) => {
    objectivesRef.current = next
    setObjectives(next)
  }, [])

  const scheduleSaveRef = useRef<(krId: string) => void>(() => undefined)

  const saveNow = useCallback(async (krId: string) => {
    if (!remoteReady.current) return
    const current = findKr(objectivesRef.current, krId)
    if (!current) return
    const snapshot = clone(current)
    const revision = revisions.current.get(krId) ?? 0
    setSyncState({ kind: 'saving', message: '正在保存…' })
    try {
      const baseline = serverKrs.current.get(krId)
      if (!baseline) throw new Error('缺少服务端 KR 基线，请重新载入。')
      const saved = surface === 'weekly-report'
        ? await syncWeeklyProgress(baseline, snapshot, weekRef.current)
        : await replaceKR(snapshot)
      serverKrs.current.set(krId, clone(saved))
      lastFailedKr.current = null
      if ((revisions.current.get(krId) ?? 0) === revision) {
        publish(replaceKrIn(objectivesRef.current, krId, saved))
        setSyncState({ kind: 'saved', message: '已自动保存' })
      } else {
        const latest = findKr(objectivesRef.current, krId)
        if (latest) publish(replaceKrIn(objectivesRef.current, krId, applyEntryVersions(latest, saved)))
        scheduleSaveRef.current(krId)
      }
    } catch (error) {
      lastFailedKr.current = krId
      if (error instanceof APIError && error.status === 409 && error.data) {
        setSyncState({ kind: 'conflict', message: '这条 KR 刚被其他人更新，请选择保留哪一版。', krId, local: snapshot, remote: error.data as Kr })
        return
      }
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '保存失败，请稍后重试。' })
    }
  }, [publish, surface])

  const scheduleSave = useCallback((krId: string) => {
    const previous = timers.current.get(krId)
    if (previous) window.clearTimeout(previous)
    timers.current.set(krId, window.setTimeout(() => void saveNow(krId), SAVE_DELAY_MS))
  }, [saveNow])

  useEffect(() => {
    scheduleSaveRef.current = scheduleSave
  }, [scheduleSave])

  const loadRemote = useCallback(async (targetWeek?: string) => {
    remoteReady.current = false
    setSyncState({ kind: 'loading', message: '正在读取本周进展…' })
    try {
      const [board, remoteEnums] = await Promise.all([getBoard(quarterRef.current, targetWeek ?? '', surface), getEnums()])
      publish(board.objectives)
      serverKrs.current = new Map(board.objectives.flatMap((objective) => objective.krs).map((kr) => [kr.id, clone(kr)]))
      quarterRef.current = board.quarter
      setQuarter(board.quarter)
      weekRef.current = board.week
      setWeekState(board.week)
      setPreviousWeek(board.previousWeek)
      setAvailableWeeks(board.availableWeeks)
      setEnums(remoteEnums)
      remoteReady.current = true
      setSyncState({ kind: 'ready', message: '本周进展已加载' })
    } catch (error) {
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '加载失败，请稍后重试。' })
    }
  }, [publish, surface])

  useEffect(() => {
    const activeTimers = timers.current
    const startupTimer = window.setTimeout(() => void loadRemote(), 0)
    return () => {
      window.clearTimeout(startupTimer)
      for (const timer of activeTimers.values()) window.clearTimeout(timer)
    }
  }, [loadRemote])

  const mutate = useCallback((krId: string, fn: (draft: Objective[]) => void) => {
    const draft = clone(objectivesRef.current)
    fn(draft)
    publish(draft)
    revisions.current.set(krId, (revisions.current.get(krId) ?? 0) + 1)
    scheduleSave(krId)
  }, [publish, scheduleSave])

  const resolveConflict = useCallback((choice: 'remote' | 'local') => {
    if (syncState.kind !== 'conflict') return
    const selected = choice === 'remote' ? syncState.remote : applyEntryVersions(syncState.local, syncState.remote)
    serverKrs.current.set(syncState.krId, clone(syncState.remote))
    publish(replaceKrIn(objectivesRef.current, syncState.krId, selected))
    if (choice === 'remote') {
      setSyncState({ kind: 'saved', message: '已载入他人更新' })
      return
    }
    revisions.current.set(syncState.krId, (revisions.current.get(syncState.krId) ?? 0) + 1)
    scheduleSave(syncState.krId)
  }, [publish, scheduleSave, syncState])

  const api = useMemo<BoardApi>(() => ({
    objectives,
    quarter,
    week,
    previousWeek,
    availableWeeks,
    setWeek: (nextWeek) => {
      if (nextWeek === weekRef.current) return
      weekRef.current = nextWeek
      setWeekState(nextWeek)
      void loadRemote(nextWeek)
    },
    enums,
    syncState,

    createObjective: async (input) => {
      setSyncState({ kind: 'saving', message: '正在创建目标…' })
      try {
        await createObjectiveRequest(input)
        quarterRef.current = input.quarter
        await loadRemote()
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '创建目标失败。' })
        throw error
      }
    },

    createKr: async (objectiveId, input) => {
      setSyncState({ kind: 'saving', message: '正在创建 KR…' })
      try {
        const created = await createKR(objectiveId, input)
        const next = clone(objectivesRef.current)
        const objective = next.find((item) => item.id === objectiveId)
        if (!objective) throw new Error('目标分组不存在，请重新载入。')
        objective.krs.push(created)
        publish(next)
        setSyncState({ kind: 'saved', message: 'KR 已创建' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '创建 KR 失败。' })
        throw error
      }
    },
    deleteKr: async (krId) => {
      const current = findKr(objectivesRef.current, krId)
      if (!current) return
      const pending = timers.current.get(krId)
      if (pending) window.clearTimeout(pending)
      timers.current.delete(krId)
      setSyncState({ kind: 'saving', message: '正在删除 KR…' })
      try {
        await deleteKR(current)
        const next = clone(objectivesRef.current)
        for (const objective of next) objective.krs = objective.krs.filter((item) => item.id !== krId)
        revisions.current.delete(krId)
        publish(next)
        setSyncState({ kind: 'saved', message: 'KR 已删除' })
      } catch (error) {
        if (error instanceof APIError && error.status === 409) {
          setSyncState({ kind: 'error', message: '这条 KR 已被其他人更新，请重新载入后再删除。' })
        } else {
          setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '删除 KR 失败。' })
        }
        throw error
      }
    },

    setKrTitle: (_objId, krId, title) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.title = title
    }),
    setKrOwner: (krId, ownerName, ownerOpenId, owners) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) {
        kr.ownerName = ownerName
        if (ownerOpenId !== undefined) kr.ownerOpenId = ownerOpenId
        if (owners !== undefined) {
          kr.owners = owners
        } else {
          const previous = kr.owners ?? []
          kr.owners = ownerName.split(/[、,，;；]/).map((name) => name.trim()).filter(Boolean).map((name, index) => ({
            name,
            openId: previous.find((owner) => owner.name === name)?.openId ?? (index === 0 ? (ownerOpenId ?? kr.ownerOpenId ?? '') : ''),
          }))
        }
      }
    }),
    setKrPriority: (krId, priority) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.priority = priority
    }),
    addTag: (krId, value, type = 'custom') => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      const clean = value.trim()
      if (!kr || !clean) return
      kr.tags ??= []
      if (!kr.tags.some((tag) => tag.type === type && tag.value === clean)) kr.tags.push({ type, value: clean })
    }),
    removeTag: (krId, type, value) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.tags = (kr.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value)
    }),
    setMetricNote: (krId, note) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.metricNote = note
    }),
    patchMetric: (krId, metricId, patch) => mutate(krId, (draft) => {
      const metric = findKr(draft, krId)?.metrics.find((item) => item.id === metricId)
      if (metric) Object.assign(metric, patch)
    }),
    addMetric: (krId) => mutate(krId, (draft) => {
      findKr(draft, krId)?.metrics.push({ id: uid('m'), text: '', light: 'green', images: [] })
    }),
    removeMetric: (krId, metricId) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.metrics = kr.metrics.filter((item) => item.id !== metricId)
    }),
    setPointTitle: (_objId, krId, pointId, title) => mutate(krId, (draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) point.title = title
    }),
    setPointMeegoLink: (krId, pointId, patch) => mutate(krId, (draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) Object.assign(point, patch)
    }),
    addPoint: (_objId, krId, kind) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (!kr) return
      const point: Point = { id: uid('p'), kind, title: '', entries: [{ id: uid('e'), status: 'in_progress', text: '', docs: [], images: [] }] }
      const lastSameKind = kr.points.map((item) => item.kind).lastIndexOf(kind)
      if (lastSameKind === -1) kr.points.push(point)
      else kr.points.splice(lastSameKind + 1, 0, point)
    }),
    removePoint: (_objId, krId, pointId) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.points = kr.points.filter((item) => item.id !== pointId)
    }),
    patchEntry: (pointId, entryId, patch) => {
      const owner = objectivesRef.current.flatMap((item) => item.krs).find((kr) => kr.points.some((point) => point.id === pointId))
      if (!owner) return
      mutate(owner.id, (draft) => {
        const entry = findPoint(draft, pointId)?.entries.find((item) => item.id === entryId)
        if (entry) Object.assign(entry, patch)
      })
    },
    addEntry: (pointId) => {
      const owner = objectivesRef.current.flatMap((item) => item.krs).find((kr) => kr.points.some((point) => point.id === pointId))
      if (!owner) return
      mutate(owner.id, (draft) => findPoint(draft, pointId)?.entries.push({ id: uid('e'), status: 'in_progress', text: '', docs: [], images: [] }))
    },
    removeEntry: (pointId, entryId) => {
      const owner = objectivesRef.current.flatMap((item) => item.krs).find((kr) => kr.points.some((point) => point.id === pointId))
      if (!owner) return
      mutate(owner.id, (draft) => {
        const point = findPoint(draft, pointId)
        if (point) point.entries = point.entries.filter((item) => item.id !== entryId)
      })
    },
    reset: () => void loadRemote(),
    retry: () => {
      if (lastFailedKr.current && remoteReady.current) void saveNow(lastFailedKr.current)
      else void loadRemote()
    },
    resolveConflict,
    applySavedKr: (kr) => publish(replaceKrIn(objectivesRef.current, kr.id, kr)),
  }), [availableWeeks, enums, loadRemote, mutate, objectives, previousWeek, publish, quarter, resolveConflict, saveNow, syncState, week])

  return <BoardContext.Provider value={api}>{children}</BoardContext.Provider>
}
