export function focusDistance(nodeRadius: number, viewportHeight: number, verticalFov = 50): number {
  const height = Math.max(320, viewportHeight)
  const targetDiameter = Math.min(104, Math.max(76, height * 0.12))
  const radians = verticalFov * Math.PI / 360
  const distance = (nodeRadius * 2 * height) / (targetDiameter * 2 * Math.tan(radians))
  return Math.min(290, Math.max(92, distance))
}
