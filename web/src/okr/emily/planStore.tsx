import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { APIError, createOKRPlan, deleteOKRPlan, getEnums, getOKRPlan, listOKRPlans, replaceOKRPlan } from './api'
import { BoardContext, uid, type BoardApi, type SyncState } from './board'
import { BUSINESS_CATEGORY_TAG, PRIORITY_TAG, replaceSingleTag } from './hierarchy'
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

export function PlanBoardProvider({ children, initialQuarter = '', onQuarterChange }: { children: ReactNode; initialQuarter?: string; onQuarterChange?: (quarter: string) => void }) {
  const [quarter, setQuarterState] = useState(initialQuarter || currentQuarter())
  const [availableQuarters, setAvailableQuarters] = useState<string[]>(initialQuarter ? [initialQuarter] : [currentQuarter()])
  const [plans, setPlans] = useState<OKRPlanSummary[]>([])
  const [plan, setPlan] = useState<OKRPlan>()
  const [objectives, setObjectives] = useState<Objective[]>([])
  const [enums, setEnums] = useState<EnumValues>(DEFAULT_ENUMS)
  const [syncState, setSyncState] = useState<SyncState>({ kind: 'loading', message: '正在读取 OKR Plan…' })
  const quarterRef = useRef(quarter)
  const planRef = useRef<OKRPlan | undefined>(undefined)
  const objectivesRef = useRef<Objective[]>([])
  const saveTimer = useRef(0)
  const remoteReady = useRef(false)

  const publishPlan = useCallback((next?: OKRPlan) => {
    planRef.current = next
    setPlan(next)
    const nextObjectives = clone(next?.content.objectives ?? [])
    objectivesRef.current = nextObjectives
    setObjectives(nextObjectives)
  }, [])

  const loadRemote = useCallback(async (targetQuarter?: string, targetPlanId?: string) => {
    remoteReady.current = false
    setSyncState({ kind: 'loading', message: '正在读取 OKR Plan…' })
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
      remoteReady.current = true
      setSyncState({ kind: 'ready', message: loadedPlan ? 'OKR Plan 已加载' : '当前季度暂无 Plan' })
      return { list, plan: loadedPlan }
    } catch (error) {
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '加载 OKR Plan 失败。' })
    }
  }, [onQuarterChange, publishPlan])

  useEffect(() => {
    const timer = window.setTimeout(() => void loadRemote(), 0)
    return () => {
      window.clearTimeout(timer)
      window.clearTimeout(saveTimer.current)
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

  const saveNow = useCallback(async () => {
    if (!remoteReady.current || !planRef.current) return
    const snapshot: OKRPlan = { ...planRef.current, content: { objectives: clone(objectivesRef.current) } }
    setSyncState({ kind: 'saving', message: '正在保存 Plan…' })
    try {
      const saved = await replaceOKRPlan({ id: snapshot.id, expectedVersion: snapshot.version, title: snapshot.title, content: snapshot.content })
      publishPlan(saved)
      setPlans((items) => items.map((item) => item.id === saved.id ? {
        id: saved.id,
        quarter: saved.quarter,
        title: saved.title,
        version: saved.version,
        objectiveCount: saved.content.objectives.length,
        krCount: saved.content.objectives.reduce((total, objective) => total + objective.krs.length, 0),
        updatedAt: saved.updatedAt,
      } : item))
      setSyncState({ kind: 'saved', message: 'Plan 已保存' })
    } catch (error) {
      if (error instanceof APIError && error.status === 409 && error.data) {
        const current = error.data as OKRPlan
        publishPlan(current)
        setSyncState({ kind: 'error', message: '这个 Plan 刚被其他人更新，已载入最新版本。' })
        return
      }
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '保存 Plan 失败。' })
    }
  }, [publishPlan])

  const scheduleSave = useCallback(() => {
    window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => void saveNow(), SAVE_DELAY_MS)
  }, [saveNow])

  const mutate = useCallback((fn: (draft: Objective[]) => void) => {
    if (!planRef.current) return
    const draft = clone(objectivesRef.current)
    fn(draft)
    objectivesRef.current = draft
    setObjectives(draft)
    scheduleSave()
  }, [scheduleSave])

  const api = useMemo<PlanBoardApi>(() => ({
    objectives,
    plan,
    plans,
    quarter,
    availableQuarters,
    setQuarter: (nextQuarter) => {
      if (nextQuarter === quarterRef.current) return
      if (syncState.kind === 'saving') {
        setSyncState({ kind: 'error', message: '请等待当前 Plan 保存后再切换季度。' })
        return
      }
      window.clearTimeout(saveTimer.current)
      quarterRef.current = nextQuarter
      void loadRemote(nextQuarter)
    },
    selectPlan: (id) => {
      if (id === planRef.current?.id) return
      if (syncState.kind === 'saving') {
        setSyncState({ kind: 'error', message: '请等待当前 Plan 保存后再切换。' })
        return
      }
      window.clearTimeout(saveTimer.current)
      const target = plans.find((item) => item.id === id)
      if (target) {
        quarterRef.current = target.quarter
        setQuarterState(target.quarter)
      }
      setSyncState({ kind: 'loading', message: '正在读取 OKR Plan…' })
      getOKRPlan(id).then((loaded) => {
        publishPlan(loaded)
        setSyncState({ kind: 'ready', message: 'OKR Plan 已加载' })
      }).catch((error) => setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '读取 OKR Plan 失败。' }))
    },
    createPlan: async (input) => {
      setSyncState({ kind: 'saving', message: '正在新建 Plan…' })
      try {
        const created = await createOKRPlan({ quarter: input.quarter, title: input.title, content: { objectives: [] } })
        quarterRef.current = created.quarter
        setQuarterState(created.quarter)
        const list = await listOKRPlans(created.quarter)
        setAvailableQuarters(list.availableQuarters)
        setPlans(list.plans)
        publishPlan(created)
        setSyncState({ kind: 'saved', message: 'Plan 已新建' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '新建 Plan 失败。' })
        throw error
      }
    },
    deleteCurrentPlan: async () => {
      if (!planRef.current) throw new Error('当前没有可删除的 Plan。')
      const removed = planRef.current
      setSyncState({ kind: 'saving', message: '正在删除 Plan…' })
      try {
        await deleteOKRPlan(removed.id)
        const loaded = await loadRemote(removed.quarter)
        if (!loaded?.plan) setSyncState({ kind: 'saved', message: 'Plan 已删除，当前季度暂无 Plan' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '删除 Plan 失败。' })
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
    deleteWeeklyScope: async () => { throw new Error('OKR Plan 没有周次。') },
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
      mutate((draft) => {
        const from = draft.findIndex((item) => item.id === id)
        const to = draft.findIndex((item) => item.id === targetId)
        if (from < 0 || to < 0) return
        const [item] = draft.splice(from, 1)
        draft.splice(to, 0, item)
      })
    },
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
    retry: () => void loadRemote(),
    resolveConflict: () => undefined,
    applySavedKr: (kr) => mutate((draft) => {
      for (const objective of draft) {
        const index = objective.krs.findIndex((item) => item.id === kr.id)
        if (index >= 0) objective.krs[index] = kr
      }
    }),
  }), [availableQuarters, enums, loadRemote, mutate, objectives, plan, plans, publishPlan, quarter, saveNow, syncState])

  return (
    <PlanBoardContext.Provider value={api}>
      <BoardContext.Provider value={api}>{children}</BoardContext.Provider>
    </PlanBoardContext.Provider>
  )
}
