/**
 * 把 O → KR → 分组 → 具体 KR 点 拍平成表格行。
 * 折叠掉的分支直接不产出行，所以「收起后剩下什么」这件事在这里就能验证。
 */

import type { Kr, Objective, Point, PointKind } from './types'

export const KINDS: PointKind[] = ['strategy', 'product']

export function groupId(krId: string, kind: PointKind) {
  return `${krId}:${kind}`
}

export type Row =
  | { type: 'objective'; obj: Objective; open: boolean }
  | { type: 'kr'; objId: string; kr: Kr; open: boolean }
  | {
      type: 'group'
      objId: string
      krId: string
      kind: PointKind
      id: string
      count: number
      open: boolean
    }
  | { type: 'point'; objId: string; krId: string; point: Point; open: boolean }

export function buildRows(objectives: Objective[], closed: Set<string>): Row[] {
  const rows: Row[] = []
  const isOpen = (id: string) => !closed.has(id)

  for (const obj of objectives) {
    rows.push({ type: 'objective', obj, open: isOpen(obj.id) })
    if (!isOpen(obj.id)) continue

    for (const kr of obj.krs) {
      rows.push({ type: 'kr', objId: obj.id, kr, open: isOpen(kr.id) })
      if (!isOpen(kr.id)) continue

      for (const kind of KINDS) {
        const points = kr.points.filter((p) => p.kind === kind)
        if (points.length === 0) continue

        const gid = groupId(kr.id, kind)
        rows.push({
          type: 'group',
          objId: obj.id,
          krId: kr.id,
          kind,
          id: gid,
          count: points.length,
          open: isOpen(gid),
        })
        if (!isOpen(gid)) continue

        for (const point of points) {
          rows.push({ type: 'point', objId: obj.id, krId: kr.id, point, open: isOpen(point.id) })
        }
      }
    }
  }

  return rows
}

/** 全部收起 = 收到 KR 那一层，剩下 O + KR 标题 + 核心数据 */
export function collapseAllIds(objectives: Objective[]) {
  return new Set(objectives.flatMap((o) => o.krs.map((k) => k.id)))
}
