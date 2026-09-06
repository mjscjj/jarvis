import dayjs from 'dayjs'
import type { Task, TaskStatus } from '../types'

export const workbenchOverviewStatuses = {
  needsDecision: ['needs_human'],
  inProgress: ['pending', 'executing', 'waiting'],
  completed: ['done'],
  risk: ['failed'],
} satisfies Record<string, TaskStatus[]>

export function taskUpdatedOn(task: Task, date: string): boolean {
  return dayjs(task.updated_at).format('YYYY-MM-DD') === date
}

export function taskOneLine(task: Task): string {
  let detail = task.summary
  if (task.status === 'needs_human') {
    const question = task.execution_result?.question
    if (question && typeof question === 'object' && !Array.isArray(question)) {
      const { title, body } = question as Record<string, unknown>
      if (typeof title === 'string' && title.trim()) {
        detail = typeof body === 'string' && body.trim() ? `${title.trim()} ${body.trim()}` : title.trim()
      }
    }
  } else if (!detail) {
    const summary = task.execution_result?.summary
    if (typeof summary === 'string' && summary.trim()) detail = summary.trim()
  }
  return (detail || '暂无补充说明').replace(/\s+/g, ' ').trim()
}
