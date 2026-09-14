import assert from 'node:assert/strict'
import test from 'node:test'
import { addOrResolveOwner, krHasAnyOwner, krOwnerOptions, ownerIdentityKey, ownerOptions, rankKrOwnerSuggestions } from '../src/okr/emily/people.ts'
import type { Objective } from '../src/okr/emily/types.ts'

test('owner options keep the resolvable identity for an existing name', () => {
	const objectives = [{
		id: 'o-1', title: '方向', krs: [
			{ id: 'kr-1', title: '姓名版本', metricNote: '', metrics: [], points: [], ownerName: '甲', owners: [{ name: '甲', email: '' }] },
			{ id: 'kr-2', title: '身份版本', metricNote: '', metrics: [], points: [], ownerName: '甲、乙', owners: [{ name: '甲', email: 'a@example.test' }, { name: '乙', email: 'b@example.test' }] },
		],
	}] satisfies Objective[]

	assert.deepEqual(Object.fromEntries(ownerOptions(objectives).map((owner) => [owner.name, owner.email])), {
		甲: 'a@example.test',
		乙: 'b@example.test',
	})
})

test('selecting a feishu identity replaces the matching unresolved owner', () => {
	const owners = [{ name: '李鑫', email: '' }, { name: '乙', email: 'b@example.test' }]
	assert.deepEqual(addOrResolveOwner(owners, { name: '李鑫', email: 'lixin@example.test' }), [
		{ name: '李鑫', email: 'lixin@example.test' },
		{ name: '乙', email: 'b@example.test' },
	])
})

test('owner selection requires a resolved email and deduplicates by identity', () => {
	const owners = [{ name: '李鑫', email: 'a@example.test' }]
	assert.deepEqual(addOrResolveOwner(owners, { name: '另一个名字', email: 'a@example.test' }), owners)
	assert.deepEqual(addOrResolveOwner(owners, { name: '手填姓名', email: '' }), owners)
	assert.deepEqual(addOrResolveOwner(owners, { name: '李鑫', email: 'b@example.test' }), [
		...owners,
		{ name: '李鑫', email: 'b@example.test' },
	])
})

test('KR owner filter options use stable identities and exclude point-only owners', () => {
	const objectives = [{
		id: 'o-1', title: '方向', krs: [{
			id: 'kr-1', title: '目标', metricNote: '', metrics: [], ownerName: '甲、乙',
			owners: [{ name: '甲', email: 'a@example.test' }, { name: '乙', email: 'b@example.test' }],
			points: [{ id: 'point-1', kind: 'strategy' as const, title: '事项', owners: [{ name: '丙', email: 'c@example.test' }], entries: [] }],
		}],
	}] satisfies Objective[]

	assert.deepEqual(krOwnerOptions(objectives).map(ownerIdentityKey).sort(), ['email:a@example.test', 'email:b@example.test'])
})

test('multiple KR owners use OR matching and do not duplicate a KR', () => {
	const kr = {
		id: 'kr-1', title: '共同负责', metricNote: '', metrics: [], points: [], ownerName: '甲、乙',
		owners: [{ name: '甲', email: 'a@example.test' }, { name: '乙', email: 'b@example.test' }],
	}

	assert.equal(krHasAnyOwner(kr, [{ name: '乙', email: 'b@example.test' }, { name: '丙', email: 'c@example.test' }]), true)
	assert.equal(krHasAnyOwner(kr, [{ name: '丙', email: 'c@example.test' }]), false)
	assert.equal(krHasAnyOwner(kr, []), true)
})

test('KR owner matching falls back to exact names only for unresolved legacy identities', () => {
	const kr = { id: 'kr-1', title: '旧数据', metricNote: '', metrics: [], points: [], ownerName: '甲', ownerEmail: '' }

	assert.equal(krHasAnyOwner(kr, [{ name: '甲', email: 'a@example.test' }]), true)
	assert.equal(krHasAnyOwner(kr, [{ name: '甲', email: 'other@example.test' }]), true)
	assert.equal(krHasAnyOwner({ ...kr, ownerEmail: 'a@example.test' }, [{ name: '甲', email: 'other@example.test' }]), false)
})

test('owner suggestions prefer recent people and otherwise rank by KR count', () => {
	const owners = [
		{ name: '甲', email: 'a@example.test' },
		{ name: '乙', email: 'b@example.test' },
		{ name: '丙', email: 'c@example.test' },
	]
	const counts = new Map([['email:a@example.test', 2], ['email:b@example.test', 5], ['email:c@example.test', 3]])

	assert.deepEqual(rankKrOwnerSuggestions(owners, [], counts).map((owner) => owner.name), ['乙', '丙', '甲'])
	assert.deepEqual(rankKrOwnerSuggestions(owners, ['email:c@example.test', 'missing', 'email:a@example.test'], counts).map((owner) => owner.name), ['丙', '甲', '乙'])
})
