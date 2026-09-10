import type { SetupStatus } from './types.ts'

// Only missing human actions are shown; these are not wizard steps.
export function setupAction(status: SetupStatus): 'connect' | 'repair' | 'authorize' | 'agent' | 'start' {
  if (!status.lark.app_id) return 'connect'
  if (status.lark.bot.status !== 'ready' || !status.lark.bot.verified) return 'repair'
  if (status.lark.user.status !== 'ready' || !status.lark.user.verified) return 'authorize'
  if (!status.agent.authenticated) return 'agent'
  return 'start'
}

export function setupTaskStopped(status: string): boolean {
  return ['done', 'failed', 'observing', 'needs_human'].includes(status)
}
