export function canAddMetric(readOnly: boolean, structureReadOnly: boolean, metricCount: number): boolean {
  if (readOnly) return false
  return metricCount === 0 || !structureReadOnly
}
