import type { Kr, KrOwner, Objective } from './types'

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

export function krOwners(kr: Kr): KrOwner[] {
	if (kr.owners?.length) return kr.owners
	return splitOwnerNames(kr.ownerName).map((name, index) => ({ name, openId: index === 0 ? (kr.ownerOpenId ?? '') : '' }))
}

function normalizedOwner(owner: KrOwner): KrOwner {
	return { name: owner.name.trim(), openId: owner.openId.trim() }
}

export function krOwnerOptions(objectives: Objective[]): KrOwner[] {
	const byIdentity = new Map<string, KrOwner>()
	const resolvedNames = new Set<string>()
	for (const kr of objectives.flatMap((objective) => objective.krs)) {
		for (const rawOwner of krOwners(kr)) {
			const owner = normalizedOwner(rawOwner)
			if (!owner.name) continue
			byIdentity.set(ownerIdentityKey(owner), owner)
			if (owner.openId) resolvedNames.add(owner.name)
		}
	}
	return [...byIdentity.values()]
		.filter((owner) => owner.openId || !resolvedNames.has(owner.name))
		.sort((left, right) => left.name.localeCompare(right.name) || left.openId.localeCompare(right.openId))
}

export function ownerMatches(left: KrOwner, right: KrOwner): boolean {
	const normalizedLeft = normalizedOwner(left)
	const normalizedRight = normalizedOwner(right)
	if (normalizedLeft.openId && normalizedRight.openId) return normalizedLeft.openId === normalizedRight.openId
	return Boolean(normalizedLeft.name) && normalizedLeft.name === normalizedRight.name
}

export function krHasAnyOwner(kr: Kr, selectedOwners: KrOwner[]): boolean {
	if (selectedOwners.length === 0) return true
	return krOwners(kr).some((owner) => selectedOwners.some((selected) => ownerMatches(owner, selected)))
}

export function rankKrOwnerSuggestions(options: KrOwner[], recentKeys: string[], ownerCounts: ReadonlyMap<string, number>, limit = 5): KrOwner[] {
	const byKey = new Map(options.map((owner) => [ownerIdentityKey(owner), owner]))
	const recent = recentKeys.flatMap((key) => {
		const owner = byKey.get(key)
		return owner ? [owner] : []
	})
	const recentSet = new Set(recent.map(ownerIdentityKey))
	const fallback = options
		.filter((owner) => !recentSet.has(ownerIdentityKey(owner)))
		.sort((left, right) => (ownerCounts.get(ownerIdentityKey(right)) ?? 0) - (ownerCounts.get(ownerIdentityKey(left)) ?? 0) || left.name.localeCompare(right.name))
	return [...recent, ...fallback].slice(0, limit)
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
