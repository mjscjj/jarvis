import assert from 'node:assert/strict'
import test from 'node:test'
import { getTask, getTaskRun, listTaskRuns, listTasks } from '../src/api.ts'

test('task detail and paged run index use separate reads from run bodies', async (t) => {
  const calls: string[] = []
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    const data = path.includes('/runs?')
      ? { total: 25, page: 2, page_size: 20, items: [{ id: 5 }] }
      : path.includes('/task-runs/')
        ? { id: 5, output: { summary: '完整运行结果' }, effects: [{ kind: 'custom' }] }
        : { id: 9, execution_result: { question: { title: '问题', body: '完整正文' } } }
    return new Response(JSON.stringify({ code: 0, data }), { status: 200 })
  })
  const task = await getTask(9)
  assert.equal((task.execution_result?.question as { body: string }).body, '完整正文')
  const page = await listTaskRuns(9, 2, 20)
  assert.equal(page.total, 25)
  assert.equal(page.page, 2)
  assert.deepEqual(calls, ['/api/tasks/9?context=full', '/api/tasks/9/runs?page=2&page_size=20'])
  const run = await getTaskRun(page.items[0].id)
  assert.equal(run.output?.summary, '完整运行结果')
  assert.equal(calls.at(-1), '/api/task-runs/5')
})

test('task list forwards open action type filters', async (t) => {
  const calls: string[] = []
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    return new Response(JSON.stringify({
      code: 0,
      data: { total: 0, page: 1, page_size: 20, items: [] },
    }), { status: 200 })
  })

  await listTasks(['waiting'], 1, 20, undefined, { actionType: 'delegated_followup' })
  await listTasks(['pending'], 2, 20, undefined, { excludeActionType: 'delegated_followup' })

  assert.equal(calls[0], '/api/tasks?status=waiting&page=1&page_size=20&action_type=delegated_followup')
  assert.equal(calls[1], '/api/tasks?status=pending&page=2&page_size=20&exclude_action_type=delegated_followup')
})
