import assert from 'node:assert/strict'
import test from 'node:test'
import { addOrResolveOwner, krHasAnyOwner, krOwnerOptions, ownerIdentityKey, ownerOptions, rankKrOwnerSuggestions } from '../src/okr/emily/people.ts'
import type { Objective } from '../src/okr/emily/types.ts'

test('owner options keep the resolvable identity for an existing name', () => {
	const objectives = [{
		id: 'o-1', title: '方向', krs: [
			{ id: 'kr-1', title: '姓名版本', metricNote: '', metrics: [], points: [], ownerName: '甲', owners: [{ name: '甲', openId: '' }] },
			{ id: 'kr-2', title: '身份版本', metricNote: '', metrics: [], points: [], ownerName: '甲、乙', owners: [{ name: '甲', openId: 'ou_a' }, { name: '乙', openId: 'ou_b' }] },
		],
	}] satisfies Objective[]

	assert.deepEqual(Object.fromEntries(ownerOptions(objectives).map((owner) => [owner.name, owner.openId])), {
		甲: 'ou_a',
		乙: 'ou_b',
	})
})

test('selecting a feishu identity replaces the matching unresolved owner', () => {
	const owners = [{ name: '李鑫', openId: '' }, { name: '乙', openId: 'ou_b' }]
	assert.deepEqual(addOrResolveOwner(owners, { name: '李鑫', openId: 'ou_lixin' }), [
		{ name: '李鑫', openId: 'ou_lixin' },
		{ name: '乙', openId: 'ou_b' },
	])
})

test('owner selection requires a resolved open_id and deduplicates by identity', () => {
	const owners = [{ name: '李鑫', openId: 'ou_a' }]
	assert.deepEqual(addOrResolveOwner(owners, { name: '另一个名字', openId: 'ou_a' }), owners)
	assert.deepEqual(addOrResolveOwner(owners, { name: '手填姓名', openId: '' }), owners)
	assert.deepEqual(addOrResolveOwner(owners, { name: '李鑫', openId: 'ou_b' }), [
		...owners,
		{ name: '李鑫', openId: 'ou_b' },
	])
})

test('KR owner filter options use stable identities and exclude point-only owners', () => {
	const objectives = [{
		id: 'o-1', title: '方向', krs: [{
			id: 'kr-1', title: '目标', metricNote: '', metrics: [], ownerName: '甲、乙',
			owners: [{ name: '甲', openId: 'ou_a' }, { name: '乙', openId: 'ou_b' }],
			points: [{ id: 'point-1', kind: 'strategy' as const, title: '事项', owners: [{ name: '丙', openId: 'ou_c' }], entries: [] }],
		}],
	}] satisfies Objective[]

	assert.deepEqual(krOwnerOptions(objectives).map(ownerIdentityKey).sort(), ['open_id:ou_a', 'open_id:ou_b'])
})

test('multiple KR owners use OR matching and do not duplicate a KR', () => {
	const kr = {
		id: 'kr-1', title: '共同负责', metricNote: '', metrics: [], points: [], ownerName: '甲、乙',
		owners: [{ name: '甲', openId: 'ou_a' }, { name: '乙', openId: 'ou_b' }],
	}

	assert.equal(krHasAnyOwner(kr, [{ name: '乙', openId: 'ou_b' }, { name: '丙', openId: 'ou_c' }]), true)
	assert.equal(krHasAnyOwner(kr, [{ name: '丙', openId: 'ou_c' }]), false)
	assert.equal(krHasAnyOwner(kr, []), true)
})

test('KR owner matching falls back to exact names only for unresolved legacy identities', () => {
	const kr = { id: 'kr-1', title: '旧数据', metricNote: '', metrics: [], points: [], ownerName: '甲', ownerOpenId: '' }

	assert.equal(krHasAnyOwner(kr, [{ name: '甲', openId: 'ou_a' }]), true)
	assert.equal(krHasAnyOwner(kr, [{ name: '甲', openId: 'ou_other' }]), true)
	assert.equal(krHasAnyOwner({ ...kr, ownerOpenId: 'ou_a' }, [{ name: '甲', openId: 'ou_other' }]), false)
})

test('owner suggestions prefer recent people and otherwise rank by KR count', () => {
	const owners = [
		{ name: '甲', openId: 'ou_a' },
		{ name: '乙', openId: 'ou_b' },
		{ name: '丙', openId: 'ou_c' },
	]
	const counts = new Map([['open_id:ou_a', 2], ['open_id:ou_b', 5], ['open_id:ou_c', 3]])

	assert.deepEqual(rankKrOwnerSuggestions(owners, [], counts).map((owner) => owner.name), ['乙', '丙', '甲'])
	assert.deepEqual(rankKrOwnerSuggestions(owners, ['open_id:ou_c', 'missing', 'open_id:ou_a'], counts).map((owner) => owner.name), ['丙', '甲', '乙'])
})
