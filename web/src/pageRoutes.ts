import type { PageSelection } from './types.ts'
import { isWeeklyShareViewState, WEEKLY_SHARE_SCOPE } from './okr/emily/share.ts'

const pageHashes: Record<string, string> = {
  overview: '/today',
  tasks: '/work',
  progress: '/review',
  'agency-okr': '/okr',
  background: '/memory',
  agents: '/agents',
  todos: '/manage/clues',
  'scheduled-tasks': '/manage/automations',
  plugins: '/plugins',
  settings: '/manage/settings',
  debug: '/manage/runtime',
}

const pageKeysByHash = Object.fromEntries(
  Object.entries(pageHashes).map(([key, path]) => [path, key]),
) as Record<string, string>

export interface HashRoute {
  key: string
  selection: PageSelection | null
  viewState: Record<string, string>
}

export function routeFromHash(hash: string, initialKey: string): HashRoute {
  const raw = hash.replace(/^#/, '')
  const [path, query = ''] = raw.split('?')
  const viewState = Object.fromEntries(new URLSearchParams(query).entries())
  const taskMatch = path.match(/^\/work\/task\/(\d+)$/)
  if (taskMatch) {
    const id = Number(taskMatch[1])
    return { key: 'tasks', selection: { kind: 'task', id, label: `Task #${id}` }, viewState }
  }
  const todoMatch = path.match(/^\/manage\/clues\/(\d+)$/)
  if (todoMatch) {
    const id = Number(todoMatch[1])
    return { key: 'todos', selection: { kind: 'todo', id, label: `线索 #${id}` }, viewState }
  }
  // Shared weekly-report links use the Agency OKR module while switching its
  // surface to the weekly fill, meeting, or Review view.
  if (path === '/weekly-report') {
    return { key: 'agency-okr', selection: null, viewState: { ...viewState, share: WEEKLY_SHARE_SCOPE, tab: viewState.tab || 'weekly-fill' } }
  }
  return { key: pageKeysByHash[path] || initialKey, selection: null, viewState }
}

export function pageHash(
  key: string,
  selection: PageSelection | null,
  viewState: Record<string, string>,
): string {
  const weeklyShare = key === 'agency-okr' && isWeeklyShareViewState(viewState)
  const basePath = weeklyShare ? '/weekly-report' : pageHashes[key]
  if (!basePath) throw new Error(`unknown page key: ${key}`)
  let path = basePath
  if (key === 'tasks' && selection?.kind === 'task') path = `/work/task/${selection.id}`
  if (key === 'todos' && selection?.kind === 'todo') path = `/manage/clues/${selection.id}`
  const query = new URLSearchParams(
    Object.entries(viewState)
      .filter(([viewKey]) => !(weeklyShare && viewKey === 'share'))
      .sort(([left], [right]) => left.localeCompare(right)),
  ).toString()
  return `#${path}${query ? `?${query}` : ''}`
}
