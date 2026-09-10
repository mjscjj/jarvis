import assert from 'node:assert/strict'
import test from 'node:test'
import { checkInput, progressText, updatedProgress } from '../src/delegations/presentation.ts'
import { getDelegation, listDelegations, listDelegationTasks, updateDelegation } from '../src/api.ts'
import type { Delegation } from '../src/types.ts'

test('a new check keeps the commitment identity and complete original context', () => {
  const source = { source: { payload: '原始理解' }, capture: { messages: [{ content: '完整前序背景' }] }, annotation: { owner: '张三' } }
  const content = { summary: '已改派李四，尚未交付', evidence: ['message:2'], novel: ['unrestricted'] }
  const row = { id: 7, title: '张三交方案', source_payload: source, content } as Delegation
  const input = checkInput(row)
  const payload = input.source_payload as Record<string, unknown>
  assert.equal(input.action_type, 'investigate')
  assert.equal(payload.delegation_id, 7)
  assert.deepEqual(payload.original_context, source)
  assert.deepEqual(payload.current_progress, content)
  assert.equal(progressText(content), '已改派李四，尚未交付')
  assert.deepEqual(updatedProgress(content, '已交付'), { ...content, summary: '已交付' })
  assert.equal(content.summary, '已改派李四，尚未交付')
})

test('check progress accepts prose and arbitrary evidence without requiring owner fields', () => {
  assert.equal(progressText('尚未回复'), '尚未回复')
  assert.equal(progressText(null), '')
  assert.deepEqual(updatedProgress(['原始核验', { anything: true }], '新进展'), { summary: '新进展', detail: ['原始核验', { anything: true }] })
})

test('commitment API updates progress independently from Task completion', async t => {
  const calls: Array<{ path: string; method: string; body: unknown }> = []
  t.mock.method(globalThis, 'fetch', async (path: string, init: RequestInit = {}) => {
    calls.push({ path, method: init.method || 'GET', body: init.body ? JSON.parse(String(init.body)) : undefined })
    return new Response(JSON.stringify({ code: 0, data: { id: 7, version: 1, items: [] } }))
  })
  await listDelegations('open', '张三', 2)
  await getDelegation(7)
  await listDelegationTasks(7, 2)
  await updateDelegation(7, 0, { summary: '检查完成，对方尚未交付', free: [1, 'a'] }, false)
  assert.equal(new URL(calls[0].path, 'http://local').searchParams.get('query'), '张三')
  assert.equal(calls[1].path, '/api/delegations/7')
  assert.equal(calls[2].path, '/api/delegations/7/tasks?page=2&page_size=20')
  assert.deepEqual(calls[3], { path: '/api/delegations/7', method: 'PATCH', body: { expected_version: 0, content: { summary: '检查完成，对方尚未交付', free: [1, 'a'] }, closed: false, actor: 'user' } })
  assert.ok(calls.every(call => !call.path.includes('/finish')))
})
