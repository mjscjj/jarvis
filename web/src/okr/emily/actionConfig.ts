import type { ScheduledTask, ScheduledTaskInput } from '../../types'

export type OKRActionKey = 'remind_missing' | 'progress_sync' | 'meeting_summary' | 'publish_report'

export interface OKRActionDefinition {
  key: OKRActionKey
  title: string
  shortLabel: string
  description: string
  skill: string
  cadence: 'weekly' | 'interval'
  weekday?: number
  time?: string
  intervalMinutes?: number
  scope: string
  recipientRule: string
  approval: 'automatic' | 'draft_only' | 'review_then_send' | 'require_approval'
  effect: 'read_only' | 'internal_write' | 'external_write'
  defaultEnabled: boolean
  instruction: string
}

export interface OKRActionFormValue {
  weekday: number
  time: string
  intervalMinutes: number
  target: string
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

export const OKR_ACTIONS: OKRActionDefinition[] = [
  {
    key: 'remind_missing',
    title: '周报催填',
    shortLabel: '催',
    description: '检查当周未填写的 KR，只提醒真实负责人并保留逐人回执。',
    skill: 'weekly-report-reminder',
    cadence: 'weekly',
    weekday: 1,
    time: '09:00',
    scope: '当前季度 · 当前周 · 未填写 KR',
    recipientRule: '缺失项负责人',
    approval: 'review_then_send',
    effect: 'external_write',
    defaultEnabled: false,
    instruction: '读取 weekly-report-reminder Skill；按 action_config 检查本周未填写 KR，生成催填批次；仅在审批策略允许且目标有真实 open_id 时发送，并输出逐人回执。',
  },
  {
    key: 'progress_sync',
    title: '进展自动巡检',
    shortLabel: '巡',
    description: '只读检查 Meego 和已采集消息，把有证据的变化写入世界模型。',
    skill: 'weekly-report-progress-sync',
    cadence: 'interval',
    intervalMinutes: 360,
    scope: '当前季度 · 未闭环 KR',
    recipientRule: '不发送消息',
    approval: 'automatic',
    effect: 'internal_write',
    defaultEnabled: false,
    instruction: '读取 weekly-report-progress-sync Skill；只读 Meego 和已采集消息，更新 Page/Fact；不发送消息。输出扫描范围、证据数、更新数、跳过原因与覆盖缺口。',
  },
  {
    key: 'meeting_summary',
    title: '会议材料生成',
    shortLabel: '会',
    description: '从本周 KR 事实生成会议摘要草稿，保留风险、变化和来源。',
    skill: 'weekly-report-materials',
    cadence: 'weekly',
    weekday: 2,
    time: '10:00',
    scope: '当前季度 · 当前周 · 全部 KR',
    recipientRule: '生成内部草稿，不外发',
    approval: 'draft_only',
    effect: 'internal_write',
    defaultEnabled: false,
    instruction: '读取 weekly-report-materials Skill；执行 meeting_summary，只生成本周会议材料草稿，不创建外部文档、不发送消息。输出风险、变化、缺失和草稿定位信息。',
  },
  {
    key: 'publish_report',
    title: '对外材料提交',
    shortLabel: '交',
    description: '基于已保存草稿创建飞书文档，并按配置提交到目标位置。',
    skill: 'weekly-report-materials',
    cadence: 'weekly',
    weekday: 2,
    time: '18:00',
    scope: '当前季度 · 当前周 · 已保存草稿',
    recipientRule: '配置的飞书目标',
    approval: 'require_approval',
    effect: 'external_write',
    defaultEnabled: false,
    instruction: '读取 weekly-report-materials Skill；执行 publish_report。只能使用已保存草稿；创建飞书文档或发送消息前必须进入审批，目标未配置时标记 needs_human，不能自行猜测。',
  },
]

export function actionDefinition(key: OKRActionKey): OKRActionDefinition {
  const definition = OKR_ACTIONS.find((item) => item.key === key)
  if (!definition) throw new Error(`unknown OKR action: ${key}`)
  return definition
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function numberValue(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function actionConfig(snapshot: Record<string, unknown>): Record<string, unknown> {
  const value = snapshot.action_config
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

export function actionKeyForSchedule(task: ScheduledTask): OKRActionKey | undefined {
  const workflow = stringValue(task.context_snapshot.workflow)
  if (OKR_ACTIONS.some((item) => item.key === workflow)) return workflow as OKRActionKey
  const skill = stringValue(task.context_snapshot.skill)
  if (skill === 'weekly-report-reminder') return 'remind_missing'
  if (skill === 'weekly-report-progress-sync') return 'progress_sync'
  if (skill === 'weekly-report-materials') {
    const mode = stringValue(actionConfig(task.context_snapshot).mode)
    if (mode === 'meeting_summary' || mode === 'publish_report') return mode
  }
  return undefined
}

export function formValueForAction(definition: OKRActionDefinition, task?: ScheduledTask): OKRActionFormValue {
  const config = task ? actionConfig(task.context_snapshot) : {}
  return {
    weekday: numberValue(config.weekday) ?? definition.weekday ?? 1,
    time: task?.daily_time || definition.time || '09:00',
    intervalMinutes: task?.interval_minutes || definition.intervalMinutes || 360,
    target: stringValue(config.target),
    enabled: task?.enabled ?? definition.defaultEnabled,
  }
}

export function scheduledTaskInput(definition: OKRActionDefinition, value: OKRActionFormValue): ScheduledTaskInput {
  const weekly = definition.cadence === 'weekly'
  return {
    title: `OKR · ${definition.title}`,
    action_type: 'agent_task',
    instruction: definition.instruction,
    context_snapshot: {
      module: 'weekly-report',
      skill: definition.skill,
      workflow: definition.key,
      action_config: {
        mode: definition.key,
        timezone: 'Asia/Shanghai',
        weekday: weekly ? value.weekday : null,
        scope: 'current_week',
        recipient_rule: definition.recipientRule,
        target: value.target.trim(),
        approval: definition.approval,
      },
    },
    schedule_type: weekly ? 'daily' : 'interval',
    daily_time: weekly ? value.time : null,
    interval_minutes: weekly ? null : value.intervalMinutes,
    run_at: null,
    enabled: value.enabled,
  }
}

export function actionScheduleText(definition: OKRActionDefinition, task?: ScheduledTask): string {
  if (!task) return definition.cadence === 'weekly'
    ? `建议 ${WEEKDAYS.find((item) => item.value === definition.weekday)?.label || '每周'} ${definition.time}`
    : `建议每 ${definition.intervalMinutes} 分钟`
  if (definition.cadence === 'weekly') {
    const config = actionConfig(task.context_snapshot)
    const weekday = numberValue(config.weekday) ?? definition.weekday ?? 1
    return `${WEEKDAYS.find((item) => item.value === weekday)?.label || '每周'} ${task.daily_time || definition.time}`
  }
  return `每 ${task.interval_minutes || definition.intervalMinutes} 分钟`
}

export function nextActionRunAt(definition: OKRActionDefinition, task: ScheduledTask): string {
  if (definition.cadence !== 'weekly') return task.next_run_at
  const config = actionConfig(task.context_snapshot)
  const targetWeekday = numberValue(config.weekday) ?? definition.weekday ?? 1
  const next = new Date(task.next_run_at)
  const weekdayName = new Intl.DateTimeFormat('en-US', { weekday: 'short', timeZone: 'Asia/Shanghai' }).format(next)
  const weekdayNumber = ({ Mon: 1, Tue: 2, Wed: 3, Thu: 4, Fri: 5, Sat: 6, Sun: 7 } as Record<string, number>)[weekdayName] || 1
  const dayDelta = (targetWeekday - weekdayNumber + 7) % 7
  next.setUTCDate(next.getUTCDate() + dayDelta)
  return next.toISOString()
}
