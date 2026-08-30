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

export function ownerOptions(objectives: Objective[]): KrOwner[] {
	const byName = new Map<string, KrOwner>()
	for (const kr of objectives.flatMap((objective) => objective.krs)) {
		const structured = kr.owners?.length
			? kr.owners
			: splitOwnerNames(kr.ownerName).map((name, index) => ({ name, openId: index === 0 ? (kr.ownerOpenId ?? '') : '' }))
		for (const owner of structured) {
			const name = owner.name.trim()
			if (!name) continue
			const previous = byName.get(name)
			if (!previous || (!previous.openId && owner.openId)) byName.set(name, { name, openId: owner.openId.trim() })
		}
	}
	return [...byName.values()].sort((left, right) => left.name.localeCompare(right.name))
}
