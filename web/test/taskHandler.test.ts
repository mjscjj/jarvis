import assert from 'node:assert/strict'
import test from 'node:test'
import type { Task } from '../src/types.ts'
import { modelCloseReason, taskHandlerMeta } from '../src/tasks/taskPresentation.ts'

function resolvedBy(actorType: string): Task {
  return {
    resolution: {
      event_type: 'closed',
      actor_type: actorType,
      actor_ref: null,
      occurred_at: '2026-08-05T10:00:00Z',
    },
  } as Task
}

test('labels human and model task resolution distinctly', () => {
  assert.equal(taskHandlerMeta(resolvedBy('user'))?.label, '人工处理')
  assert.equal(taskHandlerMeta(resolvedBy('m5'))?.label, '模型处理')
  assert.equal(taskHandlerMeta(resolvedBy('proactive'))?.label, '模型关闭')
})

test('does not guess a handler before terminal resolution', () => {
  assert.equal(taskHandlerMeta({ resolution: null } as Task), null)
})

test('shows the persisted reason only for proactive model closure', () => {
  const task = {
    ...resolvedBy('proactive'),
    summary: '跨日审批已过期，当前没有继续执行价值。',
    execution_result: { summary: '旧理由' },
  }
  assert.equal(modelCloseReason(task), '跨日审批已过期，当前没有继续执行价值。')
  assert.equal(modelCloseReason({ ...task, resolution: { ...task.resolution!, actor_type: 'm5' } }), null)
})
