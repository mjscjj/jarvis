import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { APIError, createOKRPlan, createOKRPlanObjective, deleteOKRPlan, deleteOKRPlanObjective, getEnums, getOKRPlan, listOKRPlans, reorderOKRPlanObjectives, updateOKRPlanObjective } from './api'
import { BoardContext, uid, type BoardApi, type SyncState } from './board'
import { BUSINESS_CATEGORY_TAG, PRIORITY_TAG, replaceSingleTag } from './hierarchy'
import { swappedOrder } from './ordering'
import type { EnumValues, Kr, KrOwner, KrPriority, MetricLine, Objective, OKRPlan, OKRPlanSummary, Point, WeekTemplateKey } from './types'
import { LIGHTS, STATUSES } from './template'

const SAVE_DELAY_MS = 700

const DEFAULT_ENUMS: EnumValues = {
  statuses: STATUSES.map((item) => item.value),
  pointKinds: ['strategy', 'product'],
  lights: LIGHTS.map((item) => item.value),
}

function currentQuarter(): string {
  const now = new Date()
  return `${now.getFullYear()}-Q${Math.floor(now.getMonth() / 3) + 1}`
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function findKr(draft: Objective[], krId: string): Kr | undefined {
  for (const objective of draft) {
    const kr = objective.krs.find((item) => item.id === krId)
    if (kr) return kr
  }
}

function findPoint(draft: Objective[], pointId: string): Point | undefined {
  for (const objective of draft) {
    for (const kr of objective.krs) {
      const point = kr.points.find((item) => item.id === pointId)
      if (point) return point
    }
  }
}

export interface PlanBoardApi extends BoardApi {
  plan?: OKRPlan
  plans: OKRPlanSummary[]
  selectPlan: (id: string) => void
  createPlan: (input: { quarter: string; title: string }) => Promise<void>
  deleteCurrentPlan: () => Promise<void>
}

export const PlanBoardContext = createContext<PlanBoardApi | null>(null)

export function usePlanBoard() {
  const ctx = useContext(PlanBoardContext)
  if (!ctx) throw new Error('usePlanBoard 必须在 PlanBoardProvider 内使用')
  return ctx
}

export function PlanBoardProvider({ children, initialQuarter = '', initialPlanId = '', onQuarterChange }: { children: ReactNode; initialQuarter?: string; initialPlanId?: string; onQuarterChange?: (quarter: string) => void }) {
  const [quarter, setQuarterState] = useState(initialQuarter || currentQuarter())
  const [availableQuarters, setAvailableQuarters] = useState<string[]>(initialQuarter ? [initialQuarter] : [currentQuarter()])
  const [plans, setPlans] = useState<OKRPlanSummary[]>([])
  const [plan, setPlan] = useState<OKRPlan>()
  const [objectives, setObjectives] = useState<Objective[]>([])
  const [enums, setEnums] = useState<EnumValues>(DEFAULT_ENUMS)
  const [syncState, setSyncState] = useState<SyncState>({ kind: 'loading', message: '正在读取 Biz OKR Plan…' })
  const quarterRef = useRef(quarter)
  const planRef = useRef<OKRPlan | undefined>(undefined)
  const objectivesRef = useRef<Objective[]>([])
  const saveTimer = useRef(0)
  const dirtyObjectives = useRef(new Set<string>())
  const deletedObjectives = useRef(new Map<string, Objective>())
  const objectiveRevisions = useRef(new Map<string, number>())
  const objectiveOrderDirty = useRef(false)
  const objectiveOrderRevision = useRef(0)
  const saveInFlight = useRef(false)
  const saveAgain = useRef(false)
  const scheduleSaveRef = useRef<() => void>(() => undefined)
  const remoteReady = useRef(false)

  const publishPlan = useCallback((next?: OKRPlan) => {
    planRef.current = next
    setPlan(next)
    const nextObjectives = clone(next?.objectives ?? [])
    objectivesRef.current = nextObjectives
    setObjectives(nextObjectives)
  }, [])

  const loadRemote = useCallback(async (targetQuarter?: string, targetPlanId?: string) => {
    remoteReady.current = false
    setSyncState({ kind: 'loading', message: '正在读取 Biz OKR Plan…' })
    try {
      const list = await listOKRPlans(targetQuarter ?? quarterRef.current)
      const remoteEnums = await getEnums()
      const selected = targetPlanId ? list.plans.find((item) => item.id === targetPlanId) : list.plans[0]
      const loadedPlan = selected ? await getOKRPlan(selected.id) : undefined
      quarterRef.current = list.quarter
      setQuarterState(list.quarter)
      onQuarterChange?.(list.quarter)
      setAvailableQuarters(list.availableQuarters)
      setPlans(list.plans)
      setEnums(remoteEnums)
      publishPlan(loadedPlan)
      dirtyObjectives.current.clear()
      deletedObjectives.current.clear()
      objectiveRevisions.current.clear()
      objectiveOrderDirty.current = false
      objectiveOrderRevision.current = 0
      remoteReady.current = true
      setSyncState({ kind: 'ready', message: loadedPlan ? 'Biz OKR Plan 已加载' : '当前季度暂无 Biz OKR Plan' })
      return { list, plan: loadedPlan }
    } catch (error) {
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '加载 Biz OKR Plan 失败。' })
    }
  }, [onQuarterChange, publishPlan])

  useEffect(() => {
    const timer = window.setTimeout(() => void loadRemote(initialQuarter, initialPlanId), 0)
    return () => {
      window.clearTimeout(timer)
      window.clearTimeout(saveTimer.current)
      dirtyObjectives.current.clear()
      deletedObjectives.current.clear()
      objectiveRevisions.current.clear()
      objectiveOrderDirty.current = false
      objectiveOrderRevision.current = 0
    }
  }, [loadRemote])

  useEffect(() => {
    const nextQuarter = initialQuarter.trim()
    if (!nextQuarter || nextQuarter === quarterRef.current) return
    if (syncState.kind === 'saving') {
      setSyncState({ kind: 'error', message: '请等待当前 Plan 保存后再切换季度。' })
      return
    }
    window.clearTimeout(saveTimer.current)
    quarterRef.current = nextQuarter
    void loadRemote(nextQuarter)
  }, [initialQuarter, loadRemote, syncState.kind])

  const mergeSavedPlan = useCallback((saved: OKRPlan, objectiveId?: string) => {
    const current = planRef.current
    if (!current) return
    const nextObjective = objectiveId ? saved.objectives.find((item) => item.id === objectiveId) : undefined
    const nextObjectives = objectiveId && nextObjective
      ? objectivesRef.current.map((item) => item.id === objectiveId ? { ...item, version: nextObjective.version } : item)
      : objectivesRef.current
    const nextPlan = { ...current, version: saved.version, updatedBy: saved.updatedBy, updatedAt: saved.updatedAt, objectives: nextObjectives }
    planRef.current = nextPlan
    setPlan(nextPlan)
    objectivesRef.current = nextObjectives
    setObjectives(nextObjectives)
    setPlans((items) => items.map((item) => item.id === saved.id ? {
      ...item,
      version: saved.version,
      objectiveCount: nextObjectives.length,
      krCount: nextObjectives.reduce((total, objective) => total + objective.krs.length, 0),
      updatedAt: saved.updatedAt,
    } : item))
  }, [])

  const replaceConflictedObjective = useCallback((saved: OKRPlan, objectiveId: string) => {
    const current = planRef.current
    if (!current) return
    const remoteObjective = saved.objectives.find((item) => item.id === objectiveId)
    let nextObjectives = objectivesRef.current.filter((item) => item.id !== objectiveId)
    if (remoteObjective) {
      const remoteOrder = new Map(saved.objectives.map((item, index) => [item.id, index]))
      const remoteIndex = remoteOrder.get(objectiveId) ?? nextObjectives.length
      const insertAt = nextObjectives.findIndex((item) => (remoteOrder.get(item.id) ?? Number.MAX_SAFE_INTEGER) > remoteIndex)
      nextObjectives = clone(nextObjectives)
      nextObjectives.splice(insertAt < 0 ? nextObjectives.length : insertAt, 0, clone(remoteObjective))
    }
    const nextPlan = { ...current, version: saved.version, updatedBy: saved.updatedBy, updatedAt: saved.updatedAt, objectives: nextObjectives }
    planRef.current = nextPlan
    setPlan(nextPlan)
    objectivesRef.current = nextObjectives
    setObjectives(nextObjectives)
    setPlans((items) => items.map((item) => item.id === saved.id ? {
      ...item,
      version: saved.version,
      objectiveCount: nextObjectives.length,
      krCount: nextObjectives.reduce((total, objective) => total + objective.krs.length, 0),
      updatedAt: saved.updatedAt,
    } : item))
  }, [])

  const mergeSavedObjectiveOrder = useCallback((saved: OKRPlan) => {
    const currentByID = new Map(objectivesRef.current.map((objective) => [objective.id, objective]))
    const nextObjectives = saved.objectives.map((remoteObjective) => {
      const current = currentByID.get(remoteObjective.id)
      return current ? { ...current, version: remoteObjective.version } : remoteObjective
    })
    publishPlan({ ...saved, objectives: nextObjectives })
    setPlans((items) => items.map((item) => item.id === saved.id ? {
      ...item,
      version: saved.version,
      objectiveCount: nextObjectives.length,
      krCount: nextObjectives.reduce((total, objective) => total + objective.krs.length, 0),
      updatedAt: saved.updatedAt,
    } : item))
  }, [publishPlan])

  const saveNow = useCallback(async () => {
    if (!remoteReady.current || !planRef.current) return
    if (saveInFlight.current) {
      saveAgain.current = true
      return
    }
    saveInFlight.current = true
    let failed = false
    let reordered = false
    const conflictedObjectives: string[] = []
    setSyncState({ kind: 'saving', message: dirtyObjectives.current.size === 0 && objectiveOrderDirty.current ? '正在调整顺序…' : '正在保存 Plan…' })
    try {
      do {
        saveAgain.current = false
        const pending = Array.from(dirtyObjectives.current)
        for (const objectiveId of pending) {
          const objective = objectivesRef.current.find((item) => item.id === objectiveId)
          const deleted = deletedObjectives.current.get(objectiveId)
          const revision = objectiveRevisions.current.get(objectiveId) ?? 0
          try {
            if (objective) {
              const saved = (objective.version ?? 0) === 0 && !planRef.current.objectives.some((item) => item.id === objectiveId)
                ? await createOKRPlanObjective(planRef.current.id, objective)
                : await updateOKRPlanObjective(planRef.current.id, objective)
              mergeSavedPlan(saved, objectiveId)
              if ((objectiveRevisions.current.get(objectiveId) ?? 0) === revision) {
                dirtyObjectives.current.delete(objectiveId)
                objectiveRevisions.current.delete(objectiveId)
              }
            } else if (deleted) {
              if (planRef.current.objectives.some((item) => item.id === objectiveId)) {
                await deleteOKRPlanObjective(planRef.current.id, deleted)
              }
              dirtyObjectives.current.delete(objectiveId)
              deletedObjectives.current.delete(objectiveId)
              objectiveRevisions.current.delete(objectiveId)
              mergeSavedPlan({ ...planRef.current, version: planRef.current.version + 1, updatedAt: new Date().toISOString() })
            } else {
              dirtyObjectives.current.delete(objectiveId)
              objectiveRevisions.current.delete(objectiveId)
            }
          } catch (error) {
            if (error instanceof APIError && error.status === 409 && error.data) {
              const remote = error.data as OKRPlan
              const title = objective?.title || deleted?.title || objectiveId
              replaceConflictedObjective(remote, objectiveId)
              dirtyObjectives.current.delete(objectiveId)
              deletedObjectives.current.delete(objectiveId)
              objectiveRevisions.current.delete(objectiveId)
              conflictedObjectives.push(title)
              continue
            }
            throw error
          }
        }
        if (objectiveOrderDirty.current && planRef.current) {
          const revision = objectiveOrderRevision.current
          let saved: OKRPlan
          try {
            saved = await reorderOKRPlanObjectives(planRef.current.id, objectivesRef.current.map((objective) => objective.id))
          } catch (reorderError) {
            try {
              const remote = await getOKRPlan(planRef.current.id)
              if (objectiveOrderRevision.current === revision && dirtyObjectives.current.size === 0) {
                objectiveOrderDirty.current = false
                objectiveOrderRevision.current = 0
                publishPlan(remote)
              }
            } catch {
              // Keep the intended local order retryable when even the readback fails.
            }
            throw reorderError
          }
          if (objectiveOrderRevision.current === revision) {
            objectiveOrderDirty.current = false
            reordered = true
            mergeSavedObjectiveOrder(saved)
          } else {
            mergeSavedPlan(saved)
          }
        }
      } while (saveAgain.current || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)
      if (conflictedObjectives.length > 0) {
        setSyncState({ kind: 'error', message: `${conflictedObjectives.join('、')} 已被其他人更新，已只刷新冲突的 O；其他 O 已继续保存。` })
      } else {
        setSyncState({ kind: 'saved', message: reordered ? '顺序已保存' : 'Plan 已保存' })
      }
    } catch (error) {
      failed = true
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '保存 Plan 失败。' })
    } finally {
      saveInFlight.current = false
      if (!failed && (dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)) scheduleSaveRef.current()
    }
  }, [mergeSavedObjectiveOrder, mergeSavedPlan, publishPlan, replaceConflictedObjective])

  const scheduleSave = useCallback(() => {
    window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => void saveNow(), SAVE_DELAY_MS)
  }, [saveNow])

  useEffect(() => {
    scheduleSaveRef.current = scheduleSave
  }, [scheduleSave])

  const mutate = useCallback((fn: (draft: Objective[]) => void) => {
    if (!planRef.current) return
    const before = objectivesRef.current
    const draft = clone(before)
    fn(draft)
    objectivesRef.current = draft
    setObjectives(draft)
    const beforeByID = new Map(before.map((item) => [item.id, item]))
    const afterIDs = new Set(draft.map((item) => item.id))
    for (const objective of draft) {
      const previous = beforeByID.get(objective.id)
      if (!previous || JSON.stringify(previous) !== JSON.stringify(objective)) {
        dirtyObjectives.current.add(objective.id)
        objectiveRevisions.current.set(objective.id, (objectiveRevisions.current.get(objective.id) ?? 0) + 1)
      }
    }
    for (const objective of before) {
      if (!afterIDs.has(objective.id)) {
        dirtyObjectives.current.add(objective.id)
        deletedObjectives.current.set(objective.id, objective)
        objectiveRevisions.current.set(objective.id, (objectiveRevisions.current.get(objective.id) ?? 0) + 1)
      }
    }
    scheduleSave()
  }, [scheduleSave])

  const queueObjectiveOrder = useCallback(async (ids: string[]) => {
    const current = objectivesRef.current
    if (!planRef.current || ids.length !== current.length) throw new Error('目标顺序已经变化，请重新载入后再试。')
    const byID = new Map(current.map((objective) => [objective.id, objective]))
    const next = ids.map((id) => byID.get(id)).filter((objective): objective is Objective => Boolean(objective))
    if (next.length !== current.length || new Set(ids).size !== ids.length) throw new Error('目标顺序已经变化，请重新载入后再试。')
    if (ids.every((id, index) => current[index]?.id === id)) return
    objectivesRef.current = next
    setObjectives(next)
    const nextPlan = { ...planRef.current, objectives: next }
    planRef.current = nextPlan
    setPlan(nextPlan)
    objectiveOrderDirty.current = true
    objectiveOrderRevision.current += 1
    window.clearTimeout(saveTimer.current)
    await saveNow()
  }, [saveNow])

  const api = useMemo<PlanBoardApi>(() => ({
    objectives,
    plan,
    plans,
    quarter,
    availableQuarters,
    setQuarter: (nextQuarter) => {
      if (nextQuarter === quarterRef.current) return
      if (syncState.kind === 'saving') {
        setSyncState({ kind: 'error', message: '请等待当前 Biz OKR Plan 保存后再切换季度。' })
        return
      }
      window.clearTimeout(saveTimer.current)
      quarterRef.current = nextQuarter
      void loadRemote(nextQuarter)
    },
    selectPlan: (id) => {
      if (id === planRef.current?.id) return
      if (syncState.kind === 'saving') {
        setSyncState({ kind: 'error', message: '请等待当前 Biz OKR Plan 保存后再切换。' })
        return
      }
      window.clearTimeout(saveTimer.current)
      const target = plans.find((item) => item.id === id)
      if (target) {
        quarterRef.current = target.quarter
        setQuarterState(target.quarter)
      }
      setSyncState({ kind: 'loading', message: '正在读取 Biz OKR Plan…' })
      getOKRPlan(id).then((loaded) => {
        publishPlan(loaded)
        dirtyObjectives.current.clear()
        deletedObjectives.current.clear()
        objectiveRevisions.current.clear()
        objectiveOrderDirty.current = false
        objectiveOrderRevision.current = 0
        setSyncState({ kind: 'ready', message: 'Biz OKR Plan 已加载' })
      }).catch((error) => setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '读取 Biz OKR Plan 失败。' }))
    },
    createPlan: async (input) => {
      setSyncState({ kind: 'saving', message: '正在新建 Biz OKR Plan…' })
      try {
        const created = await createOKRPlan({ quarter: input.quarter, title: input.title })
        quarterRef.current = created.quarter
        setQuarterState(created.quarter)
        const list = await listOKRPlans(created.quarter)
        setAvailableQuarters(list.availableQuarters)
        setPlans(list.plans)
        publishPlan(created)
        dirtyObjectives.current.clear()
        deletedObjectives.current.clear()
        objectiveRevisions.current.clear()
        objectiveOrderDirty.current = false
        objectiveOrderRevision.current = 0
        setSyncState({ kind: 'saved', message: 'Biz OKR Plan 已新建' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '新建 Biz OKR Plan 失败。' })
        throw error
      }
    },
    deleteCurrentPlan: async () => {
      if (!planRef.current) throw new Error('当前没有可删除的 Biz OKR Plan。')
      const removed = planRef.current
      setSyncState({ kind: 'saving', message: '正在删除 Biz OKR Plan…' })
      try {
        await deleteOKRPlan(removed.id)
        const loaded = await loadRemote(removed.quarter)
        if (!loaded?.plan) setSyncState({ kind: 'saved', message: 'Biz OKR Plan 已删除，当前季度暂无 Biz OKR Plan' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '删除 Biz OKR Plan 失败。' })
        throw error
      }
    },
    syncState,
    enums,
    templateKey: 'classic',
    week: '',
    previousWeek: undefined,
    availableWeeks: [],
    setWeek: () => undefined,
    setWeeklyScope: () => false,
    deleteWeeklyScope: async () => { throw new Error('Biz OKR Plan 没有周次。') },
    setKrTitle: (_objId, krId, title) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.title = title
    }),
    setKrOwner: (krId, ownerName, ownerOpenId, owners) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (!kr) return
      kr.ownerName = ownerName
      kr.ownerOpenId = ownerOpenId ?? ''
      kr.owners = owners ?? ownerName.split(/[、,，;；]/).map((name) => ({ name: name.trim(), openId: '' })).filter((owner) => owner.name)
    }),
    setKrBusinessCategory: (krId, category) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.tags = replaceSingleTag(kr.tags, BUSINESS_CATEGORY_TAG, category)
    }),
    setKrPriority: (krId, priority) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.tags = replaceSingleTag(kr.tags, PRIORITY_TAG, priority)
    }),
    createObjective: async (input) => {
      mutate((draft) => draft.push({ id: uid('plan-o'), title: input.title.trim(), krs: [] }))
    },
    updateObjective: async (id, title) => {
      mutate((draft) => {
        const objective = draft.find((item) => item.id === id)
        if (objective) objective.title = title.trim()
      })
    },
    deleteObjective: async (id) => {
      mutate((draft) => {
        const index = draft.findIndex((item) => item.id === id && item.krs.length === 0)
        if (index >= 0) draft.splice(index, 1)
      })
    },
    swapObjectives: async (id, targetId) => {
      const current = objectivesRef.current
      if (!planRef.current) return
      await queueObjectiveOrder(swappedOrder(current.map((item) => item.id), id, targetId))
    },
    reorderObjectives: queueObjectiveOrder,
    swapKrs: async (objectiveId, krId, targetId) => {
      mutate((draft) => {
        const objective = draft.find((item) => item.id === objectiveId)
        if (!objective) return
        const from = objective.krs.findIndex((item) => item.id === krId)
        const to = objective.krs.findIndex((item) => item.id === targetId)
        if (from < 0 || to < 0) return
        const [item] = objective.krs.splice(from, 1)
        objective.krs.splice(to, 0, item)
      })
    },
    createKr: async (objectiveId, input) => {
      mutate((draft) => {
        const objective = draft.find((item) => item.id === objectiveId)
        if (!objective) return
        const owners = input.owners ?? []
        objective.krs.push({
          id: uid('plan-kr'),
          title: input.title,
          owners,
          ownerName: owners.map((owner) => owner.name).join('、'),
          ownerOpenId: owners[0]?.openId ?? '',
          metricNote: '',
          metrics: [],
          points: [],
          tags: [
            { type: BUSINESS_CATEGORY_TAG, value: input.businessCategory },
            { type: PRIORITY_TAG, value: input.priority },
          ],
        })
      })
    },
    deleteKr: async (krId) => {
      mutate((draft) => {
        for (const objective of draft) objective.krs = objective.krs.filter((item) => item.id !== krId)
      })
    },
    addTag: (krId, value, type = 'custom') => mutate((draft) => {
      const clean = value.trim()
      const kr = findKr(draft, krId)
      if (!kr || !clean) return
      kr.tags ??= []
      if (!kr.tags.some((tag) => tag.type === type && tag.value === clean)) kr.tags.push({ type, value: clean })
    }),
    removeTag: (krId, type, value) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.tags = (kr.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value)
    }),
    addPointTag: (krId, pointId, value, type = 'custom') => mutate((draft) => {
      const clean = value.trim()
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (!point || !clean) return
      point.tags ??= []
      if (!point.tags.some((tag) => tag.type === type && tag.value === clean)) point.tags.push({ type, value: clean })
    }),
    removePointTag: (krId, pointId, type, value) => mutate((draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) point.tags = (point.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value)
    }),
    setPointOwners: (krId, pointId, owners) => mutate((draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) point.owners = owners
    }),
    setMetricNote: (krId, note) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.metricNote = note
    }),
    patchMetric: (krId, metricId, patch) => mutate((draft) => {
      const metric = findKr(draft, krId)?.metrics.find((item) => item.id === metricId)
      if (metric) Object.assign(metric, patch)
    }),
    addMetric: (krId, initial) => {
      const id = uid('plan-m')
      mutate((draft) => {
        findKr(draft, krId)?.metrics.push({ text: '', light: 'green', images: [], ...initial, id })
      })
      return id
    },
    removeMetric: (krId, metricId) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.metrics = kr.metrics.filter((item) => item.id !== metricId)
    }),
    setPointTitle: (_objId, krId, pointId, title) => mutate((draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) point.title = title
    }),
    setPointMeegoLink: (krId, pointId, patch) => mutate((draft) => {
      const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
      if (point) Object.assign(point, patch)
    }),
    addPoint: (_objId, krId, kind) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (!kr) return
      const point: Point = { id: uid('plan-p'), kind, title: '', tags: [], owners: [], entries: [], previousEntries: [] }
      const lastSameKind = kr.points.map((item) => item.kind).lastIndexOf(kind)
      if (lastSameKind === -1) kr.points.push(point)
      else kr.points.splice(lastSameKind + 1, 0, point)
    }),
    setPointKind: (krId, pointId, kind) => mutate((draft) => {
      const kr = findKr(draft, krId)
      const point = kr?.points.find((item) => item.id === pointId)
      if (!kr || !point || point.kind === kind) return
      point.kind = kind
      const others = kr.points.filter((item) => item.id !== pointId)
      const lastSameKind = others.map((item) => item.kind).lastIndexOf(kind)
      kr.points = lastSameKind === -1 ? [...others, point] : [...others.slice(0, lastSameKind + 1), point, ...others.slice(lastSameKind + 1)]
    }),
    removePoint: (_objId, krId, pointId) => mutate((draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.points = kr.points.filter((item) => item.id !== pointId)
    }),
    patchEntry: () => undefined,
    addEntry: () => undefined,
    removeEntry: () => undefined,
    setKrScore: async () => undefined,
    setPointScore: async () => undefined,
    reset: () => void loadRemote(),
    retry: () => {
      if (remoteReady.current && (dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)) void saveNow()
      else void loadRemote()
    },
    resolveConflict: () => undefined,
    applySavedKr: (kr) => mutate((draft) => {
      for (const objective of draft) {
        const index = objective.krs.findIndex((item) => item.id === kr.id)
        if (index >= 0) objective.krs[index] = kr
      }
    }),
  }), [availableQuarters, enums, loadRemote, mutate, objectives, plan, plans, publishPlan, quarter, queueObjectiveOrder, saveNow, syncState])

  return (
    <PlanBoardContext.Provider value={api}>
      <BoardContext.Provider value={api}>{children}</BoardContext.Provider>
    </PlanBoardContext.Provider>
  )
}
