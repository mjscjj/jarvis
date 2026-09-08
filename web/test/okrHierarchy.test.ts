import assert from 'node:assert/strict'
import test from 'node:test'
import { buildAllBusinessNavigation, buildGlobalPriorityNavigation, buildKRHierarchy, businessCategoryOf, businessCategoryOptions, commonObjectiveBusinessCategory, filterObjectivesByHierarchy, hierarchyKRCount, objectivesForBusiness, priorityKRCount, priorityOf, replaceSingleTag, withCompletePriorityNavigation, withSelectedBusinessCategory } from '../src/okr/emily/hierarchy.ts'
import type { Kr, Objective } from '../src/okr/emily/types.ts'

function kr(id: string, business?: string, priority?: string): Kr {
  return {
    id, title: id, metricNote: '', metrics: [], points: [],
    tags: [
      ...(business ? [{ type: 'business_category', value: business }] : []),
      ...(priority ? [{ type: 'priority', value: priority }] : []),
    ],
  }
}

test('标签按业务、优先级和 Objective 方向组装三级导航', () => {
  const objectives: Objective[] = [
    { id: 'o-1', title: '方向改名不影响归属', krs: [kr('kr-focus', '公会业务', 'p0'), kr('kr-p1', '公会业务', 'p1')] },
    { id: 'o-2', title: '运营方向', krs: [kr('kr-ops', '运营效率', 'p0'), kr('kr-missing')] },
    { id: 'o-empty', title: '空方向', krs: [] },
  ]

  const navigation = buildKRHierarchy(objectives)
  assert.deepEqual(navigation.map((item) => item.label), ['公会业务', '运营效率', '未标注业务'])
  assert.deepEqual(navigation[0].priorities.map((item) => item.label), ['Focus', 'P1'])
  assert.equal(hierarchyKRCount(navigation[0]), 2)
  assert.deepEqual(navigation[0].priorities[0].objectives[0].krs.map((item) => item.id), ['kr-focus'])
  assert.deepEqual(navigation[0].priorities[1].objectives[0].krs.map((item) => item.id), ['kr-p1'])
	assert.deepEqual(navigation[2].priorities[0].objectives.map((item) => item.id), ['o-2'])

  const globalPriorities = buildGlobalPriorityNavigation(navigation)
  assert.deepEqual(globalPriorities.map((item) => item.label), ['Focus', 'P1', '未标注'])
  assert.deepEqual(globalPriorities.map(priorityKRCount), [2, 1, 1])
  assert.deepEqual(buildAllBusinessNavigation(navigation), { value: '__all__', label: '全部 OKR', priorities: globalPriorities })
})

test('全部 OKR 按优先级合并同一方向，而不是复制方向选项', () => {
  const navigation = buildKRHierarchy([
    { id: 'o-shared', title: '共享方向', krs: [kr('kr-a', '业务 A', 'p0'), kr('kr-b', '业务 B', 'p0')] },
  ])
  const globalPriorities = buildGlobalPriorityNavigation(navigation)
  assert.equal(globalPriorities[0].objectives.length, 1)
  assert.deepEqual(globalPriorities[0].objectives[0].krs.map((item) => item.id), ['kr-a', 'kr-b'])
})

test('管理页固定展示 Focus P1 P2，并在有数据时保留未标注入口', () => {
	const [business] = buildKRHierarchy([
		{ id: 'o-1', title: '方向', krs: [kr('kr-focus', '公会业务', 'p0'), kr('kr-untagged', '公会业务')] },
	])
	const complete = withCompletePriorityNavigation(business)
	assert.deepEqual(complete?.priorities.map((item) => [item.value, priorityKRCount(item)]), [
		['p0', 1],
		['p1', 0],
		['p2', 0],
		['', 1],
	])
	assert.equal(withCompletePriorityNavigation(undefined), undefined)
})

test('业务分类标签条的选项和计数与三级导航同源', () => {
  const objectives: Objective[] = [
    { id: 'o-1', title: '公会', krs: [kr('kr-a', '公会业务', 'p0'), kr('kr-b', '公会业务', 'p1')] },
    { id: 'o-2', title: '运营', krs: [kr('kr-c', '运营效率', 'p0'), kr('kr-d')] },
  ]
  assert.deepEqual(businessCategoryOptions(buildKRHierarchy(objectives)), [
    { value: '公会业务', label: '公会业务', count: 2 },
    { value: '运营效率', label: '运营效率', count: 1 },
    { value: '', label: '未标注业务', count: 1 },
  ])
})

// A filter that leaves an objective without KRs must not invent an untagged
// category, so management drops those objectives before building the strip.
test('筛空的方向不产生 0 条的未标注业务选项', () => {
  const emptied: Objective[] = [{ id: 'o-1', title: '被筛空', krs: [] }]
	assert.deepEqual(businessCategoryOptions(buildKRHierarchy(emptied)), [])
  assert.deepEqual(businessCategoryOptions(buildKRHierarchy(emptied.filter((objective) => objective.krs.length > 0))), [])
})

test('业务分类按固定口径排序并只改展示名', () => {
	const objectives: Objective[] = [{
		id: 'o-1',
		title: '方向',
		krs: [
			kr('kr-other', '其他业务', 'p0'),
			kr('kr-star', '优质主播 & 内容专项', 'p0'),
			kr('kr-ai', 'AI提效', 'p0'),
			kr('kr-guild', '公会业务', 'p0'),
			kr('kr-ops', '运营效率', 'p0'),
		],
	}]
	assert.deepEqual(businessCategoryOptions(buildKRHierarchy(objectives)).map((item) => [item.value, item.label]), [
		['公会业务', '公会业务'],
		['运营效率', '运营效率'],
		['AI提效', 'AI提效'],
		['优质主播 & 内容专项', '优质主播&内容专项'],
		['其他业务', '其他业务'],
	])
})

test('三层筛选逐级收窄且全部范围保留空 O', () => {
	const objectives: Objective[] = [
		{ id: 'o-1', title: '共享方向', krs: [kr('kr-a', '公会业务', 'p0'), kr('kr-b', '公会业务', 'p1'), kr('kr-c', '运营效率', 'p0')] },
		{ id: 'o-empty', title: '空方向', krs: [] },
	]
	assert.deepEqual(filterObjectivesByHierarchy(objectives, undefined, undefined, '').map((item) => item.id), ['o-1', 'o-empty'])
	assert.deepEqual(filterObjectivesByHierarchy(objectives, '公会业务', undefined, '')[0].krs.map((item) => item.id), ['kr-a', 'kr-b'])
	assert.deepEqual(filterObjectivesByHierarchy(objectives, '公会业务', 'p0', '')[0].krs.map((item) => item.id), ['kr-a'])
	assert.deepEqual(filterObjectivesByHierarchy(objectives, undefined, 'p0', 'o-1')[0].krs.map((item) => item.id), ['kr-a', 'kr-c'])

	const allBusiness = buildAllBusinessNavigation(buildKRHierarchy(objectives))
	assert.deepEqual(objectivesForBusiness(allBusiness)[0].krs.map((item) => item.id), ['kr-a', 'kr-c', 'kr-b'])
})

test('被其它筛选清空的当前分类仍留在标签条上，计数为 0', () => {
  const options = businessCategoryOptions(buildKRHierarchy([{ id: 'o-1', title: '公会', krs: [kr('kr-a', '公会业务', 'p1')] }]))
  assert.deepEqual(withSelectedBusinessCategory(options, '算法进展'), [
    { value: '公会业务', label: '公会业务', count: 1 },
    { value: '算法进展', label: '算法进展', count: 0 },
  ])
  assert.deepEqual(withSelectedBusinessCategory(options, ''), [
    { value: '公会业务', label: '公会业务', count: 1 },
    { value: '', label: '未标注业务', count: 0 },
  ])
  assert.equal(withSelectedBusinessCategory(options, '公会业务'), options)
  assert.equal(withSelectedBusinessCategory(options, undefined), options)
})

test('结构标签保持单值且优先级不再依赖 KR 独立字段', () => {
  const tags = replaceSingleTag([{ type: 'priority', value: 'p1' }, { type: 'custom', value: '保留' }], 'priority', 'p0')
  const item = kr('kr-1')
  item.tags = replaceSingleTag(tags, 'business_category', '公会业务')
  assert.equal(priorityOf(item), 'p0')
  assert.equal(businessCategoryOf(item), '公会业务')
  assert.deepEqual(item.tags, [
    { type: 'custom', value: '保留' },
    { type: 'priority', value: 'p0' },
    { type: 'business_category', value: '公会业务' },
  ])
})

test('O 的业务分类只投影其下全部 KR 的共同分类', () => {
  assert.equal(commonObjectiveBusinessCategory({ id: 'o-empty', title: '空 O', krs: [] }), undefined)
  assert.equal(commonObjectiveBusinessCategory({
    id: 'o-one',
    title: '同一业务',
    krs: [kr('kr-a', '公会业务'), kr('kr-b', '公会业务')],
  }), '公会业务')
  assert.equal(commonObjectiveBusinessCategory({
    id: 'o-mixed',
    title: '多个业务',
    krs: [kr('kr-a', '公会业务'), kr('kr-b', '运营效率')],
  }), undefined)
})
