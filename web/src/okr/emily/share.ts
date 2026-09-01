export type WeeklyShareMode = 'fill' | 'meeting'

export function weeklyShareURL(currentURL: string, mode: WeeklyShareMode): string {
  const url = new URL(currentURL)
  url.hash = `/weekly-report?tab=${mode === 'meeting' ? 'weekly-meeting' : 'weekly-fill'}`
  return url.toString()
}
