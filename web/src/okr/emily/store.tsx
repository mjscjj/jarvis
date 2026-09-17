import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { APIError, createKR, createObjective as createObjectiveRequest, createProgress, deleteKR, deleteObjective as deleteObjectiveRequest, deleteProgress, deleteWeeklyReportWeek, deleteWeeklyScore, getBoard, getEnums, getWeeklyKR, listWeeklyReportWeeks, patchPointDefinition, reorderKRs, reorderObjectives, replaceKR, replaceKRDefinition, replaceWeeklyKRCore, replaceWeeklyScore, updateObjective as updateObjectiveRequest, updateProgress, type BoardData, type BoardSurface, type PointDefinitionPatchResult } from './api'
import { BoardContext, uid, type BoardApi, type SyncState } from './board'
import { definitionSignature } from './definition'
import { adoptRemoteVersionsForOverwrite, mergePointProgressOnly, mergeScoreOnly, rebasePendingChanges } from './concurrency'
import { findKrDraftIssue } from './draftValidation'
import { BUSINESS_CATEGORY_TAG, PRIORITY_TAG, replaceSingleTag } from './hierarchy'
import { swappedOrder, swappedPointsWithinKind } from './ordering'
import { LIGHTS, STATUSES } from './template'
import type { Entry, EnumValues, Kr, KrOwner, Objective, Point, WeekTemplateKey, WeeklyScore } from './types'
import { resolveWeeklyBoardScope } from './weeklyScope'
import { krConflictLocation } from './saveNotice'

const SAVE_DELAY_MS = 700

type PendingPointPatch = {
	krId: string
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
  initialWeek = '',
  onQuarterChange,
}: {
  children: ReactNode
  surface?: BoardSurface
  weekTemplateKey?: WeekTemplateKey
  initialQuarter?: string
  initialWeek?: string
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
	const deleteTokenRef = useRef('')
  const remoteReady = useRef(false)
  const serverKrs = useRef(new Map<string, Kr>())
  const revisions = useRef(new Map<string, number>())
  const timers = useRef(new Map<string, number>())
  const pointTimers = useRef(new Map<string, number>())
  const pointPatches = useRef(new Map<string, PendingPointPatch>())
  const pointRevisions = useRef(new Map<string, number>())
  const pointSavesInFlight = useRef(new Set<string>())
  const lastFailedKr = useRef<string | null>(null)
  const syncStateRef = useRef(syncState)
  const focusRefreshPending = useRef(false)
  const focusRefreshInFlight = useRef(false)
  const loadRequest = useRef(0)
  syncStateRef.current = syncState

  const hasLocalWork = useCallback(() => (
    timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 ||
    syncStateRef.current.kind === 'saving' || syncStateRef.current.kind === 'conflict'
  ), [])

  const publish = useCallback((next: Objective[]) => {
    objectivesRef.current = next
    setObjectives(next)
  }, [])

  const scheduleSaveRef = useRef<(krId: string) => void>(() => undefined)
  const schedulePointSaveRef = useRef<(pointId: string) => void>(() => undefined)

  const savePointNow = useCallback(async (pointId: string) => {
    const patch = pointPatches.current.get(pointId)
    if (!remoteReady.current || !patch || pointSavesInFlight.current.has(pointId)) return
    const revision = pointRevisions.current.get(pointId) ?? 0
    let failed = false
    pointSavesInFlight.current.add(pointId)
    setSyncState({ kind: 'saving', message: '正在保存具体 KR…' })
    try {
      const currentPoint = findKr(objectivesRef.current, patch.krId)?.points.find((point) => point.id === pointId)
			if (!currentPoint) {
				pointPatches.current.delete(pointId)
				pointRevisions.current.delete(pointId)
				return
			}
			const saved = await patchPointDefinition({ pointId, expectedVersion: currentPoint.version ?? 0, title: patch.title, owners: patch.owners, kind: patch.kind, meegoWorkItemId: patch.meegoWorkItemId, meegoUrl: patch.meegoUrl, tags: patch.tags })
      const current = findKr(objectivesRef.current, patch.krId)
      if (current) {
        const next = clone(current)
				if (saved.deleteToken !== undefined) next.deleteToken = saved.deleteToken
				const point = next.points.find((item) => item.id === pointId)
				if (point) point.version = saved.version
        publish(replaceKrIn(objectivesRef.current, patch.krId, next))
      }
      const baseline = serverKrs.current.get(patch.krId)
      if (baseline) {
        const nextBaseline = clone(baseline)
		if (saved.deleteToken !== undefined) nextBaseline.deleteToken = saved.deleteToken
        const point = nextBaseline.points.find((item) => item.id === pointId)
        if (point) {
					point.version = saved.version
          if (patch.title !== undefined) point.title = patch.title
          if (patch.owners !== undefined) point.owners = clone(patch.owners)
					if (patch.kind !== undefined) point.kind = patch.kind
					if (patch.meegoWorkItemId !== undefined) point.meegoWorkItemId = patch.meegoWorkItemId
					if (patch.meegoUrl !== undefined) point.meegoUrl = patch.meegoUrl
					if (patch.tags !== undefined) point.tags = clone(patch.tags)
        }
				serverKrs.current.set(patch.krId, nextBaseline)
      }
      if ((pointRevisions.current.get(pointId) ?? 0) === revision) {
        pointPatches.current.delete(pointId)
        pointRevisions.current.delete(pointId)
      }
      if (pointPatches.current.size === 0 && timers.current.size === 0) setSyncState({ kind: 'saved', message: '已自动保存' })
    } catch (error) {
      failed = true
		if (error instanceof APIError && error.status === 409 && error.data) {
				const remote = error.data as PointDefinitionPatchResult
				const current = findKr(objectivesRef.current, patch.krId)
				if (current) {
					setSyncState({
						kind: 'conflict',
						message: '这个具体 KR 刚被其他人更新，请选择保留哪一版。',
						krId: patch.krId,
						pointId,
						location: krConflictLocation(objectivesRef.current, patch.krId, pointId),
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
      const pendingKR = pointPatches.current.get(pointId)?.krId ?? patch.krId
      if (!failed && timers.current.has(pendingKR)) scheduleSaveRef.current(pendingKR)
    }
  }, [publish])

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

  const saveNow = useCallback(async (krId: string) => {
    if (!remoteReady.current) return
    if (Array.from(pointSavesInFlight.current).some((pointId) => pointPatches.current.get(pointId)?.krId === krId)) {
      scheduleSaveRef.current(krId)
      return
    }
    const current = findKr(objectivesRef.current, krId)
    if (!current) return
    const snapshot = clone(current)
    const revision = revisions.current.get(krId) ?? 0
    const draftIssue = findKrDraftIssue(snapshot, { allowImageOnlyMetrics: surface === 'weekly-report' })
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
      const latest = findKr(objectivesRef.current, krId)
      if (latest) {
        const merged = rebasePendingChanges(snapshot, latest, saved)
        publish(replaceKrIn(objectivesRef.current, krId, merged))
      }
      if ((revisions.current.get(krId) ?? 0) === revision) {
        setSyncState({ kind: 'saved', message: '已自动保存' })
      } else {
        scheduleSaveRef.current(krId)
      }
    } catch (error) {
      lastFailedKr.current = krId
      if (error instanceof APIError && error.status === 409 && error.data) {
        setSyncState({ kind: 'conflict', message: '这条 KR 刚被其他人更新，请选择保留哪一版。', krId, location: krConflictLocation(objectivesRef.current, krId), local: snapshot, remote: error.data as Kr })
        return
      }
	  setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '保存失败，请稍后重试。', logid: error instanceof APIError ? error.logid : undefined })
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

  const loadRemote = useCallback(async (targetWeek?: string, targetQuarter?: string, options?: { background?: boolean }): Promise<BoardData | undefined> => {
    const background = options?.background === true
    if (background && (!remoteReady.current || hasLocalWork())) return
    const request = ++loadRequest.current
    const startingQuarter = quarterRef.current
    const startingWeek = weekRef.current
    const startingObjectives = background ? JSON.stringify(objectivesRef.current) : ''
    if (!background) {
      remoteReady.current = false
      setSyncState({ kind: 'loading', message: '正在读取本周进展…' })
    }
    try {
      if (!background) {
        for (const pointTimer of pointTimers.current.values()) window.clearTimeout(pointTimer)
        pointTimers.current.clear()
      }
      let board: BoardData
      let remoteEnums: EnumValues
      if (surface === 'weekly-report') {
        [board, remoteEnums] = await Promise.all([
          resolveWeeklyBoardScope(
            targetQuarter ?? quarterRef.current,
            targetWeek,
            weekTemplateKey,
            { listWeeks: listWeeklyReportWeeks, getBoard },
          ),
          getEnums(),
        ])
      } else {
        [board, remoteEnums] = await Promise.all([
          getBoard(targetQuarter ?? quarterRef.current, targetWeek ?? '', surface),
          getEnums(),
        ])
      }
      if (request !== loadRequest.current) return
      if (background && (hasLocalWork() || quarterRef.current !== startingQuarter || weekRef.current !== startingWeek || JSON.stringify(objectivesRef.current) !== startingObjectives)) {
        focusRefreshPending.current = true
        return
      }
      publish(board.objectives)
      serverKrs.current = new Map(board.objectives.flatMap((objective) => objective.krs).map((kr) => [kr.id, clone(kr)]))
      revisions.current.clear()
      pointPatches.current.clear()
      pointRevisions.current.clear()
      lastFailedKr.current = null
      quarterRef.current = board.quarter
      setQuarterState(board.quarter)
      setAvailableQuarters(board.availableQuarters)
      onQuarterChange?.(board.quarter)
      weekRef.current = board.week
		deleteTokenRef.current = board.deleteToken ?? ''
      setWeekState(board.week)
      setTemplateKey(board.templateKey)
      setPreviousWeek(board.previousWeek)
      setAvailableWeeks(board.availableWeeks)
      setEnums(remoteEnums)
      remoteReady.current = true
      setSyncState({ kind: 'ready', message: board.week ? '本周进展已加载' : surface === 'weekly-report' ? '当前季度暂无对应周次' : 'OKR 已加载' })
      return board
    } catch (error) {
      if (!background && request === loadRequest.current) setSyncState({ kind: 'error', title: '读取失败', message: error instanceof Error ? error.message : '加载失败，请稍后重试。', logid: error instanceof APIError ? error.logid : undefined })
    }
  }, [hasLocalWork, onQuarterChange, publish, surface, weekTemplateKey])

  useEffect(() => {
    const activeTimers = timers.current
    const startupTimer = window.setTimeout(() => void loadRemote(initialWeek, initialQuarter), 0)
    return () => {
      window.clearTimeout(startupTimer)
      for (const timer of activeTimers.values()) window.clearTimeout(timer)
      for (const timer of pointTimers.current.values()) window.clearTimeout(timer)
      pointTimers.current.clear()
    }
  }, [loadRemote])

  useEffect(() => {
    const warnBeforeUnload = (event: BeforeUnloadEvent) => {
      if (timers.current.size === 0 && pointPatches.current.size === 0 && pointSavesInFlight.current.size === 0) return
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
      await loadRemote(weekRef.current, quarterRef.current, { background: true })
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
    if (timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
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
    setSyncState({ kind: 'ready', message: '有修改待保存…' })
    scheduleSave(krId)
  }, [publish, scheduleSave])

	const mutatePointDefinition = useCallback((krId: string, pointId: string, patch: Omit<PendingPointPatch, 'krId'>) => {
    const draft = clone(objectivesRef.current)
    const point = findKr(draft, krId)?.points.find((item) => item.id === pointId)
    if (!point) return
		const persisted = point.version !== undefined
		Object.assign(point, patch)
    publish(draft)
		// New points are created by the already queued structural KR save. Once
		// the server assigns a point version, every later edit uses point PATCH.
		if (!persisted) {
			revisions.current.set(krId, (revisions.current.get(krId) ?? 0) + 1)
			setSyncState({ kind: 'ready', message: '有修改待保存…' })
			scheduleSave(krId)
			return
		}
    const pending = pointPatches.current.get(pointId)
    pointPatches.current.set(pointId, { ...pending, krId, ...patch })
    pointRevisions.current.set(pointId, (pointRevisions.current.get(pointId) ?? 0) + 1)
    setSyncState({ kind: 'ready', message: '有修改待保存…' })
    schedulePointSave(pointId)
  }, [publish, schedulePointSave, scheduleSave])

  const resolveConflict = useCallback((choice: 'remote' | 'local') => {
    if (syncState.kind !== 'conflict') return
    const selected = choice === 'remote' ? syncState.remote : adoptRemoteVersionsForOverwrite(syncState.local, syncState.remote)
    serverKrs.current.set(syncState.krId, clone(syncState.remote))
    publish(replaceKrIn(objectivesRef.current, syncState.krId, selected))
    if (choice === 'remote') {
		if (syncState.pointId) {
			pointPatches.current.delete(syncState.pointId)
			pointRevisions.current.delete(syncState.pointId)
		} else if (lastFailedKr.current === syncState.krId) lastFailedKr.current = null
      setSyncState({ kind: 'saved', message: '已载入他人更新' })
		for (const pointId of pointPatches.current.keys()) schedulePointSave(pointId)
      return
    }
	setSyncState({ kind: 'saving', message: '正在保存你的修改…' })
	if (syncState.pointId) {
		pointRevisions.current.set(syncState.pointId, (pointRevisions.current.get(syncState.pointId) ?? 0) + 1)
		schedulePointSave(syncState.pointId)
		return
	}
    revisions.current.set(syncState.krId, (revisions.current.get(syncState.krId) ?? 0) + 1)
    scheduleSave(syncState.krId)
  }, [publish, schedulePointSave, scheduleSave, syncState])

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
      const baseline = serverKrs.current.get(krId)
      if (baseline) serverKrs.current.set(krId, mergeScoreOnly(baseline, saved, targetKind, targetId))
      const current = findKr(objectivesRef.current, krId)
      if (current) publish(replaceKrIn(objectivesRef.current, krId, mergeScoreOnly(current, saved, targetKind, targetId)))
      setSyncState({ kind: 'saved', message: '评分已保存' })
    } catch (error) {
      if (error instanceof APIError && error.status === 409 && error.data) {
        const current = error.data as Kr
        const baseline = serverKrs.current.get(krId)
        if (baseline) serverKrs.current.set(krId, mergeScoreOnly(baseline, current, targetKind, targetId))
        const local = findKr(objectivesRef.current, krId)
        if (local) publish(replaceKrIn(objectivesRef.current, krId, mergeScoreOnly(local, current, targetKind, targetId)))
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
      if (timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
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
      if (timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
        setSyncState({ kind: 'error', message: '请等待当前修改保存后再切换周次。' })
        return
      }
      weekRef.current = nextWeek
			setWeekState(nextWeek)
			void loadRemote(nextWeek)
		},
		setWeeklyScope: (nextQuarter, nextWeek) => {
			if (nextQuarter === quarterRef.current && nextWeek === weekRef.current) return true
			if (timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict') {
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
			if (timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'loading' || syncState.kind === 'conflict') {
				throw new Error('请等待当前修改保存后再删除本周。')
			}
			remoteReady.current = false
			setSyncState({ kind: 'saving', message: `正在删除 ${selectedWeek} ${lifecycleName}…` })
			try {
				if (!deleteTokenRef.current) throw new Error('当前周次缺少删除校验，请重新载入后再试。')
				const result = await deleteWeeklyReportWeek(selectedQuarter, selectedWeek, deleteTokenRef.current)
				const loaded = await loadRemote('', selectedQuarter)
				return { ...result, nextWeek: loaded?.week || undefined }
			} catch (error) {
				remoteReady.current = true
				setSyncState({ kind: 'error', message: error instanceof APIError && error.status === 409
					? '这个周次已被其他人更新，请重新载入后再删除。'
					: error instanceof Error ? error.message : '删除本周失败。' })
				throw error
			}
		},
		enums,
    syncState,
    hasPendingChanges: timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving' || syncState.kind === 'conflict',

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

	updateObjective: async (id, title, owners) => {
      const clean = title.trim()
      if (!clean) throw new Error('目标名称不能为空。')
			const current = objectivesRef.current.find((objective) => objective.id === id)
			if (!current) throw new Error('目标不存在，请重新载入。')
      setSyncState({ kind: 'saving', message: '正在更新目标…' })
      try {
		const saved = await updateObjectiveRequest(current, clean, owners)
				publish(objectivesRef.current.map((objective) => objective.id === id ? { ...objective, title: saved.title, version: saved.version, owners: saved.owners } : objective))
        setSyncState({ kind: 'saved', message: '目标已更新' })
      } catch (error) {
        setSyncState({ kind: 'error', message: error instanceof Error ? error.message : '更新目标失败。' })
        throw error
      }
    },

    deleteObjective: async (id) => {
			const current = objectivesRef.current.find((objective) => objective.id === id)
			if (!current) throw new Error('目标不存在，请重新载入。')
      setSyncState({ kind: 'saving', message: '正在删除空目标…' })
      try {
        await deleteObjectiveRequest(current)
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
				const expectedOrder = objectivesRef.current.map((objective) => objective.id)
        const order = await reorderObjectives(quarterRef.current, swappedOrder(expectedOrder, id, targetId), expectedOrder)
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

    reorderObjectives: async (ids) => {
      setSyncState({ kind: 'saving', message: '正在调整顺序…' })
      try {
				const expectedOrder = objectivesRef.current.map((objective) => objective.id)
        const order = await reorderObjectives(quarterRef.current, ids, expectedOrder)
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
				const expectedOrder = objective.krs.map((kr) => kr.id)
        const order = await reorderKRs(objectiveId, swappedOrder(expectedOrder, krId, targetId), expectedOrder)
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
        serverKrs.current.set(created.id, clone(created))
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
      const pointIds = current.points.map((point) => point.id)
      for (const pointId of pointIds) {
        const pointTimer = pointTimers.current.get(pointId)
        if (pointTimer) window.clearTimeout(pointTimer)
        pointTimers.current.delete(pointId)
        pointPatches.current.delete(pointId)
        pointRevisions.current.delete(pointId)
      }
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
    setKrOwner: (krId, ownerName, ownerEmail, owners) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) {
        kr.ownerName = ownerName
        if (ownerEmail !== undefined) kr.ownerEmail = ownerEmail
        if (owners !== undefined) {
          kr.owners = owners
        } else {
          const previous = kr.owners ?? []
          kr.owners = ownerName.split(/[、,，;；]/).map((name) => name.trim()).filter(Boolean).map((name, index) => ({
            name,
            email: previous.find((owner) => owner.name === name)?.email ?? (index === 0 ? (ownerEmail ?? kr.ownerEmail ?? '') : ''),
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
    addPointTag: (krId, pointId, value, type = 'custom') => {
      const point = findKr(objectivesRef.current, krId)?.points.find((item) => item.id === pointId)
      const clean = value.trim()
      if (!point || !clean) return
      if (!(point.tags ?? []).some((tag) => tag.type === type && tag.value === clean)) mutatePointDefinition(krId, pointId, { tags: [...(point.tags ?? []), { type, value: clean }] })
    },
    removePointTag: (krId, pointId, type, value) => {
      const point = findKr(objectivesRef.current, krId)?.points.find((item) => item.id === pointId)
      if (point) mutatePointDefinition(krId, pointId, { tags: (point.tags ?? []).filter((tag) => tag.type !== type || tag.value !== value) })
    },
    setPointOwners: (krId, pointId, owners) => mutatePointDefinition(krId, pointId, { owners }),
    setMetricNote: (krId, note) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.metricNote = note
    }),
    patchMetric: (krId, metricId, patch) => mutate(krId, (draft) => {
      const metric = findKr(draft, krId)?.metrics.find((item) => item.id === metricId)
      if (metric) Object.assign(metric, patch)
    }),
    addMetric: (krId, initial) => {
      const id = uid('m')
      mutate(krId, (draft) => {
        findKr(draft, krId)?.metrics.push({ text: '', light: 'green', images: [], ...initial, id })
      })
      return id
    },
    removeMetric: (krId, metricId) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.metrics = kr.metrics.filter((item) => item.id !== metricId)
    }),
    setPointTitle: (_objId, krId, pointId, title) => mutatePointDefinition(krId, pointId, { title }),
    setPointMeegoLink: (krId, pointId, patch) => mutatePointDefinition(krId, pointId, patch),
    addPoint: (_objId, krId, kind) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (!kr) return
      const point: Point = { id: uid('p'), kind, title: '', tags: [], entries: [{ id: uid('e'), status: 'in_progress', text: '', docs: [], images: [] }] }
      const lastSameKind = kr.points.map((item) => item.kind).lastIndexOf(kind)
      if (lastSameKind === -1) kr.points.push(point)
      else kr.points.splice(lastSameKind + 1, 0, point)
    }),
    swapPoints: (krId, pointId, targetId) => mutate(krId, (draft) => {
      const kr = findKr(draft, krId)
      if (kr) kr.points = swappedPointsWithinKind(kr.points, pointId, targetId)
    }),
		setPointKind: (krId, pointId, kind) => {
			const draft = clone(objectivesRef.current)
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
			publish(draft)
			mutatePointDefinition(krId, pointId, { kind })
		},
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
    reset: () => {
      if (timers.current.size > 0 || pointPatches.current.size > 0 || pointSavesInFlight.current.size > 0 || syncState.kind === 'saving') {
        setSyncState({ kind: 'error', message: '请等待当前修改保存后再刷新。' })
        return
      }
      void loadRemote()
    },
    retry: () => {
      if (pointPatches.current.size > 0 && remoteReady.current) {
        for (const pointId of pointPatches.current.keys()) schedulePointSave(pointId)
      }
      else if (lastFailedKr.current && remoteReady.current) void saveNow(lastFailedKr.current)
      else void loadRemote()
    },
    resolveConflict,
    applySavedKr: (kr, pointId) => {
      if (!pointId) return
      const baseline = serverKrs.current.get(kr.id)
      if (baseline) serverKrs.current.set(kr.id, mergePointProgressOnly(baseline, kr, pointId, 'meego'))
      const current = findKr(objectivesRef.current, kr.id)
      if (current) publish(replaceKrIn(objectivesRef.current, kr.id, mergePointProgressOnly(current, kr, pointId, 'meego')))
    },
  }), [availableQuarters, availableWeeks, enums, loadRemote, mutate, mutatePointDefinition, objectives, previousWeek, publish, quarter, resolveConflict, saveNow, saveWeeklyScore, schedulePointSave, syncState, templateKey, week, weekTemplateKey])

  return <BoardContext.Provider value={api}>{children}</BoardContext.Provider>
}
