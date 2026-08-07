import assert from 'node:assert/strict'
import test from 'node:test'
import type { Task } from '../src/types.ts'
import { proposalOf, structureProposalAction } from '../src/tasks/taskPresentation.ts'

test('structures a free-form numbered proposal without dropping its parts', () => {
  const result = structureProposalAction('在独立分支实施，不发布：1) 修改 core 并补测试。2) 修改 gateway。3) 运行 PPE 回归。')
  assert.equal(result.introduction, '在独立分支实施，不发布')
  assert.deepEqual(result.steps, ['修改 core 并补测试', '修改 gateway', '运行 PPE 回归'])
})

test('keeps ordinary proposal text intact', () => {
  const text = '更新文档后发给 Principal 确认。'
  assert.deepEqual(structureProposalAction(text), { introduction: text, steps: [] })
})

test('does not treat dates and metrics as numbered steps', () => {
  const text = '8 月 10 日前完成，模型调用 17 次，耗时 206 秒。'
  assert.deepEqual(structureProposalAction(text), { introduction: text, steps: [] })
})

test('treats a preserved proposal as active only while awaiting approval', () => {
  const task = {
    status: 'awaiting_approval',
    execution_result: {
      stage: 'proposal',
      proposal: { action: '发消息', target: '群聊', artifact: '内容' },
    },
  } as Task
  assert.equal(proposalOf(task)?.proposal.action, '发消息')
  assert.equal(proposalOf({ ...task, status: 'done' }), null)
})
