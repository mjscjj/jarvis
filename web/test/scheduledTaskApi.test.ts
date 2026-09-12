import assert from 'node:assert/strict'
import test from 'node:test'
import { getScheduledTask, listScheduledTasks } from '../src/api.ts'

test('scheduled task list and detail use separate reads', async (t) => {
  const calls: string[] = []
  t.mock.method(globalThis, 'fetch', async (path: string) => {
    calls.push(path)
    const data = path === '/api/scheduled-tasks/7'
      ? { id: 7, title: 'timer', context_snapshot: { project_id: 9 }, dispatch_payload: { reason: 'wait' } }
      : { items: [{ id: 7, title: 'timer', instruction: 'run' }] }
    return Response.json({ code: 0, data })
  })

  const list = await listScheduledTasks()
  assert.equal(list.items[0].id, 7)
  const detail = await getScheduledTask(7)
  assert.deepEqual(detail.context_snapshot, { project_id: 9 })
  assert.deepEqual(calls, ['/api/scheduled-tasks?limit=200', '/api/scheduled-tasks/7'])
})
