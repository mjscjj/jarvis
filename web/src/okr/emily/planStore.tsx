import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { APIError, createOKRPlan, createOKRPlanObjective, deleteOKRPlan, deleteOKRPlanObjective, getEnums, getOKRPlan, listOKRPlans, patchPointDefinition, reorderOKRPlanObjectives, updateOKRPlanKR, updateOKRPlanObjective, type PointDefinitionPatchResult } from './api'
import { BoardContext, uid, type BoardApi, type SyncState } from './board'
import { BUSINESS_CATEGORY_TAG, PRIORITY_TAG, replaceSingleTag } from './hierarchy'
import { swappedOrder, swappedPointsWithinKind } from './ordering'
import { adoptRemoteVersionsForOverwrite, rebasePendingChanges } from './concurrency'
import type { EnumValues, Kr, KrOwner, KrPriority, MetricLine, Objective, OKRPlan, OKRPlanSummary, Point, WeekTemplateKey } from './types'
import { LIGHTS, STATUSES } from './template'
import { krConflictLocation } from './saveNotice'

const SAVE_DELAY_MS = 700

type PendingPointPatch = {
	title?: string
	owners?: KrOwner[]
	kind?: Point['kind']
	meegoWorkItemId?: string
	meegoUrl?: string
	tags?: Point['tags']
}

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

function findKrByPoint(draft: Objective[], pointId: string): Kr | undefined {
  return draft.flatMap((objective) => objective.krs).find((kr) => kr.points.some((point) => point.id === pointId))
}

function withPointDefinition(kr: Kr, pointId: string, saved: PointDefinitionPatchResult): Kr {
  const next = clone(kr)
  const point = next.points.find((item) => item.id === pointId)
  if (point) Object.assign(point, {
    version: saved.version,
    title: saved.title,
    kind: saved.kind,
    meegoWorkItemId: saved.meegoWorkItemId,
    meegoUrl: saved.meegoUrl,
    owners: saved.owners,
    tags: saved.tags,
  })
  return next
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
	const dirtyKRs = useRef(new Set<string>())
	const krRevisions = useRef(new Map<string, number>())
  const objectiveOrderDirty = useRef(false)
  const objectiveOrderRevision = useRef(0)
  const saveInFlight = useRef(false)
  const saveAgain = useRef(false)
  const scheduleSaveRef = useRef<() => void>(() => undefined)
  const pointTimers = useRef(new Map<string, number>())
  const pointPatches = useRef(new Map<string, PendingPointPatch>())
  const pointRevisions = useRef(new Map<string, number>())
  const pointSavesInFlight = useRef(new Set<string>())
  const schedulePointSaveRef = useRef<(pointId: string) => void>(() => undefined)
  const remoteReady = useRef(false)
  const persistedObjectiveIDs = useRef(new Set<string>())
  const syncStateRef = useRef(syncState)
  const focusRefreshPending = useRef(false)
  const focusRefreshInFlight = useRef(false)
  const loadRequest = useRef(0)
  syncStateRef.current = syncState

  const hasLocalWork = useCallback(() => (
    saveInFlight.current || dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 ||
    objectiveOrderDirty.current || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 ||
    syncStateRef.current.kind === 'saving' || syncStateRef.current.kind === 'conflict'
  ), [])

  const publishPlan = useCallback((next?: OKRPlan) => {
    planRef.current = next
    setPlan(next)
    const nextObjectives = clone(next?.objectives ?? [])
    objectivesRef.current = nextObjectives
    setObjectives(nextObjectives)
  }, [])

  const loadRemote = useCallback(async (targetQuarter?: string, targetPlanId?: string, options?: { background?: boolean }) => {
    const background = options?.background === true
    if (background && (!remoteReady.current || hasLocalWork())) return
    const request = ++loadRequest.current
    const startingPlanID = planRef.current?.id
    const startingObjectives = background ? JSON.stringify(objectivesRef.current) : ''
    if (!background) {
      remoteReady.current = false
      setSyncState({ kind: 'loading', message: '正在读取 Biz OKR Plan…' })
    }
    try {
      if (!background) {
        for (const pointTimer of pointTimers.current.values()) window.clearTimeout(pointTimer)
        pointTimers.current.clear()
      }
      const list = await listOKRPlans(targetQuarter ?? quarterRef.current)
      const remoteEnums = await getEnums()
      const selected = targetPlanId ? list.plans.find((item) => item.id === targetPlanId) : list.plans[0]
      const loadedPlan = selected ? await getOKRPlan(selected.id) : undefined
      if (request !== loadRequest.current) return
      if (background && (hasLocalWork() || planRef.current?.id !== startingPlanID || JSON.stringify(objectivesRef.current) !== startingObjectives)) {
        focusRefreshPending.current = true
        return
      }
      quarterRef.current = list.quarter
      setQuarterState(list.quarter)
      onQuarterChange?.(list.quarter)
      setAvailableQuarters(list.availableQuarters)
      setPlans(list.plans)
      setEnums(remoteEnums)
      publishPlan(loadedPlan)
      persistedObjectiveIDs.current = new Set(loadedPlan?.objectives.map((objective) => objective.id) ?? [])
      dirtyObjectives.current.clear()
      deletedObjectives.current.clear()
      objectiveRevisions.current.clear()
		dirtyKRs.current.clear()
		krRevisions.current.clear()
      objectiveOrderDirty.current = false
      objectiveOrderRevision.current = 0
      pointPatches.current.clear()
      pointRevisions.current.clear()
      remoteReady.current = true
      setSyncState({ kind: 'ready', message: loadedPlan ? 'Biz OKR Plan 已加载' : '当前季度暂无 Biz OKR Plan' })
      return { list, plan: loadedPlan }
    } catch (error) {
      if (!background && request === loadRequest.current) setSyncState({ kind: 'error', title: '读取失败', message: error instanceof Error ? error.message : '加载 Biz OKR Plan 失败。', logid: error instanceof APIError ? error.logid : undefined })
    }
  }, [hasLocalWork, onQuarterChange, publishPlan])

  useEffect(() => {
    const timer = window.setTimeout(() => void loadRemote(initialQuarter, initialPlanId), 0)
    return () => {
      window.clearTimeout(timer)
      window.clearTimeout(saveTimer.current)
      for (const pointTimer of pointTimers.current.values()) window.clearTimeout(pointTimer)
      dirtyObjectives.current.clear()
      deletedObjectives.current.clear()
      objectiveRevisions.current.clear()
		dirtyKRs.current.clear()
		krRevisions.current.clear()
      objectiveOrderDirty.current = false
      objectiveOrderRevision.current = 0
      pointTimers.current.clear()
      pointPatches.current.clear()
      pointRevisions.current.clear()
    }
  }, [loadRemote])

  useEffect(() => {
    const warnBeforeUnload = (event: BeforeUnloadEvent) => {
      if (dirtyObjectives.current.size === 0 && dirtyKRs.current.size === 0 && !objectiveOrderDirty.current && pointPatches.current.size === 0 && pointSavesInFlight.current.size === 0) return
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', warnBeforeUnload)
    return () => window.removeEventListener('beforeunload', warnBeforeUnload)
  }, [])

  const refreshOnFocus = useCallback(async () => {
    if (document.visibilityState !== 'visible') return
    if (focusRefreshInFlight.current) return
    focusRefreshPending.current = true
    if (!remoteReady.current || hasLocalWork()) return
    focusRefreshPending.current = false
    focusRefreshInFlight.current = true
    try {
      await loadRemote(quarterRef.current, planRef.current?.id, { background: true })
    } finally {
      focusRefreshInFlight.current = false
      if (focusRefreshPending.current && !hasLocalWork()) void refreshOnFocus()
    }
  }, [hasLocalWork, loadRemote])

  useEffect(() => {
    const refresh = () => { void refreshOnFocus() }
    window.addEventListener('focus', refresh)
    document.addEventListener('visibilitychange', refresh)
    return () => {
      window.removeEventListener('focus', refresh)
      document.removeEventListener('visibilitychange', refresh)
    }
  }, [refreshOnFocus])

  useEffect(() => {
    if (focusRefreshPending.current && !hasLocalWork()) void refreshOnFocus()
  }, [hasLocalWork, refreshOnFocus, syncState.kind])

  useEffect(() => {
    const nextQuarter = initialQuarter.trim()
    if (!nextQuarter || nextQuarter === quarterRef.current) return
    if (syncState.kind === 'saving' || dirtyObjectives.current.size > 0 || dirtyKRs.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0) {
      setSyncState({ kind: 'error', message: '请等待当前 Plan 保存后再切换季度。' })
      return
    }
    window.clearTimeout(saveTimer.current)
    quarterRef.current = nextQuarter
    void loadRemote(nextQuarter)
  }, [initialQuarter, loadRemote, syncState.kind])

  const mergeSavedPlan = useCallback((saved: OKRPlan, objectiveId?: string, submitted?: Objective) => {
    const current = planRef.current
    if (!current) return
    const nextObjective = objectiveId ? saved.objectives.find((item) => item.id === objectiveId) : undefined
    const nextObjectives = objectiveId && nextObjective
			? objectivesRef.current.map((item) => item.id === objectiveId
        ? submitted ? rebasePendingChanges(submitted, item, nextObjective) : nextObjective
        : item)
      : objectivesRef.current
    const nextPlan = { ...current, version: saved.version, deleteToken: saved.deleteToken, updatedBy: saved.updatedBy, updatedAt: saved.updatedAt, objectives: nextObjectives }
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

	const mergeSavedKR = useCallback((saved: OKRPlan, krId: string, submitted: Kr) => {
		const current = planRef.current
		if (!current) return
		let remoteObjective: Objective | undefined
		let remoteKR: Kr | undefined
		for (const objective of saved.objectives) {
			const candidate = objective.krs.find((kr) => kr.id === krId)
			if (candidate) {
				remoteObjective = objective
				remoteKR = candidate
				break
			}
		}
		if (!remoteObjective || !remoteKR) return
		const nextObjectives = objectivesRef.current.map((objective) => objective.id !== remoteObjective!.id ? objective : {
			...objective,
			structureToken: remoteObjective!.structureToken,
			krs: objective.krs.map((kr) => kr.id === krId ? rebasePendingChanges(submitted, kr, remoteKR!) : kr),
		})
		const nextPlan = { ...current, version: saved.version, deleteToken: saved.deleteToken, updatedBy: saved.updatedBy, updatedAt: saved.updatedAt, objectives: nextObjectives }
		planRef.current = nextPlan
		setPlan(nextPlan)
		objectivesRef.current = nextObjectives
		setObjectives(nextObjectives)
		setPlans((items) => items.map((item) => item.id === saved.id ? { ...item, version: saved.version, updatedAt: saved.updatedAt } : item))
	}, [])

  const mergeSavedObjectiveOrder = useCallback((saved: OKRPlan) => {
    const currentByID = new Map(objectivesRef.current.map((objective) => [objective.id, objective]))
    const nextObjectives = saved.objectives.map((remoteObjective) => {
      const current = currentByID.get(remoteObjective.id)
      return current ?? remoteObjective
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

  const savePointNow = useCallback(async (pointId: string) => {
    const currentPlan = planRef.current
    const patch = pointPatches.current.get(pointId)
    if (!remoteReady.current || !currentPlan || !patch || pointSavesInFlight.current.has(pointId)) return
    const revision = pointRevisions.current.get(pointId) ?? 0
    let failed = false
    pointSavesInFlight.current.add(pointId)
    setSyncState({ kind: 'saving', message: '正在保存具体 KR…' })
    try {
      const currentPoint = findPoint(objectivesRef.current, pointId)
			if (!currentPoint) {
				pointPatches.current.delete(pointId)
				pointRevisions.current.delete(pointId)
				return
			}
			const saved = await patchPointDefinition({ pointId, planId: currentPlan.id, expectedVersion: currentPoint.version ?? 0, ...patch })
      const nextObjectives = objectivesRef.current.map((objective) => ({
        ...objective,
		structureToken: objective.krs.some((kr) => kr.points.some((point) => point.id === pointId))
			? saved.structureToken ?? objective.structureToken
			: objective.structureToken,
        krs: objective.krs.map((kr) => ({
          ...kr,
			structureToken: kr.points.some((point) => point.id === pointId)
				? saved.krStructureToken ?? kr.structureToken
				: kr.structureToken,
          points: kr.points.map((point) => point.id === pointId ? { ...point, version: saved.version } : point),
        })),
      }))
      objectivesRef.current = nextObjectives
      setObjectives(nextObjectives)
			if (planRef.current) {
				const nextPlan = { ...planRef.current, deleteToken: saved.planDeleteToken ?? planRef.current.deleteToken, objectives: nextObjectives }
				planRef.current = nextPlan
				setPlan(nextPlan)
			}
      if ((pointRevisions.current.get(pointId) ?? 0) === revision) {
        pointPatches.current.delete(pointId)
        pointRevisions.current.delete(pointId)
      }
      if (pointPatches.current.size === 0 && dirtyKRs.current.size === 0 && dirtyObjectives.current.size === 0 && !objectiveOrderDirty.current) {
        setSyncState({ kind: 'saved', message: 'Plan 已保存' })
      }
    } catch (error) {
      failed = true
			if (error instanceof APIError && error.status === 409 && error.data) {
				const remote = error.data as PointDefinitionPatchResult
				const current = findKrByPoint(objectivesRef.current, pointId)
				if (current) {
					setSyncState({
						kind: 'conflict',
						message: '这个具体 KR 刚被其他人更新，请选择保留哪一版。',
						krId: current.id,
						pointId,
						location: krConflictLocation(objectivesRef.current, current.id, pointId),
						local: clone(current),
						remote: withPointDefinition(current, pointId, remote),
					})
				} else setSyncState({ kind: 'error', message: '具体 KR 已不存在，请重新载入后再编辑。', logid: error.logid })
			} else {
				setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '具体 KR 保存失败，请重试。', logid: error instanceof APIError ? error.logid : undefined })
			}
    } finally {
      pointSavesInFlight.current.delete(pointId)
      if (!failed && pointPatches.current.has(pointId)) schedulePointSaveRef.current(pointId)
      if (!failed && (dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)) scheduleSaveRef.current()
    }
  }, [])

  const schedulePointSave = useCallback((pointId: string) => {
    const previous = pointTimers.current.get(pointId)
    if (previous) window.clearTimeout(previous)
    const timer = window.setTimeout(() => {
      pointTimers.current.delete(pointId)
      void savePointNow(pointId)
    }, SAVE_DELAY_MS)
    pointTimers.current.set(pointId, timer)
  }, [savePointNow])

  useEffect(() => {
    schedulePointSaveRef.current = schedulePointSave
  }, [schedulePointSave])

  const saveNow = useCallback(async () => {
    if (!remoteReady.current || !planRef.current) return
    if (pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0) {
      saveAgain.current = true
      return
    }
    if (saveInFlight.current) {
      saveAgain.current = true
      return
    }
    saveInFlight.current = true
    let failed = false
    let reordered = false
    setSyncState({ kind: 'saving', message: dirtyKRs.current.size > 0 ? '正在保存 KR…' : dirtyObjectives.current.size === 0 && objectiveOrderDirty.current ? '正在调整顺序…' : '正在保存 Plan…' })
    try {
      do {
        saveAgain.current = false
		const pendingKRs = Array.from(dirtyKRs.current)
		for (const krId of pendingKRs) {
			const kr = findKr(objectivesRef.current, krId)
			if (!kr || kr.version === undefined) {
				dirtyKRs.current.delete(krId)
				krRevisions.current.delete(krId)
				continue
			}
			const revision = krRevisions.current.get(krId) ?? 0
			const submitted = clone(kr)
			try {
				const saved = await updateOKRPlanKR(planRef.current.id, submitted)
				mergeSavedKR(saved, krId, submitted)
				if ((krRevisions.current.get(krId) ?? 0) === revision) {
					dirtyKRs.current.delete(krId)
					krRevisions.current.delete(krId)
				}
			} catch (error) {
				if (error instanceof APIError && error.status === 409 && error.data) {
					const remotePlan = error.data as OKRPlan
					const remoteObjective = remotePlan.objectives.find((objective) => objective.krs.some((item) => item.id === krId))
					const remoteKR = remoteObjective?.krs.find((item) => item.id === krId)
					if (remoteObjective && remoteKR) {
						const nextObjectives = objectivesRef.current.map((objective) => objective.id === remoteObjective.id
							? { ...objective, structureToken: remoteObjective.structureToken }
							: objective)
						objectivesRef.current = nextObjectives
						setObjectives(nextObjectives)
						if (planRef.current) {
							const nextPlan = { ...planRef.current, version: remotePlan.version, deleteToken: remotePlan.deleteToken, updatedBy: remotePlan.updatedBy, updatedAt: remotePlan.updatedAt, objectives: nextObjectives }
							planRef.current = nextPlan
							setPlan(nextPlan)
						}
						failed = true
						setSyncState({
							kind: 'conflict',
							message: '这条 KR 刚被其他人更新，请选择保留哪一版。',
							krId,
							location: krConflictLocation(objectivesRef.current, krId),
							local: submitted,
							remote: remoteKR,
						})
						return
					}
				}
				throw error
			}
		}
        const pendingObjectives = Array.from(dirtyObjectives.current)
        for (const objectiveId of pendingObjectives) {
          const objective = objectivesRef.current.find((item) => item.id === objectiveId)
          const deleted = deletedObjectives.current.get(objectiveId)
          const revision = objectiveRevisions.current.get(objectiveId) ?? 0
          try {
            if (objective) {
              const saved = !persistedObjectiveIDs.current.has(objectiveId)
                ? await createOKRPlanObjective(planRef.current.id, objective)
                : await updateOKRPlanObjective(planRef.current.id, objective)
              persistedObjectiveIDs.current.add(objectiveId)
              mergeSavedPlan(saved, objectiveId, objective)
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
              persistedObjectiveIDs.current.delete(objectiveId)
              mergeSavedPlan({ ...planRef.current, version: planRef.current.version + 1, updatedAt: new Date().toISOString() })
            } else {
              dirtyObjectives.current.delete(objectiveId)
              objectiveRevisions.current.delete(objectiveId)
            }
          } catch (error) {
            if (error instanceof APIError && error.status === 409 && error.data) {
              const title = objective?.title || deleted?.title || objectiveId
              throw new Error(`${title} 已被其他人更新；你的本地内容仍保留，请先复制再刷新重试。`)
            }
            throw error
          }
        }
        if (objectiveOrderDirty.current && planRef.current) {
          const revision = objectiveOrderRevision.current
          let saved: OKRPlan
          try {
            saved = await reorderOKRPlanObjectives(planRef.current.id, objectivesRef.current.map((objective) => objective.id), planRef.current.version)
          } catch (reorderError) {
            try {
              const remote = await getOKRPlan(planRef.current.id)
              if (objectiveOrderRevision.current === revision && dirtyKRs.current.size === 0 && dirtyObjectives.current.size === 0) {
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
      } while (saveAgain.current || dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)
      setSyncState({ kind: 'saved', message: reordered ? '顺序已保存' : 'Plan 已保存' })
    } catch (error) {
      failed = true
      setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '保存 Plan 失败。', logid: error instanceof APIError ? error.logid : undefined })
    } finally {
      saveInFlight.current = false
      if (!failed && (dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)) scheduleSaveRef.current()
    }
  }, [mergeSavedKR, mergeSavedObjectiveOrder, mergeSavedPlan, publishPlan])

  const scheduleSave = useCallback(() => {
    window.clearTimeout(saveTimer.current)
    saveTimer.current = window.setTimeout(() => void saveNow(), SAVE_DELAY_MS)
  }, [saveNow])

  useEffect(() => {
    scheduleSaveRef.current = scheduleSave
  }, [scheduleSave])

  const resolveConflict = useCallback((choice: 'remote' | 'local') => {
    if (syncState.kind !== 'conflict') return
    const selected = choice === 'remote' ? syncState.remote : adoptRemoteVersionsForOverwrite(syncState.local, syncState.remote)
    const nextObjectives = objectivesRef.current.map((objective) => objective.krs.some((kr) => kr.id === syncState.krId)
      ? { ...objective, krs: objective.krs.map((kr) => kr.id === syncState.krId ? selected : kr) }
      : objective)
    objectivesRef.current = nextObjectives
    setObjectives(nextObjectives)
    if (planRef.current) {
      const nextPlan = { ...planRef.current, objectives: nextObjectives }
      planRef.current = nextPlan
      setPlan(nextPlan)
    }
    if (choice === 'remote') {
      if (syncState.pointId) {
        pointPatches.current.delete(syncState.pointId)
        pointRevisions.current.delete(syncState.pointId)
      } else {
        dirtyKRs.current.delete(syncState.krId)
        krRevisions.current.delete(syncState.krId)
      }
      setSyncState({ kind: 'saved', message: '已使用服务器版本' })
      if (pointPatches.current.size > 0) {
        for (const pointId of pointPatches.current.keys()) schedulePointSave(pointId)
      } else if (dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current) scheduleSave()
      return
    }
    setSyncState({ kind: 'saving', message: '正在保存你的修改…' })
    if (syncState.pointId) {
      pointRevisions.current.set(syncState.pointId, (pointRevisions.current.get(syncState.pointId) ?? 0) + 1)
      schedulePointSave(syncState.pointId)
      return
    }
    dirtyKRs.current.add(syncState.krId)
    krRevisions.current.set(syncState.krId, (krRevisions.current.get(syncState.krId) ?? 0) + 1)
    scheduleSave()
  }, [schedulePointSave, scheduleSave, syncState])

  const mutateObjective = useCallback((fn: (draft: Objective[]) => void) => {
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
    setSyncState({ kind: 'ready', message: '有修改待保存…' })
    scheduleSave()
  }, [scheduleSave])

	const mutateKR = useCallback((krId: string, fn: (kr: Kr) => void) => {
		if (!planRef.current) return
		const draft = clone(objectivesRef.current)
		const kr = findKr(draft, krId)
		if (!kr) return
		const before = JSON.stringify(kr)
		fn(kr)
		if (JSON.stringify(kr) === before) return
		objectivesRef.current = draft
		setObjectives(draft)
		if (kr.version !== undefined) {
			dirtyKRs.current.add(krId)
			krRevisions.current.set(krId, (krRevisions.current.get(krId) ?? 0) + 1)
		} else {
			const objective = draft.find((item) => item.krs.some((item) => item.id === krId))
			if (objective) {
				dirtyObjectives.current.add(objective.id)
				objectiveRevisions.current.set(objective.id, (objectiveRevisions.current.get(objective.id) ?? 0) + 1)
			}
		}
		setSyncState({ kind: 'ready', message: '有修改待保存…' })
		scheduleSave()
	}, [scheduleSave])

	const mutatePointDefinition = useCallback((pointId: string, patch: PendingPointPatch) => {
		const draft = clone(objectivesRef.current)
		const point = findPoint(draft, pointId)
		if (!point) return
		const persisted = point.version !== undefined
		Object.assign(point, patch)
    objectivesRef.current = draft
    setObjectives(draft)
		// New points are created by the already queued KR save.
		// Existing points never fall back to that whole-object write path.
		if (!persisted) {
			const kr = draft.flatMap((objective) => objective.krs).find((item) => item.points.some((item) => item.id === pointId))
			if (kr) {
				if (kr.version !== undefined) {
					dirtyKRs.current.add(kr.id)
					krRevisions.current.set(kr.id, (krRevisions.current.get(kr.id) ?? 0) + 1)
				} else {
					const objective = draft.find((item) => item.krs.some((item) => item.id === kr.id))
					if (objective) {
						dirtyObjectives.current.add(objective.id)
						objectiveRevisions.current.set(objective.id, (objectiveRevisions.current.get(objective.id) ?? 0) + 1)
					}
				}
				setSyncState({ kind: 'ready', message: '有修改待保存…' })
				scheduleSave()
			}
			return
		}
    const pending = pointPatches.current.get(pointId)
		pointPatches.current.set(pointId, { ...pending, ...patch })
    pointRevisions.current.set(pointId, (pointRevisions.current.get(pointId) ?? 0) + 1)
    setSyncState({ kind: 'ready', message: '有修改待保存…' })
    schedulePointSave(pointId)
  }, [schedulePointSave, scheduleSave])

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
      if (syncState.kind === 'saving' || dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0) {
        setSyncState({ kind: 'error', message: '请等待当前 Biz OKR Plan 保存后再切换季度。' })
        return
      }
      window.clearTimeout(saveTimer.current)
      quarterRef.current = nextQuarter
      void loadRemote(nextQuarter)
    },
    selectPlan: (id) => {
      if (id === planRef.current?.id) return
      if (syncState.kind === 'saving' || dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0) {
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
        persistedObjectiveIDs.current = new Set(loaded.objectives.map((objective) => objective.id))
        dirtyKRs.current.clear()
        krRevisions.current.clear()
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
        persistedObjectiveIDs.current = new Set(created.objectives.map((objective) => objective.id))
        dirtyKRs.current.clear()
        krRevisions.current.clear()
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
		await deleteOKRPlan(removed.id, removed.deleteToken)
        const loaded = await loadRemote(removed.quarter)
        if (!loaded?.plan) setSyncState({ kind: 'saved', message: 'Biz OKR Plan 已删除，当前季度暂无 Biz OKR Plan' })
      } catch (error) {
		setSyncState({ kind: 'error', message: error instanceof APIError && error.status === 409
			? '这个 Plan 已被其他人更新，请重新载入后再删除。'
			: error instanceof Error ? error.message : '删除 Biz OKR Plan 失败。' })
        throw error
      }
    },
    syncState,
    hasPendingChanges: dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || saveInFlight.current,
    enums,
    templateKey: 'classic',
    week: '',
    previousWeek: undefined,
    availableWeeks: [],
    setWeek: () => undefined,
    setWeeklyScope: () => false,
    deleteWeeklyScope: async () => { throw new Error('Biz OKR Plan 没有周次。') },
    setKrTitle: (_objId, krId, title) => mutateKR(krId, (kr) => { kr.title = title }),
    setKrOwner: (krId, ownerName, ownerEmail, owners) => mutateKR(krId, (kr) => {
      kr.ownerName = ownerName
      kr.ownerEmail = ownerEmail ?? ''
      kr.owners = owners ?? ownerName.split(/[、,，;；]/).map((name) => ({ name: name.trim(), email: '' })).filter((owner) => owner.name)
    }),
    setKrBusinessCategory: (krId, category) => mutateKR(krId, (kr) => { kr.tags = replaceSingleTag(kr.tags, BUSINESS_CATEGORY_TAG, category) }),
    setKrPriority: (krId, priority) => mutateKR(krId, (kr) => { kr.tags = replaceSingleTag(kr.tags, PRIORITY_TAG, priority) }),
    createObjective: async (input) => {
      mutateObjective((draft) => draft.push({ id: uid('plan-o'), title: input.title.trim(), krs: [] }))
    },
    updateObjective: async (id, title) => {
      mutateObjective((draft) => {
        const objective = draft.find((item) => item.id === id)
        if (objective) objective.title = title.trim()
      })
    },
    deleteObjective: async (id) => {
      mutateObjective((draft) => {
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
      mutateObjective((draft) => {
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
      mutateObjective((draft) => {
        const objective = draft.find((item) => item.id === objectiveId)
        if (!objective) return
        const owners = input.owners ?? []
        objective.krs.push({
          id: uid('plan-kr'),
          title: input.title,
          owners,
          ownerName: owners.map((owner) => owner.name).join('、'),
          ownerEmail: owners[0]?.email ?? '',
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
      mutateObjective((draft) => {
        for (const objective of draft) objective.krs = objective.krs.filter((item) => item.id !== krId)
      })
    },
    addTag: (krId, value, type = 'custom') => mutateKR(krId, (kr) => {
      const clean = value.trim()
		if (!clean) return
      kr.tags ??= []
      if (!kr.tags.some((tag) => tag.type === type && tag.value === clean)) kr.tags.push({ type, value: clean })
    }),
    removeTag: (krId, type, value) => mutateKR(krId, (kr) => { kr.tags = (kr.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value) }),
    addPointTag: (_krId, pointId, value, type = 'custom') => {
      const clean = value.trim()
      const point = findPoint(objectivesRef.current, pointId)
      if (!point || !clean) return
      if (!(point.tags ?? []).some((tag) => tag.type === type && tag.value === clean)) {
        mutatePointDefinition(pointId, { tags: [...(point.tags ?? []), { type, value: clean }] })
      }
    },
    removePointTag: (_krId, pointId, type, value) => {
      const point = findPoint(objectivesRef.current, pointId)
      if (point) mutatePointDefinition(pointId, { tags: (point.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value) })
    },
    setPointOwners: (_krId, pointId, owners) => mutatePointDefinition(pointId, { owners }),
    setMetricNote: (krId, note) => mutateKR(krId, (kr) => { kr.metricNote = note }),
    patchMetric: (krId, metricId, patch) => mutateKR(krId, (kr) => {
      const metric = kr.metrics.find((item) => item.id === metricId)
      if (metric) Object.assign(metric, patch)
    }),
    addMetric: (krId, initial) => {
      const id = uid('plan-m')
      mutateKR(krId, (kr) => { kr.metrics.push({ text: '', light: 'green', images: [], ...initial, id }) })
      return id
    },
    removeMetric: (krId, metricId) => mutateKR(krId, (kr) => { kr.metrics = kr.metrics.filter((item) => item.id !== metricId) }),
    setPointTitle: (_objId, _krId, pointId, title) => mutatePointDefinition(pointId, { title }),
    setPointMeegoLink: (_krId, pointId, patch) => mutatePointDefinition(pointId, patch),
    addPoint: (_objId, krId, kind) => mutateKR(krId, (kr) => {
      const point: Point = { id: uid('plan-p'), kind, title: '', tags: [], owners: [], entries: [], previousEntries: [] }
      const lastSameKind = kr.points.map((item) => item.kind).lastIndexOf(kind)
      if (lastSameKind === -1) kr.points.push(point)
      else kr.points.splice(lastSameKind + 1, 0, point)
    }),
    swapPoints: (krId, pointId, targetId) => mutateKR(krId, (kr) => { kr.points = swappedPointsWithinKind(kr.points, pointId, targetId) }),
    setPointKind: (krId, pointId, kind) => {
			const draft = clone(objectivesRef.current)
      const kr = findKr(draft, krId)
      const point = kr?.points.find((item) => item.id === pointId)
      if (!kr || !point || point.kind === kind) return
      point.kind = kind
      const others = kr.points.filter((item) => item.id !== pointId)
      const lastSameKind = others.map((item) => item.kind).lastIndexOf(kind)
      kr.points = lastSameKind === -1 ? [...others, point] : [...others.slice(0, lastSameKind + 1), point, ...others.slice(lastSameKind + 1)]
			objectivesRef.current = draft
			setObjectives(draft)
			mutatePointDefinition(pointId, { kind })
		},
    removePoint: (_objId, krId, pointId) => mutateKR(krId, (kr) => { kr.points = kr.points.filter((item) => item.id !== pointId) }),
    patchEntry: () => undefined,
    addEntry: () => undefined,
    removeEntry: () => undefined,
    setKrScore: async () => undefined,
    setPointScore: async () => undefined,
    reset: () => {
      if (dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || saveInFlight.current) {
        setSyncState({ kind: 'error', message: '请等待当前修改保存后再刷新。' })
        return
      }
      void loadRemote()
    },
    retry: () => {
      if (remoteReady.current && pointPatches.current.size > 0) {
        for (const pointId of pointPatches.current.keys()) schedulePointSave(pointId)
      } else if (remoteReady.current && (dirtyKRs.current.size > 0 || dirtyObjectives.current.size > 0 || objectiveOrderDirty.current)) void saveNow()
      else void loadRemote()
    },
    resolveConflict,
    applySavedKr: (kr) => mutateKR(kr.id, (current) => { Object.assign(current, kr) }),
  }), [availableQuarters, enums, loadRemote, mutateKR, mutateObjective, mutatePointDefinition, objectives, plan, plans, publishPlan, quarter, queueObjectiveOrder, resolveConflict, saveNow, schedulePointSave, syncState])

  return (
    <PlanBoardContext.Provider value={api}>
      <BoardContext.Provider value={api}>{children}</BoardContext.Provider>
    </PlanBoardContext.Provider>
  )
}
