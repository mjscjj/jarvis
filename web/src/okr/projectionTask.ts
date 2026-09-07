import type { CreateTaskInput, Task } from '../types'

export const projectionTaskStatuses = ['pending', 'executing', 'waiting', 'needs_human'] as const

export function projectionTaskTitle(quarter: string): string {
  return `投影 ${quarter.replace('-', ' ')} OKR 到世界模型`
}

export function projectionTaskInput(quarter: string): CreateTaskInput {
  const title = projectionTaskTitle(quarter)
  const instruction = `加载 okr-world-projector Skill，完整遍历 ${quarter}，并按 Skill 的整季度完成协议建立和审计 OKR 到现实世界的映射。只允许写入 Jarvis 内部世界模型；禁止发送消息、禁止任何外部写入、禁止执行 weekly-report-progress-sync、禁止修改正式 Progress、禁止生成 WorldProgress，最终 effects 必须为空。`
  return {
    title,
    action_type: 'agent_task',
    target: instruction,
    background: {
      module: 'okr',
      skill: 'okr-world-projector',
      quarter,
      scope: 'whole_quarter',
      write_scope: 'jarvis_internal_world_model_only',
      external_effects_forbidden: true,
    },
    source_payload: {
      schema: 'okr_world_projection.v1',
      instruction,
      module: 'okr',
      skill: 'okr-world-projector',
      quarter,
      scope: 'whole_quarter',
      hard_boundaries: {
        send_messages: false,
        external_writes: false,
        modify_formal_progress: false,
        generate_world_progress: false,
        run_progress_sync: false,
        effects_must_be_empty: true,
      },
      acceptance: {
        every_objective_maps_to_project: true,
        every_kr_maps_to_key_matter: true,
        every_point_maps_to_or_advances_key_matter: true,
        every_owner_resolves_to_principal_or_person: true,
        every_owner_occurrence_has_owned_by_relation: true,
        uncovered_nodes: 0,
        unresolved_owners: 0,
        write_confirmed_relations_only: true,
      },
    },
  }
}

export function reusableProjectionTask(tasks: Task[], quarter: string): Task | undefined {
  const title = projectionTaskTitle(quarter)
  return tasks.find((task) => (
    task.title === title
    && task.action_type === 'agent_task'
    && task.target.includes('okr-world-projector')
    && projectionTaskStatuses.includes(task.status as typeof projectionTaskStatuses[number])
  ))
}
