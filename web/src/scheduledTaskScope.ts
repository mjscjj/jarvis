import type { ScheduledTaskInput } from './types.ts'

export interface ScheduleScope {
  pluginID: string
  skills: { name: string; description: string; available: boolean }[]
}

// Keep background extensions while binding the selected Skill and plugin.
export function bindScheduleScope(input: ScheduledTaskInput, scope: ScheduleScope, skill: string): ScheduledTaskInput {
  if (!scope.skills.some((item) => item.name === skill && item.available)) {
    throw new Error('请选择已启用的产品 Skill')
  }
  return { ...input, context_snapshot: { ...input.context_snapshot, plugin: scope.pluginID, skill } }
}
