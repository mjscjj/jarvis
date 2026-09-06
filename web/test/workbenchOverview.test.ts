import assert from 'node:assert/strict'
import test from 'node:test'
import type { Task } from '../src/types.ts'
import { taskOneLine, taskUpdatedOn, workbenchOverviewStatuses } from '../src/review/workbenchOverview.ts'

test('workbench overview owns the four requested task groups', () => {
  assert.deepEqual(workbenchOverviewStatuses.needsDecision, ['needs_human'])
  assert.deepEqual(workbenchOverviewStatuses.inProgress, ['pending', 'executing', 'waiting'])
  assert.deepEqual(workbenchOverviewStatuses.completed, ['done'])
  assert.deepEqual(workbenchOverviewStatuses.risk, ['failed'])
})

test('workbench overview turns task context into one readable line', () => {
  const task = {
    status: 'needs_human',
    summary: null,
    execution_result: { question: { title: '是否发布？', body: '已完成验证，\n发布后会对外可见。' } },
  } as Task
  assert.equal(taskOneLine(task), '是否发布？ 已完成验证， 发布后会对外可见。')
})

test('workbench overview filters terminal tasks by natural day', () => {
  const task = { updated_at: '2026-09-06T08:30:00Z' } as Task
  assert.equal(taskUpdatedOn(task, '2026-09-06'), true)
  assert.equal(taskUpdatedOn(task, '2026-09-05'), false)
})
