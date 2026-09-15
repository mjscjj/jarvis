import type { Kr } from './types'

function clone<T>(value: T): T {
  if (value === undefined || value === null) return value
  return JSON.parse(JSON.stringify(value)) as T
}

function same(left: unknown, right: unknown): boolean {
  return JSON.stringify(left) === JSON.stringify(right)
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function identified(values: unknown[]): values is Array<Record<string, unknown> & { id: string }> {
  return values.every((value) => isRecord(value) && typeof value.id === 'string')
}

// Rebase only edits made after submitted was captured onto the canonical
// server response. A version is therefore never copied onto stale content:
// unchanged fields come from remote, while newer local edits are explicit.
export function rebasePendingChanges<T>(submitted: T, latest: T, remote: T): T {
  if (same(submitted, latest)) return clone(remote)

  if (Array.isArray(submitted) && Array.isArray(latest) && Array.isArray(remote)) {
    const all = [...submitted, ...latest, ...remote]
    if (!identified(all)) return clone(latest) as T

    const submittedByID = new Map(submitted.map((value) => [value.id, value]))
    const latestByID = new Map(latest.map((value) => [value.id, value]))
    const remoteByID = new Map(remote.map((value) => [value.id, value]))
    const explicitlyDeleted = new Set(submitted.filter((value) => !latestByID.has(value.id)).map((value) => value.id))
    const submittedOrder = submitted.map((value) => value.id)
    const latestOrder = latest.map((value) => value.id)
    const structureChanged = !same(submittedOrder, latestOrder)
    const order = structureChanged
      ? [...latestOrder, ...remote.map((value) => value.id).filter((id) => !latestByID.has(id) && !explicitlyDeleted.has(id))]
      : remote.map((value) => value.id)

    return order.flatMap((id) => {
      const before = submittedByID.get(id)
      const local = latestByID.get(id)
      const saved = remoteByID.get(id)
      if (before && local && saved) return [rebasePendingChanges(before, local, saved)]
      if (local) return [clone(local)]
      if (saved && !explicitlyDeleted.has(id)) return [clone(saved)]
      return []
    }) as T
  }

  if (isRecord(submitted) && isRecord(latest) && isRecord(remote)) {
    const result: Record<string, unknown> = clone(remote)
    const keys = new Set([...Object.keys(submitted), ...Object.keys(latest)])
    for (const key of keys) {
      if (!(key in latest)) {
        if (key in submitted) delete result[key]
        continue
      }
      result[key] = key in submitted && key in remote
        ? rebasePendingChanges(submitted[key], latest[key], remote[key])
        : clone(latest[key])
    }
    return result as T
  }

  return clone(latest)
}

// Conflict resolution is an explicit local-wins action. Unlike automatic
// rebasing it may adopt remote versions because the user has chosen to replace
// the conflicting server values with the retained local draft.
export function adoptRemoteVersionsForOverwrite(local: Kr, remote: Kr): Kr {
  const merged = clone(local)
  const remotePoints = new Map(remote.points.map((point) => [point.id, point]))
  merged.version = remote.version
	merged.structureToken = remote.structureToken
  merged.weeklyCoreVersion = remote.weeklyCoreVersion
  for (const point of merged.points) {
    const remotePoint = remotePoints.get(point.id)
    if (!remotePoint) continue
    point.version = remotePoint.version
    const remoteEntries = new Map(remotePoint.entries.map((entry) => [entry.id, entry]))
    for (const entry of point.entries) {
      const saved = remoteEntries.get(entry.id)
      if (saved) entry.version = saved.version
    }
  }
  return merged
}

export function mergeScoreOnly(current: Kr, remote: Kr, targetKind: 'kr' | 'point', targetID: string): Kr {
  const merged = clone(current)
  if (targetKind === 'kr') {
    merged.score = clone(remote.score)
    return merged
  }
  const localPoint = merged.points.find((point) => point.id === targetID)
  const remotePoint = remote.points.find((point) => point.id === targetID)
  if (localPoint) localPoint.score = clone(remotePoint?.score)
  return merged
}

export function mergePointProgressOnly(current: Kr, remote: Kr, pointID: string, source: string): Kr {
  const merged = clone(current)
  const localPoint = merged.points.find((point) => point.id === pointID)
  const remotePoint = remote.points.find((point) => point.id === pointID)
  if (!localPoint || !remotePoint) return merged

  const remoteSourceEntries = remotePoint.entries.filter((entry) => (entry.source ?? 'manual') === source)
  const remoteByID = new Map(remoteSourceEntries.map((entry) => [entry.id, entry]))
  const sourceIDs = new Set(remoteSourceEntries.map((entry) => entry.id))
  localPoint.entries = localPoint.entries
    .filter((entry) => (entry.source ?? 'manual') !== source || sourceIDs.has(entry.id))
    .map((entry) => remoteByID.has(entry.id) ? clone(remoteByID.get(entry.id)!) : entry)
  const existing = new Set(localPoint.entries.map((entry) => entry.id))
  localPoint.entries.push(...remoteSourceEntries.filter((entry) => !existing.has(entry.id)).map(clone))
  return merged
}
