import assert from 'node:assert/strict'
import test from 'node:test'
import { mergeVisibleObjectiveOrder, swappedOrder } from '../src/okr/emily/ordering.ts'

test('上移下移交换两行位置，其余行原位不动', () => {
  const ids = ['o-1', 'o-2', 'o-3', 'o-4']
  assert.deepEqual(swappedOrder(ids, 'o-3', 'o-2'), ['o-1', 'o-3', 'o-2', 'o-4'])
  assert.deepEqual(swappedOrder(ids, 'o-1', 'o-2'), ['o-2', 'o-1', 'o-3', 'o-4'])
  assert.deepEqual(ids, ['o-1', 'o-2', 'o-3', 'o-4'])
})

test('筛选藏起来的行留在原位，被换的两行跨过它', () => {
  // 页面上只看到 o-1 和 o-4（o-2、o-3 被业务分类筛掉了），点 o-4 的上移。
  const order = swappedOrder(['o-1', 'o-2', 'o-3', 'o-4'], 'o-4', 'o-1')
  assert.deepEqual(order, ['o-4', 'o-2', 'o-3', 'o-1'])
})

test('目标行不在顺序里直接报错，不静默乱排', () => {
  assert.throws(() => swappedOrder(['o-1', 'o-2'], 'o-1', 'o-missing'), /重新载入/)
  assert.throws(() => swappedOrder(['o-1', 'o-2'], 'o-missing', 'o-1'), /重新载入/)
})

test('拖动筛选后的具体 O 只交换可见 O 占据的全量位置', () => {
  assert.deepEqual(
    mergeVisibleObjectiveOrder(
      ['o-a', 'o-hidden-x', 'o-b', 'o-hidden-y', 'o-c'],
      ['o-c', 'o-b', 'o-a'],
    ),
    ['o-c', 'o-hidden-x', 'o-b', 'o-hidden-y', 'o-a'],
  )
})

test('具体 O 的局部顺序必须是全量顺序的无重复子集', () => {
  assert.throws(() => mergeVisibleObjectiveOrder(['o-a', 'o-b'], ['o-a', 'o-a']), /重新载入/)
  assert.throws(() => mergeVisibleObjectiveOrder(['o-a', 'o-b'], ['o-a', 'o-missing']), /重新载入/)
})
