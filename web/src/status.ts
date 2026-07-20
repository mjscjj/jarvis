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
  processing: '#b45309',
  warning: '#c2620b',
  success: '#16a34a',
  error: '#dc2626',
  leader: '#7c3aed',
} as const

export const todoStatusMeta: Record<TodoStatus, StatusMeta> = {
  extracted: { label: '待评估', color: C.info },
  scoring: { label: '评估中', color: C.processing },
  auto: { label: '自动执行', color: C.success },
  need_info: { label: '待补信息', color: C.warning },
  need_decision: { label: '待决策', color: C.warning },
  confirmed: { label: '已确认', color: C.success },
  dismissed: { label: '已忽略', color: C.default },
  dropped: { label: '已丢弃', color: C.default },
  expired: { label: '已过期', color: C.error },
}

export const taskStatusMeta: Record<TaskStatus, StatusMeta> = {
  pending: { label: '待执行', color: C.info },
  executing: { label: '执行中', color: C.processing },
  done: { label: '已完成', color: C.success },
  failed: { label: '失败', color: C.error },
}

export const actionLabels: Record<ActionType, string> = {
  code_change: '代码修改',
  summary_post: '总结并发群',
  investigate: '查证澄清',
  schedule_meeting: '安排会议',
  reply_message: '回复消息',
  doc_write: '撰写文档',
  manual_followup: '人工跟进',
}

export const leaderColor = C.leader
