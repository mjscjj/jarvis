import type { Kr, Objective, RegionalAlignmentBoard, RegionalDemand } from './types'

export interface RegionalAlignedProject {
  objective: Objective
  kr: Kr
  demands: RegionalDemand[]
}

export interface RegionalPendingDemand {
  demand: RegionalDemand
  reasons: Array<'unmatched' | 'platform_not_onboard' | 'platform_not_accepted' | 'platform_onboard_but_not_accepted'>
}

export interface RegionalPendingPlatform {
  objective: Objective
  kr: Kr
  demands: RegionalDemand[]
  reason: 'unmatched' | 'region_not_onboard'
}

export interface RegionalAlignmentProjection {
  aligned: RegionalAlignedProject[]
  pendingDemands: RegionalPendingDemand[]
  pendingPlatform: RegionalPendingPlatform[]
}

export function projectRegionalAlignment(board: RegionalAlignmentBoard): RegionalAlignmentProjection {
  const decisions = new Map(board.decisions.map((item) => [item.planKrId, item]))
  const demandsByKr = new Map<string, RegionalDemand[]>()
  for (const demand of board.demands) {
    for (const krId of new Set(demand.planKrIds)) {
      demandsByKr.set(krId, [...(demandsByKr.get(krId) ?? []), demand])
    }
  }

  const aligned: RegionalAlignedProject[] = []
  const pendingPlatform: RegionalPendingPlatform[] = []
  for (const objective of board.plan.objectives) {
    for (const kr of objective.krs) {
      const decision = decisions.get(kr.id)
      const linked = demandsByKr.get(kr.id) ?? []
      const accepted = linked.filter((demand) => demand.acceptance === 'yes')
      if (decision?.onboard === 'yes' && accepted.length > 0) {
        aligned.push({ objective, kr, demands: accepted })
      }
      if (decision?.onboard === 'yes' && accepted.length === 0) {
        pendingPlatform.push({ objective, kr, demands: linked, reason: 'unmatched' })
      } else if (decision?.onboard === 'no' || (decision?.onboard !== 'yes' && accepted.length > 0)) {
        pendingPlatform.push({ objective, kr, demands: accepted.length > 0 ? accepted : linked, reason: 'region_not_onboard' })
      }
    }
  }

  const pendingDemands = board.demands.flatMap((demand): RegionalPendingDemand[] => {
    const reasons = new Set<RegionalPendingDemand['reasons'][number]>()
    if (demand.acceptance === 'yes' && demand.planKrIds.length === 0) reasons.add('unmatched')
    if (demand.acceptance === 'yes' && demand.planKrIds.some((id) => decisions.get(id)?.onboard !== 'yes')) reasons.add('platform_not_onboard')
    if (demand.acceptance === 'no' && !demand.planKrIds.some((id) => decisions.get(id)?.onboard === 'yes')) reasons.add('platform_not_accepted')
    if (demand.acceptance !== 'yes' && demand.planKrIds.some((id) => decisions.get(id)?.onboard === 'yes')) reasons.add('platform_onboard_but_not_accepted')
    return reasons.size ? [{ demand, reasons: [...reasons] }] : []
  })

  return { aligned, pendingDemands, pendingPlatform }
}
