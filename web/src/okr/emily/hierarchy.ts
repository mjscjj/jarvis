import type { Kr, KrPriority, KrTag, Objective } from './types'

export const BUSINESS_CATEGORY_TAG = 'business_category'
export const PRIORITY_TAG = 'priority'

export interface PriorityNavigation {
  value: KrPriority | ''
  label: string
  objectives: Objective[]
}

export interface BusinessNavigation {
  value: string
  label: string
  priorities: PriorityNavigation[]
}

function singleTagValue(kr: Kr, type: string): string {
  return kr.tags?.find((tag) => tag.type === type)?.value ?? ''
}

export function businessCategoryOf(kr: Kr): string {
  return singleTagValue(kr, BUSINESS_CATEGORY_TAG)
}

export function priorityOf(kr: Kr): KrPriority | '' {
  const value = singleTagValue(kr, PRIORITY_TAG)
  return value === 'p0' || value === 'p1' || value === 'p2' ? value : ''
}

export function priorityLabel(value: KrPriority | ''): string {
  if (value === 'p0') return 'Focus'
  if (value === 'p1') return 'P1'
  if (value === 'p2') return 'P2'
  return '未标注'
}

export function replaceSingleTag(tags: KrTag[] | undefined, type: string, value: string): KrTag[] {
  const next = (tags ?? []).filter((tag) => tag.type !== type)
  const clean = value.trim()
  if (clean) next.push({ type, value: clean })
  return next
}

export function isStructuralTag(tag: KrTag): boolean {
  return tag.type === BUSINESS_CATEGORY_TAG || tag.type === PRIORITY_TAG
}

function priorityOrder(value: KrPriority | ''): number {
  if (value === 'p0') return 0
  if (value === 'p1') return 1
  if (value === 'p2') return 2
  return 3
}

export function buildKRHierarchy(objectives: Objective[]): BusinessNavigation[] {
  const businesses = new Map<string, Map<KrPriority | '', Map<string, Objective>>>()

  const add = (objective: Objective, kr?: Kr) => {
    const business = kr ? businessCategoryOf(kr) : ''
    const priority = kr ? priorityOf(kr) : ''
    let priorities = businesses.get(business)
    if (!priorities) {
      priorities = new Map()
      businesses.set(business, priorities)
    }
    let directions = priorities.get(priority)
    if (!directions) {
      directions = new Map()
      priorities.set(priority, directions)
    }
    let direction = directions.get(objective.id)
    if (!direction) {
      direction = { ...objective, krs: [] }
      directions.set(objective.id, direction)
    }
    if (kr) direction.krs.push(kr)
  }

  for (const objective of objectives) {
    if (objective.krs.length === 0) add(objective)
    else for (const kr of objective.krs) add(objective, kr)
  }

  return [...businesses.entries()]
    .sort(([left], [right]) => left === '' ? 1 : right === '' ? -1 : 0)
    .map(([business, priorities]) => ({
      value: business,
      label: business || '未标注业务',
      priorities: [...priorities.entries()]
        .sort(([left], [right]) => priorityOrder(left) - priorityOrder(right))
        .map(([priority, directions]) => ({
          value: priority,
          label: priorityLabel(priority),
          objectives: [...directions.values()],
        })),
    }))
}

export function hierarchyKRCount(business: BusinessNavigation): number {
  return business.priorities.reduce((sum, priority) => sum + priority.objectives.reduce((subtotal, objective) => subtotal + objective.krs.length, 0), 0)
}

export function priorityKRCount(priority: PriorityNavigation): number {
  return priority.objectives.reduce((sum, objective) => sum + objective.krs.length, 0)
}
