import type { CreateTaskInput, ScheduledTask, ScheduledTaskInput } from '../../types'

export type OKRActionKey = 'remind_missing' | 'progress_sync' | 'meeting_summary' | 'publish_report'
export type OKRActionCategory = 'notification' | 'material'

export interface OKRActionDefinition {
  key: OKRActionKey
  title: string
  shortLabel: string
  description: string
  promptKey: string
  cadence: 'weekly' | 'interval'
  category: OKRActionCategory
  weekday?: number
  time?: string
  intervalMinutes?: number
  tone: 'amber' | 'blue' | 'violet' | 'emerald'
}

export interface OKRActionFormValue {
  weekday: number
  time: string
  intervalMinutes: number
  enabled: boolean
}

export const WEEKDAYS = [
  { value: 1, label: '周一' },
  { value: 2, label: '周二' },
  { value: 3, label: '周三' },
  { value: 4, label: '周四' },
  { value: 5, label: '周五' },
  { value: 6, label: '周六' },
  { value: 7, label: '周日' },
]

// The product exposes this fixed action catalog. Scope, recipients, output and
// acceptance criteria live in the bound Prompt instead of another form/schema.
export const OKR_ACTIONS: OKRActionDefinition[] = [
  {
    key: 'remind_missing',
    title: '周报催填',
    shortLabel: '催',
    description: '周一定位 Platform Team 周会，会前先逐人私聊催填，再按 O-KR 维度群内汇总提醒。',
    promptKey: 'okr_agent_weekly_reminder',
    cadence: 'weekly',
    category: 'notification',
    weekday: 1,
    time: '09:00',
    tone: 'amber',
  },
  {
    key: 'progress_sync',
    title: '进展自动巡检',
    shortLabel: '巡',
    description: '巡检 Meego 和消息证据，把客观变化写回世界模型。',
    promptKey: 'okr_agent_progress_sync',
    cadence: 'interval',
    category: 'notification',
    intervalMinutes: 360,
    tone: 'blue',
  },
  {
    key: 'meeting_summary',
    title: '会议材料生成',
    shortLabel: '会',
    description: '根据最近两个有效周次生成双周会材料草稿。',
    promptKey: 'okr_agent_report_c',
    cadence: 'weekly',
    category: 'material',
    weekday: 2,
    time: '10:00',
    tone: 'violet',
  },
  {
    key: 'publish_report',
    title: '对外材料提交',
    shortLabel: '交',
    description: '生成中台周报并提交到 Prompt 约定的固定落点。',
    promptKey: 'okr_agent_report_b',
    cadence: 'weekly',
    category: 'material',
    weekday: 2,
    time: '18:00',
    tone: 'emerald',
  },
]

export function actionDefinition(key: OKRActionKey): OKRActionDefinition {
  const definition = OKR_ACTIONS.find((item) => item.key === key)
  if (!definition) throw new Error(`unknown OKR action: ${key}`)
  return definition
}

function contextString(task: ScheduledTask, key: string): string {
  const value = task.context_snapshot?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

export function actionKeyForSchedule(task: ScheduledTask): OKRActionKey | undefined {
  if (task.dispatch_kind !== 'create_task') return undefined
  if (contextString(task, 'skill') !== 'okr-agent-orchestrator') return undefined
  const key = contextString(task, 'action_key')
  return OKR_ACTIONS.some((item) => item.key === key) ? key as OKRActionKey : undefined
}

export function formValueForAction(definition: OKRActionDefinition, task?: ScheduledTask): OKRActionFormValue {
  return {
    weekday: task?.weekday ?? definition.weekday ?? 1,
    time: task?.daily_time ?? definition.time ?? '09:00',
    intervalMinutes: task?.interval_minutes ?? definition.intervalMinutes ?? 360,
    enabled: task?.enabled ?? false,
  }
}

export function scheduledTaskInput(definition: OKRActionDefinition, value: OKRActionFormValue): ScheduledTaskInput {
  const weekly = definition.cadence === 'weekly'
  return {
    title: `OKR · ${definition.title}`,
    action_type: 'agent_task',
    instruction: `执行固定行动“${definition.title}”；范围、对象、产出和验收标准以绑定 Prompt 为准。`,
    context_snapshot: {
      module: 'biz-okr',
      skill: 'okr-agent-orchestrator',
      action_key: definition.key,
      prompt_key: definition.promptKey,
    },
    schedule_type: weekly ? 'weekly' : 'interval',
    daily_time: weekly ? value.time : null,
    weekday: weekly ? value.weekday : null,
    interval_minutes: weekly ? null : value.intervalMinutes,
    run_at: null,
    enabled: value.enabled,
  }
}

export function manualTaskInput(definition: OKRActionDefinition): CreateTaskInput {
  const context = {
    module: 'biz-okr',
    skill: 'okr-agent-orchestrator',
    action_key: definition.key,
    prompt_key: definition.promptKey,
  }
  return {
    title: `OKR · ${definition.title}`,
    action_type: 'agent_task',
    target: `执行固定行动“${definition.title}”；范围、对象、产出和验收标准以绑定 Prompt 为准。`,
    background: context,
    source_payload: {
      instruction: `执行固定行动“${definition.title}”；范围、对象、产出和验收标准以绑定 Prompt 为准。`,
      ...context,
    },
  }
}

export function actionScheduleText(definition: OKRActionDefinition, task?: ScheduledTask): string {
  if (definition.cadence === 'interval') {
    return `每 ${task?.interval_minutes ?? definition.intervalMinutes} 分钟`
  }
  const weekday = task?.weekday ?? definition.weekday ?? 1
  const weekdayLabel = WEEKDAYS.find((item) => item.value === weekday)?.label ?? '每周'
  return `${weekdayLabel} ${task?.daily_time ?? definition.time}`
}
