import type { Kr, KrOwner } from './types'

function ownerSignature(owners: KrOwner[] | undefined): string[] {
  return (owners ?? []).map((owner) => `${owner.openId ?? ''}\u0000${owner.name}`)
}

// Names exactly the fields a filling week is allowed to push back onto the
// shared definition: the wording and the people of the KR and of its existing
// points, plus the point array order. Metrics, lights, labels and progress belong to a week or to
// 管理与打标, so a change there must not look like a definition edit.
export function definitionSignature(kr: Kr): string {
  return JSON.stringify([
    kr.title,
    ownerSignature(kr.owners),
    kr.points.map((point) => [point.id, point.title, ownerSignature(point.owners)]),
  ])
}
