import assert from 'node:assert/strict'
import test from 'node:test'
import type { Task } from '../src/types.ts'
import { projectionTaskInput, projectionTaskTitle, reusableProjectionTask } from '../src/okr/projectionTask.ts'

function task(id: number, title: string, status: Task['status']): Task {
  return {
    id, todo_id: null, title, action_type: 'agent_task', target: '加载 okr-world-projector Skill', source_payload: null, status,
    execution_result: null, summary: null, last_progress_at: null, project_id: null, source_type: 'manual',
    source_id: null, occurrence_key: null, version: 0, created_at: '', updated_at: '', resolution: null,
  }
}

test('builds a normal agent task with a frozen whole-quarter projection contract', () => {
  const input = projectionTaskInput('2026-Q3')
  assert.equal(input.title, '投影 2026 Q3 OKR 到世界模型')
  assert.equal(input.action_type, 'agent_task')
  assert.deepEqual(input.background, {
    module: 'okr',
    skill: 'okr-world-projector',
    quarter: '2026-Q3',
    scope: 'whole_quarter',
    write_scope: 'jarvis_internal_world_model_only',
    external_effects_forbidden: true,
  })
  const sourcePayload = input.source_payload as Record<string, unknown>
  assert.equal(sourcePayload.schema, 'okr_world_projection.v1')
  assert.deepEqual(sourcePayload.hard_boundaries, {
    send_messages: false,
    external_writes: false,
    modify_formal_progress: false,
    generate_world_progress: false,
    run_progress_sync: false,
    effects_must_be_empty: true,
  })
  assert.deepEqual(sourcePayload.acceptance, {
    every_objective_maps_to_project: true,
    every_kr_maps_to_key_matter: true,
    every_point_maps_to_or_advances_key_matter: true,
    every_owner_resolves_to_principal_or_person: true,
    every_owner_occurrence_has_owned_by_relation: true,
    uncovered_nodes: 0,
    unresolved_owners: 0,
    write_confirmed_relations_only: true,
  })
  assert.match(input.target, /okr-world-projector/)
  assert.match(input.target, /禁止发送消息/)
  assert.match(input.target, /禁止任何外部写入/)
  assert.match(input.target, /禁止执行 weekly-report-progress-sync/)
  assert.match(input.target, /禁止修改正式 Progress/)
  assert.match(input.target, /禁止生成 WorldProgress/)
  assert.match(input.target, /effects 必须为空/)
})

test('reuses only a non-terminal projection task for the same quarter', () => {
  const q3 = projectionTaskTitle('2026-Q3')
  const tasks = [task(1, projectionTaskTitle('2026-Q2'), 'executing'), task(2, q3, 'done'), task(3, q3, 'waiting')]
  assert.equal(reusableProjectionTask(tasks, '2026-Q3')?.id, 3)
  assert.equal(reusableProjectionTask([task(4, q3, 'done')], '2026-Q3'), undefined)
})
