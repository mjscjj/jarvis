export type LabelMode = 'related' | 'all' | 'hidden'
export type LabelSize = 'small' | 'medium' | 'large'
export type LabelLength = 'short' | 'medium' | 'full'
export type FocusScope = 'entity' | 'neighbors' | 'visible'
export type CameraRange = 'near' | 'standard' | 'far'
export type LayoutMode = 'free3d' | 'flat2d' | 'relation'
export type SpacingMode = 'compact' | 'standard' | 'loose'
export type NodeSizeMode = 'equal' | 'semantic'

export interface WorldMapSettings {
  labelMode: LabelMode
  labelSize: LabelSize
  labelLength: LabelLength
  focusScope: FocusScope
  cameraRange: CameraRange
  layoutMode: LayoutMode
  spacingMode: SpacingMode
  nodeSizeMode: NodeSizeMode
  arrows: boolean
  particles: boolean
  autoRotate: boolean
}

export const defaultWorldMapSettings: WorldMapSettings = {
  labelMode: 'related',
  labelSize: 'medium',
  labelLength: 'medium',
  focusScope: 'neighbors',
  cameraRange: 'standard',
  layoutMode: 'relation',
  spacingMode: 'standard',
  nodeSizeMode: 'semantic',
  arrows: true,
  particles: false,
  autoRotate: false,
}

export const worldMapSettingsStorageKey = 'jarvis.world-map.settings.v1'

const choices = {
  labelMode: ['related', 'all', 'hidden'],
  labelSize: ['small', 'medium', 'large'],
  labelLength: ['short', 'medium', 'full'],
  focusScope: ['entity', 'neighbors', 'visible'],
  cameraRange: ['near', 'standard', 'far'],
  layoutMode: ['free3d', 'flat2d', 'relation'],
  spacingMode: ['compact', 'standard', 'loose'],
  nodeSizeMode: ['equal', 'semantic'],
} as const

export function normalizeWorldMapSettings(value: unknown): WorldMapSettings {
  if (!value || typeof value !== 'object') return defaultWorldMapSettings
  const record = value as Record<string, unknown>
  const choice = <K extends keyof typeof choices>(key: K): WorldMapSettings[K] => (
    (choices[key] as readonly unknown[]).includes(record[key]) ? record[key] : defaultWorldMapSettings[key]
  ) as WorldMapSettings[K]
  return {
    labelMode: choice('labelMode'),
    labelSize: choice('labelSize'),
    labelLength: choice('labelLength'),
    focusScope: choice('focusScope'),
    cameraRange: choice('cameraRange'),
    layoutMode: choice('layoutMode'),
    spacingMode: choice('spacingMode'),
    nodeSizeMode: choice('nodeSizeMode'),
    arrows: typeof record.arrows === 'boolean' ? record.arrows : defaultWorldMapSettings.arrows,
    particles: typeof record.particles === 'boolean' ? record.particles : defaultWorldMapSettings.particles,
    autoRotate: typeof record.autoRotate === 'boolean' ? record.autoRotate : defaultWorldMapSettings.autoRotate,
  }
}

export function readWorldMapSettings(storage: Pick<Storage, 'getItem'> | undefined): WorldMapSettings {
  if (!storage) return defaultWorldMapSettings
  try {
    const raw = storage.getItem(worldMapSettingsStorageKey)
    return raw ? normalizeWorldMapSettings(JSON.parse(raw)) : defaultWorldMapSettings
  } catch {
    return defaultWorldMapSettings
  }
}

export function labelLengthValue(mode: LabelLength): number {
  return mode === 'short' ? 8 : mode === 'medium' ? 16 : Number.POSITIVE_INFINITY
}

export function labelSizeValue(mode: LabelSize): number {
  return mode === 'small' ? .84 : mode === 'large' ? 1.22 : 1
}

export function spacingValue(mode: SpacingMode): number {
  return mode === 'compact' ? .78 : mode === 'loose' ? 1.35 : 1
}

export function cameraPaddingValue(mode: CameraRange): number {
  return mode === 'near' ? .86 : mode === 'far' ? 1.48 : 1.08
}
