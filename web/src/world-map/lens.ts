import type { AppModule } from '../types'

export type WorldLens = 'world' | 'okr'

export function isOKRPluginEnabled(modules: readonly AppModule[]): boolean {
  return modules.some((module) => module.key === 'okr' && module.is_enabled)
}

export function defaultWorldLens(modules: readonly AppModule[]): WorldLens {
  return isOKRPluginEnabled(modules) ? 'okr' : 'world'
}
