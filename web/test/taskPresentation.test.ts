import assert from 'node:assert/strict'
import test from 'node:test'
import type { Task } from '../src/types.ts'
import { questionOf, questionText } from '../src/tasks/taskPresentation.ts'

const parked = (question: unknown) => ({
  status: 'needs_human',
  execution_result: { question },
}) as Task

test('reads the question a parked task is waiting on', () => {
  const task = parked({
    title: '要不要把这条发到群里？',
    body: '文案已经写好，发出后无法撤回。',
    fields: [{ type: 'button', name: 'decision', label: '发送' }],
  })
  assert.equal(questionOf(task)?.title, '要不要把这条发到群里？')
  assert.equal(questionText(task), '要不要把这条发到群里？\n\n文案已经写好，发出后无法撤回。')
})

test('falls back to the title alone when there is no body', () => {
  assert.equal(questionText(parked({ title: '用哪个分支？' })), '用哪个分支？')
})

test('ignores a question that cannot be rendered', () => {
  assert.equal(questionOf(parked({ body: '只有正文' })), null)
  assert.equal(questionOf(parked({ title: '   ' })), null)
  assert.equal(questionOf(parked(null)), null)
  assert.equal(questionOf({ status: 'needs_human' } as Task), null)
})
