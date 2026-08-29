import assert from 'node:assert/strict'
import test from 'node:test'
import type { ScheduledTask } from '../src/types.ts'
import {
  actionDefinition,
  actionKeyForSchedule,
  formValueForAction,
  scheduledTaskInput,
} from '../src/okr/emily/actionConfig.ts'

function scheduled(overrides: Partial<ScheduledTask>): ScheduledTask {
  return {
    id: 1,
    dispatch_kind: 'create_task',
    subject_type: null,
    subject_id: null,
    source_run_id: null,
    dispatch_payload: null,
    title: 'legacy',
    action_type: 'agent_task',
    instruction: 'legacy',
    context_snapshot: {},
    schedule_type: 'daily',
    daily_time: '09:00',
    interval_minutes: null,
    run_at: null,
    next_run_at: '2026-08-31T01:00:00Z',
    enabled: false,
    status: 'active',
    last_run_status: null,
    last_task_id: null,
    last_result: null,
    last_error_detail: null,
    last_started_at: null,
    last_finished_at: null,
    created_at: '2026-08-28T00:00:00Z',
    updated_at: '2026-08-28T00:00:00Z',
    ...overrides,
  }
}

test('recognizes migrated reminder and progress schedules without workflow metadata', () => {
  assert.equal(actionKeyForSchedule(scheduled({ context_snapshot: { module: 'weekly-report', skill: 'weekly-report-reminder' } })), 'remind_missing')
  assert.equal(actionKeyForSchedule(scheduled({ context_snapshot: { module: 'weekly-report', skill: 'weekly-report-progress-sync' } })), 'progress_sync')
})

test('stores business cadence in context while reusing the generic scheduler', () => {
  const definition = actionDefinition('publish_report')
  const input = scheduledTaskInput(definition, { weekday: 5, time: '17:30', intervalMinutes: 360, target: '管理群', enabled: false })
  assert.equal(input.schedule_type, 'daily')
  assert.equal(input.daily_time, '17:30')
  assert.equal(input.enabled, false)
  assert.deepEqual(input.context_snapshot.action_config, {
    mode: 'publish_report', timezone: 'Asia/Shanghai', weekday: 5, scope: 'current_week',
    recipient_rule: '配置的飞书目标', target: '管理群', approval: 'require_approval',
  })
})

test('editing preserves an existing action cadence and enabled state', () => {
  const task = scheduled({
    enabled: true,
    daily_time: '11:15',
    context_snapshot: { workflow: 'meeting_summary', action_config: { weekday: 3, target: '' } },
  })
  assert.deepEqual(formValueForAction(actionDefinition('meeting_summary'), task), {
    weekday: 3, time: '11:15', intervalMinutes: 360, target: '', enabled: true,
  })
})
