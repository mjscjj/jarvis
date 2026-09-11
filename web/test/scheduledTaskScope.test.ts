import assert from 'node:assert/strict'
import test from 'node:test'
import { bindScheduleScope } from '../src/scheduledTaskScope.ts'
import type { ScheduledTaskInput } from '../src/types.ts'

const input: ScheduledTaskInput = {
  title: 'Review', instruction: 'Review 指定文档', action_type: 'agent_task',
  context_snapshot: { document: 'https://example.com/prd', attention: ['指标'], plugin: 'old' },
  schedule_type: 'weekly', daily_time: '09:00', weekday: 1, interval_minutes: null, run_at: null, enabled: false,
}
const scope = { pluginID: 'product-management', skills: [{ name: 'product-prd-review', description: 'Review', available: true }] }

test('plugin schedules use common inputs and preserve context and timing on edit', () => {
  const result = bindScheduleScope(input, scope, 'product-prd-review')
  assert.deepEqual(result.context_snapshot, { document: 'https://example.com/prd', attention: ['指标'], plugin: 'product-management', skill: 'product-prd-review' })
  assert.equal(result.enabled, false)
  assert.equal(result.weekday, 1)
  assert.equal(result.instruction, input.instruction)
  assert.equal(input.context_snapshot.plugin, 'old')
})

test('unknown or disabled Skills cannot be selected for plugin schedules', () => {
  assert.throws(() => bindScheduleScope(input, scope, 'missing'), /Skill/)
  assert.throws(() => bindScheduleScope(input, { ...scope, skills: [{ ...scope.skills[0], available: false }] }, 'product-prd-review'), /Skill/)
})
