import type { Kr, KrPriority, KrTag, Objective } from './types'

export const BUSINESS_CATEGORY_TAG = 'business_category'
export const PRIORITY_TAG = 'priority'

// These are presentation order and labels for the Biz OKR navigation. The
// stored tag remains untouched (notably the spaces around "&" in the last
// value), so adding the navigation never migrates or rewrites OKR data.
const BUSINESS_CATEGORY_NAV = [
  { value: '公会业务', label: '公会业务' },
  { value: '运营效率', label: '运营效率' },
  { value: 'AI提效', label: 'AI提效' },
  { value: '优质主播 & 内容专项', label: '优质主播&内容专项' },
] as const

const BUSINESS_CATEGORY_ORDER = new Map<string, number>(BUSINESS_CATEGORY_NAV.map((item, index) => [item.value, index]))
const BUSINESS_CATEGORY_LABEL = new Map<string, string>(BUSINESS_CATEGORY_NAV.map((item) => [item.value, item.label]))

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
		// A navigation option represents at least one KR. Empty Objectives remain
		// available in the unfiltered management/Plan list, but must not create a
		// zero-count "untagged" tab.
		for (const kr of objective.krs) add(objective, kr)
	}

	return [...businesses.entries()]
		.sort(([left], [right]) => businessCategoryOrder(left) - businessCategoryOrder(right) || left.localeCompare(right))
    .map(([business, priorities]) => ({
      value: business,
      label: businessCategoryLabel(business),
      priorities: [...priorities.entries()]
        .sort(([left], [right]) => priorityOrder(left) - priorityOrder(right))
        .map(([priority, directions]) => ({
          value: priority,
          label: priorityLabel(priority),
          objectives: [...directions.values()],
        })),
    }))
}

export function buildGlobalPriorityNavigation(navigation: BusinessNavigation[]): PriorityNavigation[] {
  const priorities = new Map<KrPriority | '', { label: string; objectives: Map<string, Objective> }>()
  for (const business of navigation) {
    for (const priority of business.priorities) {
      let current = priorities.get(priority.value)
      if (!current) {
        current = { label: priority.label, objectives: new Map() }
        priorities.set(priority.value, current)
      }
      for (const objective of priority.objectives) {
        const existing = current.objectives.get(objective.id)
        if (existing) existing.krs.push(...objective.krs)
        else current.objectives.set(objective.id, { ...objective, krs: [...objective.krs] })
      }
    }
  }
  return [...priorities.entries()]
    .sort(([left], [right]) => priorityOrder(left) - priorityOrder(right))
    .map(([value, priority]) => ({ value, label: priority.label, objectives: [...priority.objectives.values()] }))
}

export function buildAllBusinessNavigation(navigation: BusinessNavigation[]): BusinessNavigation {
	return { value: '__all__', label: '全部 OKR', priorities: buildGlobalPriorityNavigation(navigation) }
}

// Management is also where a missing priority gets assigned, so its navigation
// keeps the complete business vocabulary visible even when one bucket is empty.
// Review keeps using the evidence-derived list and is therefore unchanged.
export function withCompletePriorityNavigation(business?: BusinessNavigation): BusinessNavigation | undefined {
	if (!business) return undefined
	const existing = new Map(business.priorities.map((priority) => [priority.value, priority]))
	const priorities: PriorityNavigation[] = (['p0', 'p1', 'p2'] as const).map((value) =>
		existing.get(value) ?? { value, label: priorityLabel(value), objectives: [] },
	)
	const untagged = existing.get('')
	if (untagged) priorities.push(untagged)
	return { ...business, priorities }
}

export function objectivesForBusiness(business?: BusinessNavigation): Objective[] {
	const objectives = new Map<string, Objective>()
	for (const priority of business?.priorities ?? []) {
		for (const objective of priority.objectives) {
			const existing = objectives.get(objective.id)
			if (existing) existing.krs.push(...objective.krs)
			else objectives.set(objective.id, { ...objective, krs: [...objective.krs] })
		}
	}
	return [...objectives.values()]
}

export function filterObjectivesByHierarchy<T extends Objective>(
	objectives: T[],
	business: string | undefined,
	priority: KrPriority | '' | undefined,
	objectiveId: string,
): T[] {
	const constrained = business !== undefined || priority !== undefined || Boolean(objectiveId)
	return objectives
		.filter((objective) => !objectiveId || objective.id === objectiveId)
		.map((objective) => ({
			...objective,
			krs: objective.krs.filter((kr) =>
				(business === undefined || businessCategoryOf(kr) === business) &&
				(priority === undefined || priorityOf(kr) === priority)),
		} as T))
		.filter((objective) => !constrained || objective.krs.length > 0)
}

export interface BusinessCategoryOption {
  value: string
  label: string
  count: number
}

export function businessCategoryLabel(value: string): string {
	return BUSINESS_CATEGORY_LABEL.get(value) ?? (value || '未标注业务')
}

function businessCategoryOrder(value: string): number {
	if (!value) return Number.MAX_SAFE_INTEGER
	return BUSINESS_CATEGORY_ORDER.get(value) ?? BUSINESS_CATEGORY_NAV.length
}

// Another filter can empty the category someone is standing in. Keeping its tab
// visible at zero is what tells them why the list below went blank, instead of
// silently dropping the tab or moving them to a category they did not pick.
export function withSelectedBusinessCategory(options: BusinessCategoryOption[], selected?: string): BusinessCategoryOption[] {
  if (selected === undefined || options.some((option) => option.value === selected)) return options
  return [...options, { value: selected, label: businessCategoryLabel(selected), count: 0 }]
}

// The business-category strip is the one navigation level every OKR page shares,
// so its options and ordering come from the same hierarchy the fill and meeting
// pages drill through, not from a second pass over the tags.
export function businessCategoryOptions(navigation: BusinessNavigation[]): BusinessCategoryOption[] {
  return navigation.map((business) => ({ value: business.value, label: business.label, count: hierarchyKRCount(business) }))
}

export function hierarchyKRCount(business: BusinessNavigation): number {
  return business.priorities.reduce((sum, priority) => sum + priority.objectives.reduce((subtotal, objective) => subtotal + objective.krs.length, 0), 0)
}

export function priorityKRCount(priority: PriorityNavigation): number {
  return priority.objectives.reduce((sum, objective) => sum + objective.krs.length, 0)
}
