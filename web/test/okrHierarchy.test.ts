import assert from 'node:assert/strict'
import test from 'node:test'
import { buildKRHierarchy, businessCategoryOf, hierarchyKRCount, priorityOf, replaceSingleTag } from '../src/okr/emily/hierarchy.ts'
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
  assert.deepEqual(navigation[2].priorities[0].objectives.map((item) => item.id), ['o-2', 'o-empty'])
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
