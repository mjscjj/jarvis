export const DEFAULT_OKR_TAB = 'manage'

export const OKR_TAB_DEFINITIONS = [
  { key: 'manage', label: '管理与打标', group: 'okr' },
  { key: 'agent-flows', label: '自动化流程', group: 'okr' },
  { key: 'weekly-fill', label: '周报填写', group: 'weekly', requiresModule: 'weekly-report' },
  { key: 'weekly-meeting', label: '周报会议', group: 'weekly', requiresModule: 'weekly-report' },
  { key: 'okr-review', label: 'OKR Review', group: 'weekly', requiresModule: 'weekly-report' },
] as const

export type OKRTab = typeof OKR_TAB_DEFINITIONS[number]['key']
export type WeeklyWorkspaceMode = 'fill' | 'meeting' | 'review'

const validTabs = new Set<string>(OKR_TAB_DEFINITIONS.map((item) => item.key))

export function isOKRTab(value: unknown): value is OKRTab {
  return typeof value === 'string' && validTabs.has(value)
}

export function isOKRTabEnabled(tab: OKRTab, moduleEnablement: Readonly<Record<string, boolean>>): boolean {
  const definition = OKR_TAB_DEFINITIONS.find((item) => item.key === tab)
  if (!definition) throw new Error(`unknown OKR tab: ${tab}`)
  return !('requiresModule' in definition) || moduleEnablement[definition.requiresModule] === true
}

export function resolveOKRTab(
  requested: unknown,
  moduleEnablement: Readonly<Record<string, boolean>>,
): OKRTab {
  const tab = isOKRTab(requested) ? requested : DEFAULT_OKR_TAB
  return isOKRTabEnabled(tab, moduleEnablement) ? tab : DEFAULT_OKR_TAB
}

export function isWeeklyWorkspaceTab(tab: OKRTab): boolean {
  return tab === 'weekly-fill' || tab === 'weekly-meeting' || tab === 'okr-review'
}

export function weeklyWorkspaceMode(tab: OKRTab): WeeklyWorkspaceMode {
  if (tab === 'weekly-fill') return 'fill'
  if (tab === 'weekly-meeting') return 'meeting'
  if (tab === 'okr-review') return 'review'
  throw new Error(`OKR tab is not a weekly workspace: ${tab}`)
}

export function okrTabForWeeklyMode(mode: WeeklyWorkspaceMode): OKRTab {
  if (mode === 'fill') return 'weekly-fill'
  if (mode === 'meeting') return 'weekly-meeting'
  if (mode === 'review') return 'okr-review'
  throw new Error(`unknown weekly workspace mode: ${String(mode)}`)
}
