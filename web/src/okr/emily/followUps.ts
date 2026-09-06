import type { FollowUpItem, FollowUpStatus } from './types'

export const FOLLOW_UP_STATUS_OPTIONS: ReadonlyArray<{ value: FollowUpStatus; label: string }> = [
  { value: 'not_started', label: '未开始' },
  { value: 'in_progress', label: '进行中' },
  { value: 'done', label: '已完成' },
  { value: 'abandoned', label: '废弃' },
]

export function isClosedFollowUp(status: FollowUpStatus) {
  return status === 'done' || status === 'abandoned'
}

export function canEditFollowUpStatus(readOnly: boolean, statusEditable: boolean) {
  return !readOnly || statusEditable
}

type SortableFollowUp = Pick<FollowUpItem, 'id' | 'assignDate' | 'sortOrder' | 'createdAt'>

export function compareFollowUpsByAssignDate(left: SortableFollowUp, right: SortableFollowUp) {
  if (!left.assignDate && right.assignDate) return 1
  if (left.assignDate && !right.assignDate) return -1
  const dateOrder = left.assignDate.localeCompare(right.assignDate)
  if (dateOrder !== 0) return dateOrder
  const explicitOrder = left.sortOrder - right.sortOrder
  if (explicitOrder !== 0) return explicitOrder
  const createdOrder = left.createdAt.localeCompare(right.createdAt)
  if (createdOrder !== 0) return createdOrder
  return left.id.localeCompare(right.id)
}

export function sortFollowUpsByAssignDate<T extends SortableFollowUp>(items: readonly T[]) {
  return [...items].sort(compareFollowUpsByAssignDate)
}
