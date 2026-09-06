export interface MovingNode {
  x?: number
  y?: number
  z?: number
  vx?: number
  vy?: number
  vz?: number
}

export interface BoundingForce {
  (alpha: number): void
  initialize(nodes: MovingNode[]): void
}

/** Keeps unlinked nodes from drifting so far away that the useful graph becomes microscopic. */
export function createBoundingForce(radius: number, strength = .16): BoundingForce {
  let nodes: MovingNode[] = []
  const safeRadius = Math.max(40, radius)
  const force = ((alpha: number) => {
    for (const node of nodes) {
      const x = Number.isFinite(node.x) ? node.x! : 0
      const y = Number.isFinite(node.y) ? node.y! : 0
      const z = Number.isFinite(node.z) ? node.z! : 0
      const distance = Math.hypot(x, y, z)
      if (distance <= safeRadius || distance === 0) continue
      const pull = (distance - safeRadius) / distance * strength * alpha
      node.vx = (node.vx || 0) - x * pull
      node.vy = (node.vy || 0) - y * pull
      node.vz = (node.vz || 0) - z * pull
    }
  }) as BoundingForce
  force.initialize = (nextNodes) => { nodes = nextNodes }
  return force
}
