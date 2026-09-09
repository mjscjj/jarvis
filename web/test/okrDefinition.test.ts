import assert from 'node:assert/strict'
import test from 'node:test'
import { definitionSignature } from '../src/okr/emily/definition.ts'
import type { Kr } from '../src/okr/emily/types.ts'

function kr(patch: Partial<Kr> = {}): Kr {
  return {
    id: 'kr-1',
    title: 'KR1：公会大会顺利落地',
    metricNote: '主干指标说明',
    metrics: [{ id: 'm-1', text: '入驻 1200 家', light: 'green', images: [] }],
    owners: [{ name: '张三', openId: 'ou_zhang' }],
    tags: [{ type: 'business_category', value: '公会业务' }],
    points: [{
      id: 'p-1',
      kind: 'strategy',
      title: '策略要点',
      owners: [{ name: '李四', openId: 'ou_li' }],
      tags: [],
      entries: [{ id: 'e-1', status: 'in_progress', text: '本周进展', docs: [], images: [] }],
    }],
    ...patch,
  }
}

test('KR 措辞和人员的改动会被认成父级定义改动', () => {
  const baseline = kr()
  assert.notEqual(definitionSignature(baseline), definitionSignature(kr({ title: '改过的 KR 标题' })))
  assert.notEqual(definitionSignature(baseline), definitionSignature(kr({ owners: [{ name: '王五', openId: 'ou_wang' }] })))
  assert.notEqual(definitionSignature(baseline), definitionSignature(kr({
    owners: [{ name: '张三', openId: 'ou_zhang' }, { name: '新增的人', openId: 'ou_new' }],
  })))
})

test('具体 KR 内容不再触发父级定义写入', () => {
  const baseline = kr()
  const point = baseline.points[0]
  assert.equal(definitionSignature(baseline), definitionSignature(kr({ points: [{ ...point, title: '改过的要点' }] })))
  assert.equal(definitionSignature(baseline), definitionSignature(kr({ points: [{ ...point, owners: [] }] })))
  assert.equal(definitionSignature(baseline), definitionSignature(kr({ points: [{ ...point, kind: 'product' }] })))
})

test('具体 KR 顺序的改动会被认成定义改动', () => {
	const first = kr().points[0]
	const second = { ...first, id: 'p-2', title: '第二个策略要点' }
	const baseline = kr({ points: [first, second] })
	assert.notEqual(definitionSignature(baseline), definitionSignature(kr({ points: [second, first] })))
})

test('周次数据和标签的改动不会被当成定义改动', () => {
  const baseline = kr()
  // 指标文案和灯写进本周副本，标签只在管理与打标改；它们都不该触发主干写入。
  assert.equal(definitionSignature(baseline), definitionSignature(kr({ metricNote: '本周改过的指标说明' })))
  assert.equal(definitionSignature(baseline), definitionSignature(kr({ metrics: [{ id: 'm-1', text: '入驻 900 家', light: 'red', images: [] }] })))
  assert.equal(definitionSignature(baseline), definitionSignature(kr({ tags: [] })))
  assert.equal(definitionSignature(baseline), definitionSignature(kr({
    points: [{ ...baseline.points[0], entries: [{ id: 'e-1', status: 'done', text: '写完了', docs: [], images: [] }] }],
  })))
})

test('同一个人换 open_id 算改动，缺 open_id 不会和有 open_id 混为一谈', () => {
  const withOpenId = kr({ owners: [{ name: '张三', openId: 'ou_zhang' }] })
  const withoutOpenId = kr({ owners: [{ name: '张三', openId: '' }] })
  assert.notEqual(definitionSignature(withOpenId), definitionSignature(withoutOpenId))
})
