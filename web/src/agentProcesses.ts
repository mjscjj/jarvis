import type { AgentProcess } from './types'

export const agentModeLabels: Record<AgentProcess['mode'], string> = {
  exec: '执行任务',
  'app-server': '后台服务',
  cli: 'CLI 任务',
  desktop: '桌面端',
}

export const agentSourceMeta: Record<AgentProcess['source'], { label: string; color: string }> = {
  jarvis: { label: 'Jarvis', color: 'green' },
  'cc-connect': { label: 'CC Connect', color: 'blue' },
  paseo: { label: 'Paseo', color: 'cyan' },
  chatgpt: { label: 'ChatGPT', color: 'geekblue' },
  trae: { label: 'Trae', color: 'purple' },
  other: { label: '其他', color: 'default' },
}
