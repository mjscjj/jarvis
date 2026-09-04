import type { Task, TaskQuestion } from '../types'

export type FailureKind = 'codex' | 'manual' | 'interrupted' | 'stale' | 'unknown'

// questionOf reads the question a parked Task is waiting on. The model writes
// it freely, so only the title is required to render anything at all.
export function questionOf(task: Task): TaskQuestion | null {
  const question = task.execution_result?.question
  if (!question || typeof question !== 'object' || Array.isArray(question)) return null
  const typed = question as TaskQuestion
  return typeof typed.title === 'string' && typed.title.trim() ? typed : null
}

// questionText flattens a question into the prose shown outside the card.
export function questionText(task: Task): string | null {
  const question = questionOf(task)
  if (!question) return null
  const body = question.body?.trim()
  return body ? `${question.title.trim()}\n\n${body}` : question.title.trim()
}

export function strField(obj: Record<string, unknown> | null, key: string): string | null {
  if (!obj) return null
  const value = obj[key]
  return typeof value === 'string' && value.trim() ? value : null
}

export function failureKindOf(task: Task): FailureKind | null {
  if (task.status !== 'failed') return null
  const stage = strField(task.execution_result, 'stage')
  switch (stage) {
    case 'manual_failed': return 'manual'
    case 'interrupted': return 'interrupted'
    case 'stale': return 'stale'
    case 'executed': return 'codex'
    default: return 'unknown'
  }
}

export const failureMeta: Record<FailureKind, { label: string; color: string }> = {
  codex: { label: '执行失败（系统）', color: 'red' },
  manual: { label: '你标记失败', color: 'volcano' },
  interrupted: { label: '你已打断', color: 'orange' },
  stale: { label: '超时中断', color: 'orange' },
  unknown: { label: '失败', color: 'red' },
}

export function taskHandlerMeta(task: Task): { label: string; detail: string; color: string } | null {
  const actor = task.resolution?.actor_type
  if (!actor) return null
  if (actor === 'user') return { label: '人工处理', detail: '最终状态由你手动确认', color: 'gold' }
  if (actor === 'proactive') return { label: '模型关闭', detail: '主动 Agent 核验后收口', color: 'purple' }
  if (actor === 'm5') return { label: '模型处理', detail: 'M5 Agent 执行或核验后收口', color: 'blue' }
  return { label: '系统处理', detail: `最终处理者：${actor}`, color: 'default' }
}

export function modelCloseReason(task: Task): string | null {
  if (task.resolution?.actor_type !== 'proactive' || task.resolution.event_type !== 'closed') return null
  return task.summary?.trim() || strField(task.execution_result, 'summary')
}

function objectField(value: Record<string, unknown>, key: string): Record<string, unknown> | null {
  const field = value[key]
  return field && typeof field === 'object' && !Array.isArray(field)
    ? field as Record<string, unknown>
    : null
}

function textValue(value: unknown): string | null {
  return typeof value === 'string' && value.trim() ? value.trim() : null
}

export function taskProjectName(task: Task): string {
  const project = objectField(task.background, 'project')
  return textValue(project?.name) || (task.project_id != null ? `项目 #${task.project_id}` : '未关联项目')
}

export function taskSourceName(task: Task): string {
  const group = objectField(task.background, 'group')
  const assigner = objectField(task.background, 'assigner')
  const groupName = textValue(group?.name)
  const assignerName = textValue(assigner?.name)
  if (groupName && assignerName) return `${groupName} · ${assignerName}`
  if (groupName || assignerName) return groupName || assignerName || ''
  if (task.todo_id != null) return `线索 #${task.todo_id}`
  return `${task.source_type} #${task.source_id ?? '—'}`
}

export function taskConclusion(task: Task): string {
  const result = task.execution_result
  const summary = task.summary?.trim() || strField(result, 'summary')
  const question = questionOf(task)?.title.trim()
  const error = strField(result, 'error')

  if (task.status === 'needs_human') {
    return question || summary || 'Agent 正在等待你的回复。'
  }
  if (task.status === 'waiting') {
    const waiting = result?.waiting && typeof result.waiting === 'object'
      ? result.waiting as Record<string, unknown>
      : null
    const reason = textValue(waiting?.reason)
    const wakeAt = textValue(waiting?.wake_at)
    return [summary || reason || '正在等待外部条件', wakeAt ? `预计 ${wakeAt} 恢复` : null]
      .filter(Boolean)
      .join(' · ')
  }
  if (task.status === 'executing') return summary || 'Agent 正在执行，并会持续更新结果。'
  if (task.status === 'pending') return summary || task.target || '任务已经就绪，等待开始执行。'
  if (task.status === 'observing') return summary || '已经完成调查，当前无需采取行动。'
  if (task.status === 'failed') return error || summary || '任务未完成，打开查看失败原因。'
  return summary || '任务已完成。'
}

export function taskConclusionLabel(task: Task): string {
  switch (task.status) {
    case 'needs_human': return '需要你回复'
    case 'pending': return '下一步'
    case 'executing': return '当前进展'
    case 'waiting': return '等待原因'
    case 'done': return '完成结果'
    case 'observing': return '调查结论'
    case 'failed': return '异常原因'
  }
}
