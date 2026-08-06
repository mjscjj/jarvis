import type { ProposalResult, Task } from '../types'

export type FailureKind = 'codex' | 'manual' | 'rejected' | 'interrupted' | 'stale' | 'unknown'

export function proposalOf(task: Task): ProposalResult | null {
  if (task.status !== 'awaiting_approval') return null
  const result = task.execution_result as ProposalResult | null
  if (result && result.stage === 'proposal' && result.proposal) return result
  return null
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
    case 'rejected': return 'rejected'
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
  rejected: { label: '你已驳回', color: 'gold' },
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
  const proposal = proposalOf(task)
  const summary = task.summary?.trim() || strField(result, 'summary')
  const followup = strField(result, 'needs_followup')
  const error = strField(result, 'error')
  const rejectReason = strField(result, 'reject_reason')

  if (task.status === 'awaiting_approval') {
    return proposal?.needs_followup?.trim()
      || proposal?.summary?.trim()
      || proposal?.proposal.action
      || '方案已经准备好，等待你决定是否落地。'
  }
  if (task.status === 'needs_human') {
    return followup || summary || 'Agent 正在等待你的回复。'
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
  if (task.status === 'failed') return rejectReason || error || summary || '任务未完成，打开查看失败原因。'
  return summary || followup || '任务已完成。'
}

export function taskConclusionLabel(task: Task): string {
  switch (task.status) {
    case 'awaiting_approval': return '需要你决定'
    case 'needs_human': return '需要你回复'
    case 'pending': return '下一步'
    case 'executing': return '当前进展'
    case 'waiting': return '等待原因'
    case 'done': return '完成结果'
    case 'observing': return '调查结论'
    case 'failed': return '异常原因'
  }
}

export function proposalArtifactLabel(task: Task): string {
  const action = proposalOf(task)?.proposal.action.toLowerCase() || ''
  if (/code|代码|仓库|分支|测试|构建/.test(`${task.action_type.toLowerCase()} ${action}`)) return '交付物'
  if (/message|消息|通知|回复|发送|群/.test(action)) return '拟发送内容'
  if (/mail|邮件/.test(action)) return '拟发送邮件'
  if (/doc|文档|写入|更新|修改/.test(action)) return '拟写入内容'
  return '拟落地产物'
}

export interface StructuredProposalAction {
  introduction: string
  steps: string[]
}

// Proposal action remains free-form model text. This helper only adds a visual
// projection when the text already contains multiple numbered steps; it does
// not require or rewrite the stored payload.
export function structureProposalAction(value: string): StructuredProposalAction {
  const text = value.trim()
  const markers = [...text.matchAll(/(^|[\s：:；;。])(\d{1,2})[.)、]\s*/g)]
  if (markers.length < 2) return { introduction: text, steps: [] }

  const first = markers[0]
  const introduction = text.slice(0, first.index).trim().replace(/[：:；;。]+$/, '')
  const steps = markers.map((marker, index) => {
    const start = (marker.index ?? 0) + marker[0].length
    const end = index + 1 < markers.length ? markers[index + 1].index : text.length
    return text.slice(start, end).trim().replace(/[。；;]+$/, '')
  }).filter(Boolean)

  if (steps.length < 2) return { introduction: text, steps: [] }
  return { introduction, steps }
}
