import type { CreateTaskInput, Delegation } from '../types'

export function progressText(content: unknown): string {
  if (typeof content === 'string') return content
  if (content && typeof content === 'object' && !Array.isArray(content)) {
    const summary = (content as Record<string, unknown>).summary
    if (typeof summary === 'string') return summary
  }
  return ''
}

export function updatedProgress(content: unknown, summary: string): unknown {
  return content && typeof content === 'object' && !Array.isArray(content)
    ? { ...content, summary }
    : { summary, ...(content != null ? { detail: content } : {}) }
}

export function checkInput(item: Delegation): CreateTaskInput {
  return {
    title: `核验交办：${item.title}`, action_type: 'investigate',
    target: `核验交办 #${item.id} 的当前进展并写回；本次检查完成即可收口，不代替负责人交付。`,
    background: {},
    source_payload: {
      delegation_id: item.id,
      why_now: '用户要求现在核验',
      original_context: item.source_payload,
      current_progress: item.content ?? null,
    },
  }
}
