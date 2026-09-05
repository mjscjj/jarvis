import type { FollowUpItem } from './types'

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
