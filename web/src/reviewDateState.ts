export const REVIEW_DATE_STATE_TTL_MS = 6 * 60 * 60 * 1000

export function reviewDateStateExpiresAt(selectedAt: string | undefined): number | undefined {
  if (!selectedAt) return undefined
  const value = Number(selectedAt)
  return Number.isFinite(value) ? value + REVIEW_DATE_STATE_TTL_MS : undefined
}

export function isReviewDateStateFresh(selectedAt: string | undefined, now = Date.now()): boolean {
  const expiresAt = reviewDateStateExpiresAt(selectedAt)
  return expiresAt !== undefined && Number(selectedAt) <= now && now < expiresAt
}
