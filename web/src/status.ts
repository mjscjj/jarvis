// 全站统一的状态元数据与行动类型文案。
// 颜色用低饱和自定义色（对齐 tokens.css 的 --status-*），避免 antd 预设色名
// 在不同页面各用一套导致「同一状态几种蓝」。所有 <Tag color={...}> / StatusBadge
// 都从这里取色，改一处即全站生效。
import type { ActionType, TaskStatus, TodoStatus } from './types'

export interface StatusMeta {
  label: string
  color: string
}

// 语义色板（与 tokens.css --status-* 对齐；Tag 需要具体色值，故在此内联）。
const C = {
  default: '#6b7280',
  info: '#3b6fd4',
  processing: '#287a4b',
  waiting: '#5666a6',
  warning: '#b66a1b',
  success: '#287a4b',
  error: '#dc2626',
  leader: '#7c3aed',
} as const

export const todoStatusMeta: Record<TodoStatus, StatusMeta> = {
  extracted: { label: '待生成任务', color: C.info },
  observing: { label: '观察中', color: C.default },
  materialized: { label: '已生成任务', color: C.success },
}

export const taskStatusMeta: Record<TaskStatus, StatusMeta> = {
  pending: { label: '待执行', color: C.info },
  executing: { label: 'Jarvis 执行中', color: C.processing },
  waiting: { label: '等待外部', color: C.waiting },
  needs_human: { label: '待我回复', color: C.warning },
  awaiting_approval: { label: '待我审批', color: C.warning },
  done: { label: '已交付', color: C.success },
  failed: { label: '失败', color: C.error },
  observing: { label: '无需动手', color: C.default },
}

export const actionLabels: Record<ActionType, string> = {
  agent_task: '通用任务',
  code_change: '代码修改',
  summary_post: '总结并发群',
  investigate: '查证澄清',
  schedule_meeting: '安排会议',
  reply_message: '回复消息',
  doc_write: '撰写文档',
  manual_followup: '人工跟进',
}

export const leaderColor = C.leader
