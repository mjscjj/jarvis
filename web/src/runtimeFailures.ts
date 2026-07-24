export function countUnrecoveredFailures(events: Array<{ recovered: boolean }>): number {
  return events.filter((event) => !event.recovered).length
}
