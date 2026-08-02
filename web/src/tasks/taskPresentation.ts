import type { ProposalResult, Task } from '../types'

export type FailureKind = 'codex' | 'manual' | 'rejected' | 'interrupted' | 'stale' | 'unknown'

export function proposalOf(task: Task): ProposalResult | null {
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
