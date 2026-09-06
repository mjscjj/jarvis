export interface GraphBounds {
  x: [number, number]
  y: [number, number]
  z: [number, number]
}

export interface CameraFrame {
  center: { x: number; y: number; z: number }
  distance: number
}

export function cameraFrameForBounds(
  bounds: GraphBounds,
  viewportWidth: number,
  viewportHeight: number,
  verticalFov = 50,
  padding = 1.2,
): CameraFrame {
  const center = {
    x: (bounds.x[0] + bounds.x[1]) / 2,
    y: (bounds.y[0] + bounds.y[1]) / 2,
    z: (bounds.z[0] + bounds.z[1]) / 2,
  }
  const width = Math.max(24, bounds.x[1] - bounds.x[0])
  const height = Math.max(24, bounds.y[1] - bounds.y[0])
  const depth = Math.max(12, bounds.z[1] - bounds.z[0])
  const aspect = Math.max(.55, viewportWidth / Math.max(320, viewportHeight))
  const verticalRadians = verticalFov * Math.PI / 180
  const horizontalRadians = 2 * Math.atan(Math.tan(verticalRadians / 2) * aspect)
  const verticalDistance = height / 2 / Math.tan(verticalRadians / 2)
  const horizontalDistance = width / 2 / Math.tan(horizontalRadians / 2)
  const distance = Math.max(86, (Math.max(verticalDistance, horizontalDistance) + depth / 2) * padding)
  return { center, distance }
}
