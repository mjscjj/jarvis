import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { APIError, createKR, createObjective as createObjectiveRequest, createProgress, deleteKR, deleteObjective as deleteObjectiveRequest, deleteProgress, deleteWeeklyReportWeek, deleteWeeklyScore, getBoard, getEnums, getWeeklyKR, listWeeklyReportWeeks, reorderKRs, reorderObjectives, replaceKR, replaceKRDefinition, replaceWeeklyKRCore, replaceWeeklyScore, updateObjective as updateObjectiveRequest, updateProgress, type BoardData, type BoardSurface } from './api'
import { BoardContext, uid, type BoardApi, type SyncState } from './board'
import { definitionSignature } from './definition'
import { findKrDraftIssue } from './draftValidation'
import { BUSINESS_CATEGORY_TAG, PRIORITY_TAG, replaceSingleTag } from './hierarchy'
import { swappedOrder } from './ordering'
import { LIGHTS, STATUSES } from './template'
import type { Entry, EnumValues, Kr, Objective, Point, WeekTemplateKey, WeeklyScore } from './types'
import { filterWeekCatalog, previousWeekInCatalog } from './weekCatalog'

const SAVE_DELAY_MS = 700

const DEFAULT_ENUMS: EnumValues = {
	statuses: STATUSES.map((item) => item.value),
	pointKinds: ['strategy', 'product'],
	lights: LIGHTS.map((item) => item.value),
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
  merged.weeklyCoreVersion = remote.weeklyCoreVersion
  for (const point of merged.points) {
    for (const entry of point.entries) {
      const current = remoteEntries.get(entry.id)
      if (current) entry.version = current.entry.version
    }
  }
  return merged
}

function sameWeeklyCore(left: Kr, right: Kr): boolean {
  return left.metricNote === right.metricNote && JSON.stringify(left.metrics) === JSON.stringify(right.metrics)
}

// A filling week may reword a KR and move people, and that lands on the shared
// definition. A definition conflict has to report the week's own view, or the
// merge dialog would offer a remote copy with this week's progress missing.
async function syncWeeklyDefinition(remote: Kr, local: Kr, week: string): Promise<boolean> {
  if (definitionSignature(remote) === definitionSignature(local)) return false
  try {
    await replaceKRDefinition(local)
  } catch (error) {
    if (error instanceof APIError && error.status === 409) {
      throw new APIError(error.message, error.status, error.code, await getWeeklyKR(local.id, week), error.logid)
    }
    throw error
  }
  return true
}

async function syncWeeklyProgress(remote: Kr, local: Kr, week: string): Promise<Kr> {
  try {
    const before = entriesById(remote)
    const after = entriesById(local)
    if (!sameWeeklyCore(remote, local)) await replaceWeeklyKRCore(local, week)
    for (const [id, current] of before) {
      if (!after.has(id)) await deleteProgress(current.entry)
    }
    for (const [id, current] of after) {
      const previous = before.get(id)
      if (!previous) await createProgress(current.pointId, current.entry, week)
      else if (!sameEntry(previous.entry, current.entry)) await updateProgress(current.entry, week)
    }
    // Formal progress writes deliberately return the generic OKR view. The
    // Biz product re-reads its composed view so tags, scores and Meego
    // metadata remain visible after a save.
    return await getWeeklyKR(local.id, week)
  } catch (error) {
    if (error instanceof APIError && error.status === 409) {
      throw new APIError(error.message, error.status, error.code, await getWeeklyKR(local.id, week), error.logid)
    }
    throw error
  }
}

export function BoardProvider({
  children,
  surface = 'okr',
  weekTemplateKey = 'classic',
  initialQuarter = '',
  onQuarterChange,
}: {
  children: ReactNode
  surface?: BoardSurface
  weekTemplateKey?: WeekTemplateKey
  initialQuarter?: string
  onQuarterChange?: (quarter: string) => void
}) {
  const [objectives, setObjectives] = useState<Objective[]>([])
  const [enums, setEnums] = useState<EnumValues>(DEFAULT_ENUMS)
  // No week until the server names one: guessing the current ISO week makes the
  // page act on a scope the quarter may not have.
  const [week, setWeekState] = useState('')
  const [templateKey, setTemplateKey] = useState<WeekTemplateKey>('classic')
  const [quarter, setQuarterState] = useState(initialQuarter)
  const [availableQuarters, setAvailableQuarters] = useState<string[]>(initialQuarter ? [initialQuarter] : [])
  const [previousWeek, setPreviousWeek] = useState<string>()
  const [availableWeeks, setAvailableWeeks] = useState<string[]>([])
  const [syncState, setSyncState] = useState<SyncState>({ kind: 'loading', message: '正在读取本周进展…' })
  const objectivesRef = useRef(objectives)
  const weekRef = useRef('')
  const quarterRef = useRef(initialQuarter)
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
    const draftIssue = findKrDraftIssue(snapshot)
    if (draftIssue) {
      lastFailedKr.current = krId
      setSyncState({ kind: 'error', ...draftIssue })
      return
    }
    setSyncState({ kind: 'saving', message: '正在保存…' })
    try {
      const baseline = serverKrs.current.get(krId)
      if (!baseline) throw new Error('缺少服务端 KR 基线，请重新载入。')
      let saved: Kr
      if (surface === 'weekly-report') {
        await syncWeeklyDefinition(baseline, snapshot, weekRef.current)
        saved = await syncWeeklyProgress(baseline, snapshot, weekRef.current)
      } else {
        saved = await replaceKR(snapshot)
      }
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
    const timer = window.setTimeout(() => {
      timers.current.delete(krId)
      void saveNow(krId)
    }, SAVE_DELAY_MS)
    timers.current.set(krId, timer)
  }, [saveNow])

  useEffect(() => {
    scheduleSaveRef.current = scheduleSave
  }, [scheduleSave])

  const loadRemote = useCallback(async (targetWeek?: string, targetQuarter?: string): Promise<BoardData | undefined> => {
    remoteReady.current = false
    setSyncState({ kind: 'loading', message: '正在读取本周进展…' })
    try {
      let board: BoardData
      let remoteEnums: EnumValues
      if (surface === 'weekly-report') {
        let [catalog, loadedEnums] = await Promise.all([
          listWeeklyReportWeeks(targetQuarter ?? quarterRef.current),
          getEnums(),
        ])
        remoteEnums = loadedEnums
        let filteredWeeks = filterWeekCatalog(catalog.weeks, weekTemplateKey)
        const requestedWeek = targetWeek?.trim() ?? ''
        let selectedWeek = requestedWeek && filteredWeeks.includes(requestedWeek) ? requestedWeek : filteredWeeks[0] ?? ''
        if (selectedWeek) {
          let loaded = await getBoard(catalog.quarter, selectedWeek, surface)
          const fallbackQuarter = loaded.availableQuarters[0]
          if (fallbackQuarter && !loaded.availableQuarters.includes(loaded.quarter)) {
            catalog = await listWeeklyReportWeeks(fallbackQuarter)
            filteredWeeks = filterWeekCatalog(catalog.weeks, weekTemplateKey)
            selectedWeek = requestedWeek && filteredWeeks.includes(requestedWeek) ? requestedWeek : filteredWeeks[0] ?? ''
            loaded = await getBoard(catalog.quarter, selectedWeek, surface)
          }
          board = {
            ...loaded,
            templateKey: weekTemplateKey,
            previousWeek: previousWeekInCatalog(filteredWeeks, selectedWeek),
            availableWeeks: filteredWeeks,
          }
        } else {
          const core = await getBoard(catalog.quarter, '', 'okr')
          board = {
            ...core,
            week: '',
            templateKey: weekTemplateKey,
            previousWeek: undefined,
            availableWeeks: [],
            objectives: [],
          }
        }
      } else {
        [board, remoteEnums] = await Promise.all([
          getBoard(targetQuarter ?? quarterRef.current, targetWeek ?? '', surface),
          getEnums(),
        ])
      }
      publish(board.objectives)
      serverKrs.current = new Map(board.objectives.flatMap((objective) => objective.krs).map((kr) => [kr.id, clone(kr)]))
      revisions.current.clear()
      lastFailedKr.current = null
      quarterRef.current = board.quarter
      setQuarterState(board.quarter)
      setAvailableQuarters(board.availableQuarters)
      onQuarterChange?.(board.quarter)
      weekRef.current = board.week
      setWeekState(board.week)
      setTemplateKey(board.templateKey)
      setPreviousWeek(board.previousWeek)
      setAvailableWeeks(board.availableWeeks)
      setEnums(remoteEnums)
      remoteReady.current = true
      setSyncState({ kind: 'ready', message: board.week ? '本周进展已加载' : surface === 'weekly-report' ? '当前季度暂无对应周次' : 'OKR 已加载' })
      return board
    } catch (error) {
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '加载失败，请稍后重试。' })
    }
  }, [onQuarterChange, publish, surface, weekTemplateKey])

  useEffect(() => {
    const activeTimers = timers.current
    const startupTimer = window.setTimeout(() => void loadRemote(), 0)
    return () => {
      window.clearTimeout(startupTimer)
      for (const timer of activeTimers.values()) window.clearTimeout(timer)
    }
  }, [loadRemote])

  useEffect(() => {
    const nextQuarter = initialQuarter.trim()
    if (!nextQuarter || nextQuarter === quarterRef.current) return
    if (timers.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
      setSyncState({ kind: 'error', message: '请等待当前修改保存后再切换季度。' })
      return
    }
    quarterRef.current = nextQuarter
    void loadRemote('', nextQuarter)
  }, [initialQuarter, loadRemote, syncState.kind])

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

  const saveWeeklyScore = useCallback(async (krId: string, targetKind: 'kr' | 'point', targetId: string, currentScore: WeeklyScore | undefined, score?: number) => {
    if (templateKey !== 'okr_weekly_preview_v1') throw new Error('当前周次不是 OKR 周度 Preview 模板。')
    setSyncState({ kind: 'saving', message: '正在保存评分…' })
    try {
      const input = {
        quarter: quarterRef.current,
        week: weekRef.current,
        targetKind,
        targetId,
        expectedVersion: currentScore?.version ?? 0,
      }
      const saved = score === undefined
        ? await deleteWeeklyScore(input)
        : await replaceWeeklyScore({ ...input, score })
      serverKrs.current.set(krId, clone(saved))
      publish(replaceKrIn(objectivesRef.current, krId, saved))
      setSyncState({ kind: 'saved', message: '评分已保存' })
    } catch (error) {
      if (error instanceof APIError && error.status === 409 && error.data) {
        const current = error.data as Kr
        serverKrs.current.set(krId, clone(current))
        publish(replaceKrIn(objectivesRef.current, krId, current))
        setSyncState({ kind: 'error', message: '评分已被其他人更新，已载入最新值。' })
        return
      }
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '评分保存失败。' })
      throw error
    }
  }, [publish, templateKey])

  const api = useMemo<BoardApi>(() => ({
    objectives,
    quarter,
    availableQuarters,
    setQuarter: (nextQuarter) => {
      if (nextQuarter === quarterRef.current) return
      if (timers.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
        setSyncState({ kind: 'error', message: '请等待当前修改保存后再切换季度。' })
        return
      }
      quarterRef.current = nextQuarter
      void loadRemote('', nextQuarter)
    },
    week,
    templateKey,
    previousWeek,
    availableWeeks,
		setWeek: (nextWeek) => {
      if (nextWeek === weekRef.current) return
      if (timers.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
        setSyncState({ kind: 'error', message: '请等待当前修改保存后再切换周次。' })
        return
      }
      weekRef.current = nextWeek
			setWeekState(nextWeek)
			void loadRemote(nextWeek)
		},
		setWeeklyScope: (nextQuarter, nextWeek) => {
			if (nextQuarter === quarterRef.current && nextWeek === weekRef.current) return true
			if (timers.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
				setSyncState({ kind: 'error', message: '周次已开启；请等待当前修改保存后再切换。' })
				return false
			}
			quarterRef.current = nextQuarter
			weekRef.current = nextWeek
			void loadRemote(nextWeek, nextQuarter)
			return true
		},
		deleteWeeklyScope: async () => {
			const selectedQuarter = quarterRef.current
			const selectedWeek = weekRef.current
			const lifecycleName = weekTemplateKey === 'okr_weekly_preview_v1' ? 'Review' : '周报'
			if (!selectedQuarter || !selectedWeek) throw new Error(`当前没有可删除的${lifecycleName}。`)
			if (timers.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'loading' || syncState.kind === 'conflict') {
				throw new Error('请等待当前修改保存后再删除本周。')
			}
			remoteReady.current = false
			setSyncState({ kind: 'saving', message: `正在删除 ${selectedWeek} ${lifecycleName}…` })
			try {
				const result = await deleteWeeklyReportWeek(selectedQuarter, selectedWeek)
				const loaded = await loadRemote('', selectedQuarter)
				return { ...result, nextWeek: loaded?.week || undefined }
			} catch (error) {
				remoteReady.current = true
				setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '删除本周失败。' })
				throw error
			}
		},
		enums,
    syncState,

    createObjective: async (input) => {
      setSyncState({ kind: 'saving', message: '正在创建目标…' })
      try {
        await createObjectiveRequest(input)
        quarterRef.current = input.quarter
        await loadRemote('', input.quarter)
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '创建目标失败。' })
        throw error
      }
    },

    updateObjective: async (id, title) => {
      const clean = title.trim()
      if (!clean) throw new Error('目标名称不能为空。')
      setSyncState({ kind: 'saving', message: '正在更新目标…' })
      try {
        await updateObjectiveRequest(id, clean)
        publish(objectivesRef.current.map((objective) => objective.id === id ? { ...objective, title: clean } : objective))
        setSyncState({ kind: 'saved', message: '目标已更新' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '更新目标失败。' })
        throw error
      }
    },

    deleteObjective: async (id) => {
      setSyncState({ kind: 'saving', message: '正在删除空目标…' })
      try {
        await deleteObjectiveRequest(id)
        publish(objectivesRef.current.filter((objective) => objective.id !== id))
        setSyncState({ kind: 'saved', message: '空目标已删除' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '删除目标失败。' })
        throw error
      }
    },

    swapObjectives: async (id, targetId) => {
      setSyncState({ kind: 'saving', message: '正在调整顺序…' })
      try {
        const order = await reorderObjectives(quarterRef.current, swappedOrder(objectivesRef.current.map((objective) => objective.id), id, targetId))
        const byId = new Map(objectivesRef.current.map((objective) => [objective.id, objective]))
        publish(order.map((objectiveId) => {
          const objective = byId.get(objectiveId)
          if (!objective) throw new Error('顺序里出现了页面上没有的目标，请重新载入。')
          return objective
        }))
        setSyncState({ kind: 'saved', message: '顺序已保存' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '调整顺序失败。' })
        throw error
      }
    },

    swapKrs: async (objectiveId, krId, targetId) => {
      const objective = objectivesRef.current.find((item) => item.id === objectiveId)
      if (!objective) throw new Error('目标分组不存在，请重新载入。')
      setSyncState({ kind: 'saving', message: '正在调整顺序…' })
      try {
        const order = await reorderKRs(objectiveId, swappedOrder(objective.krs.map((kr) => kr.id), krId, targetId))
        const next = clone(objectivesRef.current)
        const target = next.find((item) => item.id === objectiveId)
        if (!target) throw new Error('目标分组不存在，请重新载入。')
        const byId = new Map(target.krs.map((kr) => [kr.id, kr]))
        target.krs = order.map((id) => {
          const kr = byId.get(id)
          if (!kr) throw new Error('顺序里出现了页面上没有的 KR，请重新载入。')
          return kr
        })
        publish(next)
        setSyncState({ kind: 'saved', message: '顺序已保存' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '调整顺序失败。' })
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
		setKrBusinessCategory: (krId, category) => mutate(krId, (draft) => {
			const kr = findKr(draft, krId)
			if (kr) kr.tags = replaceSingleTag(kr.tags, BUSINESS_CATEGORY_TAG, category)
		}),
		setKrPriority: (krId, priority) => mutate(krId, (draft) => {
			const kr = findKr(draft, krId)
			if (kr) kr.tags = replaceSingleTag(kr.tags, PRIORITY_TAG, priority)
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
    addPointTag: (krId, pointId, value, type = 'custom') => mutate(krId, (draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      const clean = value.trim()
      if (!point || !clean) return
      point.tags ??= []
      if (!point.tags.some((tag) => tag.type === type && tag.value === clean)) point.tags.push({ type, value: clean })
    }),
    removePointTag: (krId, pointId, type, value) => mutate(krId, (draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) point.tags = (point.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value)
    }),
    setPointOwners: (krId, pointId, owners) => mutate(krId, (draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) point.owners = owners
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
      const point: Point = { id: uid('p'), kind, title: '', tags: [], entries: [{ id: uid('e'), status: 'in_progress', text: '', docs: [], images: [] }] }
      const lastSameKind = kr.points.map((item) => item.kind).lastIndexOf(kind)
      if (lastSameKind === -1) kr.points.push(point)
      else kr.points.splice(lastSameKind + 1, 0, point)
    }),
    setPointKind: (krId, pointId, kind) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      const point = kr?.points.find((item) => item.id === pointId)
      if (!kr || !point || point.kind === kind) return
      point.kind = kind
      // Points persist in array order, so the moved one has to land in its new
      // group; left in place it would sort ahead of rows it now sits beside.
      const others = kr.points.filter((item) => item.id !== pointId)
      const lastSameKind = others.map((item) => item.kind).lastIndexOf(kind)
      kr.points = lastSameKind === -1
        ? [...others, point]
        : [...others.slice(0, lastSameKind + 1), point, ...others.slice(lastSameKind + 1)]
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
    addEntry: (pointId, text) => {
      const clean = text.trim()
      if (!clean) return
      const owner = objectivesRef.current.flatMap((item) => item.krs).find((kr) => kr.points.some((point) => point.id === pointId))
      if (!owner) return
      mutate(owner.id, (draft) => findPoint(draft, pointId)?.entries.push({ id: uid('e'), status: 'in_progress', text: clean, docs: [], images: [] }))
    },
    removeEntry: (pointId, entryId) => {
      const owner = objectivesRef.current.flatMap((item) => item.krs).find((kr) => kr.points.some((point) => point.id === pointId))
      if (!owner) return
      mutate(owner.id, (draft) => {
        const point = findPoint(draft, pointId)
        if (point) point.entries = point.entries.filter((item) => item.id !== entryId)
      })
    },
    setKrScore: async (krId, score) => {
      const kr = findKr(objectivesRef.current, krId)
      if (!kr || (score === undefined && !kr.score)) return
      await saveWeeklyScore(krId, 'kr', krId, kr.score, score)
    },
    setPointScore: async (krId, pointId, score) => {
      const point = findKr(objectivesRef.current, krId)?.points.find((item) => item.id === pointId)
      if (!point || (score === undefined && !point.score)) return
      await saveWeeklyScore(krId, 'point', pointId, point.score, score)
    },
    reset: () => void loadRemote(),
    retry: () => {
      if (lastFailedKr.current && remoteReady.current) void saveNow(lastFailedKr.current)
      else void loadRemote()
    },
    resolveConflict,
    applySavedKr: (kr) => publish(replaceKrIn(objectivesRef.current, kr.id, kr)),
  }), [availableQuarters, availableWeeks, enums, loadRemote, mutate, objectives, previousWeek, publish, quarter, resolveConflict, saveNow, saveWeeklyScore, syncState, templateKey, week, weekTemplateKey])

  return <BoardContext.Provider value={api}>{children}</BoardContext.Provider>
}
