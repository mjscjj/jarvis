import type { Kr, KrOwner } from './types'

function ownerSignature(owners: KrOwner[] | undefined): string[] {
  return (owners ?? []).map((owner) => `${owner.openId ?? ''}\u0000${owner.name}`)
}

// Names exactly the parent fields a filling week may push back to the shared
// definition. Concrete KR content has its own point PATCH; only its structural
// identity/order remains in the parent signature.
export function definitionSignature(kr: Kr): string {
  return JSON.stringify([
    kr.title,
    ownerSignature(kr.owners),
    kr.points.map((point) => point.id),
  ])
}
