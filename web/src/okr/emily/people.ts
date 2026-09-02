import type { KrOwner, Objective } from './types'

export function splitOwnerNames(value?: string) {
  return (value ?? '').split(/[、,，;；]/).map((item) => item.trim()).filter(Boolean)
}

export function joinOwnerNames(values: string[]) {
  return [...new Set(values.map((item) => item.trim()).filter(Boolean))].join('、')
}

export function hasOwner(value: string | undefined, owner: string) {
  return splitOwnerNames(value).includes(owner)
}

export function ownerIdentityKey(owner: KrOwner) {
	return owner.openId ? `open_id:${owner.openId}` : `name:${owner.name}`
}

export function addOrResolveOwner(owners: KrOwner[], candidate: KrOwner): KrOwner[] {
	const normalized = { name: candidate.name.trim(), openId: candidate.openId.trim() }
	if (!normalized.name || !normalized.openId) return owners
	if (owners.some((owner) => owner.openId === normalized.openId)) return owners
	const unresolvedIndex = owners.findIndex((owner) => !owner.openId && owner.name === normalized.name)
	if (unresolvedIndex < 0) return [...owners, normalized]
	return owners.map((owner, index) => index === unresolvedIndex ? normalized : owner)
}

export function ownerOptions(objectives: Objective[]): KrOwner[] {
	const byName = new Map<string, KrOwner>()
	for (const kr of objectives.flatMap((objective) => objective.krs)) {
		const krOwners = kr.owners?.length
			? kr.owners
			: splitOwnerNames(kr.ownerName).map((name, index) => ({ name, openId: index === 0 ? (kr.ownerOpenId ?? '') : '' }))
		const structured = [...krOwners, ...kr.points.flatMap((point) => point.owners ?? [])]
		for (const owner of structured) {
			const name = owner.name.trim()
			if (!name) continue
			const previous = byName.get(name)
			if (!previous || (!previous.openId && owner.openId)) byName.set(name, { name, openId: owner.openId.trim() })
		}
	}
	return [...byName.values()].sort((left, right) => left.name.localeCompare(right.name))
}
