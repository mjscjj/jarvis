export function quarterOptions(currentQuarter: string, availableQuarters: readonly string[]): string[] {
  const current = currentQuarter.trim()
  const result: string[] = []
  const seen = new Set<string>()
  for (const quarter of current ? [current, ...availableQuarters] : availableQuarters) {
    const clean = quarter.trim()
    if (!clean || seen.has(clean)) continue
    seen.add(clean)
    result.push(clean)
  }
  return result
}

// A weekly page cannot operate on a quarter that has no formal OKR. Plan may
// legitimately point at the next quarter, so returning from Plan must resolve
// that route against the formal-OKR catalog before selecting a Review week.
export function fallbackQuarterForUnavailableScope(
  currentQuarter: string,
  availableQuarters: readonly string[],
): string | undefined {
  const current = currentQuarter.trim()
  if (!current || availableQuarters.includes(current)) return undefined
  return availableQuarters[0]
}
