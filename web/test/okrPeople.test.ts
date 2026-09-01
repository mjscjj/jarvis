import assert from 'node:assert/strict'
import test from 'node:test'
import { addOrResolveOwner, ownerOptions } from '../src/okr/emily/people.ts'
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
