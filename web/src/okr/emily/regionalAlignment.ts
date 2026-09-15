import { priorityOf } from './hierarchy.ts'
import type { Kr, KrPriority, RegionalCode, RegionalPlanDecisionItem } from './types.ts'

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
