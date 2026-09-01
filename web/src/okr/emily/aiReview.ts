import type { CreateTaskInput, Task } from '../../types'

export const OKR_PREVIEW_REVIEW_PROMPT_KEY = 'okr_agent_preview_review'

export type PreviewReviewTarget =
  | { kind: 'all'; title: string }
  | { kind: 'kr'; objectiveId: string; krId: string; title: string }
  | { kind: 'point'; objectiveId: string; krId: string; pointId: string; title: string }

export function previewReviewKey(target: PreviewReviewTarget): string {
  if (target.kind === 'all') return 'all'
  if (target.kind === 'kr') return `kr:${target.krId}`
  return `point:${target.pointId}`
}

export function previewReviewTaskInput(quarter: string, week: string, target: PreviewReviewTarget): CreateTaskInput {
  if (!quarter.trim()) throw new Error('缺少 OKR 季度，无法发起 AI 评审。')
  if (!week.trim()) throw new Error('缺少 Preview 周次，无法发起 AI 评审。')

  const reviewScope = {
    quarter,
    week,
    ...target,
  }
  const targetLabel = target.kind === 'all' ? `${quarter} / ${week} 全部 OKR` : `“${target.title}”`
  const context = {
    module: 'weekly-report',
    skill: 'okr-agent-orchestrator',
    action_key: 'preview_review',
    prompt_key: OKR_PREVIEW_REVIEW_PROMPT_KEY,
    review_scope: reviewScope,
  }

  return {
    title: `OKR Preview 评审 · ${target.kind === 'all' ? '全部' : target.title}`,
    action_type: 'agent_task',
    target: `按照绑定的 OKR Preview 评分 Prompt 评审${targetLabel}，只输出判断和建议。`,
    background: context,
    source_payload: {
      instruction: `按照绑定的 OKR Preview 评分 Prompt 评审${targetLabel}，只输出判断和建议。`,
      ...context,
    },
  }
}

function textField(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

export function previewReviewContent(task: Task): string | undefined {
  const result = task.execution_result
  const enrichments = Array.isArray(result?.enrichments) ? result.enrichments : []
  const reports = enrichments.flatMap((raw) => {
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return []
    const item = raw as Record<string, unknown>
    const content = textField(item.content)
    if (!content) return []
    const label = textField(item.label)
    return [{ label, content }]
  })
  if (reports.length === 1) return reports[0].content
  if (reports.length > 1) {
    return reports.map(({ label, content }) => label ? `### ${label}\n\n${content}` : content).join('\n\n')
  }
  return task.summary?.trim() || textField(result?.summary)
}
