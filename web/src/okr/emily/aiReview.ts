export type PreviewReviewTarget =
  | { kind: 'all'; title: string }
  | { kind: 'kr'; objectiveId: string; krId: string; title: string }
  | { kind: 'point'; objectiveId: string; krId: string; pointId: string; title: string }

export function previewReviewKey(target: PreviewReviewTarget): string {
  if (target.kind === 'all') return 'all'
  if (target.kind === 'kr') return `kr:${target.krId}`
  return `point:${target.pointId}`
}

// previewReviewRequest turns a clicked target into the scope the review API
// expects. Only the IDs travel: the server reads the content itself, so the
// browser never has to ship the board back.
export function previewReviewRequest(quarter: string, week: string, target: PreviewReviewTarget) {
  if (!quarter.trim()) throw new Error('缺少 OKR 季度，无法发起 AI 评审。')
  if (!week.trim()) throw new Error('缺少 Preview 周次，无法发起 AI 评审。')
  return {
    quarter,
    week,
    kind: target.kind,
    krId: target.kind === 'all' ? undefined : target.krId,
    pointId: target.kind === 'point' ? target.pointId : undefined,
  }
}
