import type { SetupStatus, Task } from './types.ts'

// Only missing human actions are shown; these are not wizard steps.
export function setupAction(status: SetupStatus): 'connect' | 'application' | 'authorize' | 'agent' | 'start' {
  if (!status.lark.app_id) return 'connect'
  if (status.lark.bot.status !== 'ready' || !status.lark.bot.verified ||
      status.lark.application_checks.some(check => !check.ready)) return 'application'
  if (status.lark.user.status !== 'ready' || !status.lark.user.verified) return 'authorize'
  if (!status.agent.authenticated) return 'agent'
  return 'start'
}

export function setupSecretVisible(status: SetupStatus, editing: boolean): boolean {
  return Boolean(status.lark.app_id) && (editing || (setupAction(status) === 'start' && !status.lark.credential_available))
}

export function setupCanEnter(status: SetupStatus, restartFrom: string | null): boolean {
  return status.app_ready && (!restartFrom || status.runtime_id !== restartFrom)
}

export function worldModelProgress(task: Task): { title: string; detail: string } {
  const titles: Record<string, string> = {
    pending: '工作背景初始化：等待调度',
    executing: '工作背景初始化：后台执行中',
    waiting: '工作背景初始化：等待条件',
    needs_human: '工作背景初始化：需要你的补充',
    failed: '工作背景初始化未完成',
    observing: '工作背景初始化已暂停',
    done: '初始工作背景已建立',
  }
  const text = (value: unknown) => typeof value === 'string' ? value.trim() : ''
  const result = task.execution_result
  const summary = text(task.summary) || text(result?.summary)
  let details = [summary]
  if (task.status === 'waiting') {
    const waiting = result?.waiting as { reason?: unknown; wake_at?: unknown } | undefined
    details.push(text(waiting?.reason), text(waiting?.wake_at) ? `预计恢复：${text(waiting?.wake_at)}` : '')
  } else if (task.status === 'failed') {
    details = [text(result?.error), text(result?.failure_reason), summary]
  } else if (task.status === 'needs_human') {
    const question = result?.question as { title?: unknown; body?: unknown } | undefined
    details = [text(question?.title), text(question?.body), summary]
  }
  const detail = [...new Set(details.filter(Boolean))].join('\n\n')
    || (task.status === 'done' ? '本次任务未提供结果摘要，可查看初始化任务的运行记录。' : '尚无新的进展摘要，可查看任务状态与运行记录。')
  return { title: titles[task.status] || '工作背景初始化', detail }
}
