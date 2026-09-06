import assert from 'node:assert/strict'
import test from 'node:test'
import type { ScheduledTask } from '../src/types.ts'
import {
  OKR_ACTIONS,
  actionDefinition,
  actionKeyForSchedule,
  formValueForAction,
  manualTaskInput,
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
    title: 'fixed action',
    action_type: 'agent_task',
    instruction: 'run',
    context_snapshot: {},
    schedule_type: 'weekly',
    daily_time: '09:00',
    weekday: 1,
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

test('keeps the original four actions and binds each to one current prompt', () => {
  assert.deepEqual(OKR_ACTIONS.map((item) => item.key), [
    'remind_missing', 'progress_sync', 'meeting_summary', 'publish_report',
  ])
  assert.equal(new Set(OKR_ACTIONS.map((item) => item.promptKey)).size, 4)
  assert.ok(OKR_ACTIONS.every((item) => item.promptKey.startsWith('okr_agent_')))
})

test('manual action creates an ordinary Agent Task without requiring a schedule', () => {
  const input = manualTaskInput(actionDefinition('remind_missing'))
  assert.equal(input.title, 'OKR · 周报催填')
  assert.equal(input.action_type, 'agent_task')
  assert.deepEqual(input.background, {
    module: 'biz-okr',
    skill: 'okr-agent-orchestrator',
    action_key: 'remind_missing',
    prompt_key: 'okr_agent_weekly_reminder',
  })
  assert.equal((input.source_payload as Record<string, unknown>).prompt_key, 'okr_agent_weekly_reminder')
})

test('stores only fixed action identity and prompt binding in scheduler context', () => {
  const definition = actionDefinition('meeting_summary')
  const input = scheduledTaskInput(definition, { weekday: 3, time: '10:30', intervalMinutes: 360, enabled: true })
  assert.equal(input.schedule_type, 'weekly')
  assert.equal(input.weekday, 3)
  assert.equal(input.daily_time, '10:30')
  assert.deepEqual(input.context_snapshot, {
    module: 'biz-okr',
    skill: 'okr-agent-orchestrator',
    action_key: 'meeting_summary',
    prompt_key: 'okr_agent_report_c',
  })
  assert.equal('action_scope' in input.context_snapshot, false)
  assert.equal('action_recipient' in input.context_snapshot, false)
})

test('recognizes fixed schedules and preserves editable timing only', () => {
  const task = scheduled({
    enabled: true,
    weekday: 4,
    daily_time: '11:15',
    context_snapshot: {
      module: 'biz-okr', skill: 'okr-agent-orchestrator',
      action_key: 'publish_report', prompt_key: 'okr_agent_report_b',
    },
  })
  assert.equal(actionKeyForSchedule(task), 'publish_report')
  assert.deepEqual(formValueForAction(actionDefinition('publish_report'), task), {
    weekday: 4, time: '11:15', intervalMinutes: 360, enabled: true,
  })
})
