export type PreviewReviewTarget =
  | { kind: 'all'; title: string }
  | { kind: 'objective'; objectiveId: string; title: string }
  | { kind: 'kr'; objectiveId: string; krId: string; title: string }
  | { kind: 'point'; objectiveId: string; krId: string; pointId: string; title: string }

export type OKRReviewSource =
  | { reviewType: 'plan'; quarter: string; planId: string }
  | { reviewType: 'progress'; quarter: string; week: string }

export function previewReviewKey(target: PreviewReviewTarget): string {
  if (target.kind === 'all') return 'all'
  if (target.kind === 'objective') return `objective:${target.objectiveId}`
  if (target.kind === 'kr') return `kr:${target.krId}`
  return `point:${target.pointId}`
}

// previewReviewRequest turns a clicked target into the scope the review API
// expects. Only the IDs travel: the server reads the content itself, so the
// browser never has to ship the board back.
export function previewReviewRequest(source: OKRReviewSource, target: PreviewReviewTarget) {
  if (!source.quarter.trim()) throw new Error('缺少 OKR 季度，无法发起 AI 评审。')
  if (source.reviewType === 'plan' && !source.planId.trim()) throw new Error('缺少 OKR Plan，无法发起 AI 评审。')
  if (source.reviewType === 'progress' && !source.week.trim()) throw new Error('缺少 Review 周次，无法发起 AI 评审。')
  return {
    reviewType: source.reviewType,
    quarter: source.quarter,
    week: source.reviewType === 'progress' ? source.week : undefined,
    planId: source.reviewType === 'plan' ? source.planId : undefined,
    kind: target.kind,
    objectiveId: target.kind === 'all' ? undefined : target.objectiveId,
    krId: target.kind === 'kr' || target.kind === 'point' ? target.krId : undefined,
    pointId: target.kind === 'point' ? target.pointId : undefined,
  }
}
