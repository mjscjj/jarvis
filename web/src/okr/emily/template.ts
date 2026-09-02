/**
 * 状态是唯一被约束的字段。
 *
 * 现状：同一张表里混用了七种写法 —— (In progress) /【进行中】/【已完成】/
 * (Done) / Done /【已上线】/【未开始】。所以这里收成封闭枚举，只能从下拉框选。
 */

import type { Light, Status } from './types'

export interface StatusOption {
  value: Status
  label: string
  tone: 'blue' | 'green' | 'slate' | 'red' | 'amber'
}

export const STATUSES: StatusOption[] = [
  { value: 'in_progress', label: '进行中', tone: 'blue' },
  { value: 'done', label: '已完成', tone: 'green' },
  { value: 'not_started', label: '未开始', tone: 'slate' },
  { value: 'at_risk', label: '有风险', tone: 'red' },
  { value: 'delayed', label: 'Delay', tone: 'amber' },
  { value: 'blocked', label: '阻塞', tone: 'red' },
]

/** 存量文档里的旧写法 → 统一枚举，导入历史周报时用 */
export const LEGACY_STATUS_MAP: Record<string, Status> = {
  'in progress': 'in_progress',
  进行中: 'in_progress',
  done: 'done',
  已完成: 'done',
  已上线: 'done',
  未开始: 'not_started',
  有风险: 'at_risk',
  delay: 'delayed',
  阻塞: 'blocked',
}

export function statusOf(value: Status) {
  return STATUSES.find((s) => s.value === value)
}

/** 选了「已完成」就归到「已完成」列 */
export function isDone(status: Status) {
  return status === 'done'
}

export const TONE_CLASS: Record<StatusOption['tone'], string> = {
  blue: 'bg-blue-50 text-blue-700',
  green: 'bg-emerald-50 text-emerald-700',
  slate: 'bg-slate-100 text-slate-600',
  red: 'bg-red-50 text-red-700',
  amber: 'bg-amber-50 text-amber-700',
}

export const TONE_TEXT_CLASS: Record<StatusOption['tone'], string> = {
  blue: 'text-blue-600',
  green: 'text-emerald-600',
  slate: 'text-slate-500',
  red: 'text-red-600',
  amber: 'text-amber-600',
}

export const DOT_CLASS: Record<StatusOption['tone'], string> = {
  blue: 'bg-blue-500',
  green: 'bg-emerald-500',
  slate: 'bg-slate-400',
  red: 'bg-red-500',
  amber: 'bg-amber-500',
}

export const KIND_LABEL = {
  strategy: '策略具体 KR',
  product: '产品具体 KR',
} as const

export const METRIC_GROUP_LABEL = '核心数据'

// 红黄绿灯已从界面移除，这里只剩后端 EnumValues 的取值镜像，用于枚举兜底。
export const LIGHTS: { value: Light; label: string; className: string }[] = [
  { value: 'green', label: '绿灯', className: 'bg-emerald-500' },
  { value: 'yellow', label: '黄灯', className: 'bg-amber-400' },
  { value: 'red', label: '红灯', className: 'bg-red-500' },
]
