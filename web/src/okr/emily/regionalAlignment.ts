import { priorityOf } from './hierarchy.ts'
import type { CommentDocumentOrder } from './comments.ts'
import type { Kr, KrPriority, RegionalAlignmentBoard, RegionalCode, RegionalPlanDecisionItem } from './types.ts'

export type RegionalPriorityFilter = 'all' | KrPriority

export const REGIONAL_PRIORITY_FILTERS: Array<{ value: RegionalPriorityFilter; label: string }> = [
  { value: 'all', label: 'Focus item/P1/P2' },
  { value: 'p0', label: '焦点项 / Focus item' },
  { value: 'p1', label: 'P1' },
  { value: 'p2', label: 'P2' },
]

export function matchesRegionalPriority(kr: Kr, priority: RegionalPriorityFilter): boolean {
  return priority === 'all' || priorityOf(kr) === priority
}

export function launchRegionOptions(region: RegionalCode): string[] {
  switch (region) {
    case 'menat': return ['MENAT', 'MENA', 'TR']
    case 'eu': return ['EU', 'EU-A', 'EU-B']
    case 'sea-cca': return ['SEA&CCA', 'SEA', 'CCA']
    case 'nea': return ['NEA']
    case 'ams-anz': return ['AMS&ANZ', 'AMS', 'ANZ']
  }
}

export function regionalAlignmentShareURL(currentURL: string, publicBaseURL: string, quarter: string, region: RegionalCode): string {
  const url = new URL(publicBaseURL.trim() || currentURL)
  url.hash = `/biz-okr?${new URLSearchParams({ tab: 'regional-alignment', quarter, region })}`
  return url.toString()
}

export async function copyRegionalAlignmentShareLink(link: string, clipboard?: { writeText(value: string): Promise<void> }): Promise<boolean> {
  if (!clipboard?.writeText) return false
  await clipboard.writeText(link)
  return true
}

export function regionalDecisionSignature(value: RegionalPlanDecisionItem): string {
  return JSON.stringify({
    onboard: value.onboard,
    launchRegions: value.launchRegions,
    regionalPocs: value.regionalPocs,
    regionalOkr: value.regionalOkr,
    hidden: value.hidden,
  })
}

/** The regional comment drawer follows the complete page, not active filters. */
export function buildRegionalCommentDocumentOrder(board: RegionalAlignmentBoard): CommentDocumentOrder {
  const order = new Map<string, number>()
  let position = 1
  const add = (type: string, id: string) => {
    const key = `${type}:${id}`
    if (!order.has(key)) order.set(key, position++)
  }
  const addOKR = (objectives: RegionalAlignmentBoard['plan']['objectives']) => {
    for (const objective of objectives) {
      add('objective', objective.id)
      for (const kr of objective.krs) {
        add('kr', kr.id)
        add('alignment_item', `kr:${kr.id}:owners`)
        for (const point of kr.points) {
          add('point', point.id)
          add('alignment_item', `point:${point.id}:owners`)
          for (const entry of [...point.entries, ...(point.previousEntries ?? [])]) add('entry', entry.id)
        }
      }
    }
  }

  add('alignment_item', 'section:part0')
  add('alignment_item', 'section:demands')
  add('alignment_item', 'section:demands:guide')
  for (const demand of [...board.demands].sort((left, right) => left.sortOrder - right.sortOrder)) {
    add('alignment_item', `demand:${demand.id}`)
    for (const field of ['regional_okr', 'requirement', 'assets', 'priority', 'regional_poc', 'acceptance', 'platform_poc', 'plan_kr', 'deliverable']) add('alignment_item', `d:${demand.id}:${field}`)
  }
  add('alignment_item', 'section:platform')
  addOKR(board.plan.objectives)
  for (const objective of board.plan.objectives) for (const kr of objective.krs) {
    add('alignment_item', `plan:${kr.id}`)
    for (const field of ['onboard', 'launch_regions', 'regional_poc', 'regional_okr']) add('alignment_item', `pd:${kr.id}:${field}`)
  }
  add('alignment_item', 'section:part1')
  add('alignment_item', 'section:part2')
  addOKR(board.recap.objectives)
  return order
}
