import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { PageContext, PageSelection } from './types'

// PageContextValue exposes the readable PageContext (active_key + selection) plus
// the setters left/right panels need: pages write `selection`, App drives
// navigation via `navigate` (switch tab + clear selection).
export interface PageContextValue {
  context: PageContext
  setActiveKey: (key: string) => void
  setSelection: (selection: PageSelection | null) => void
  setViewState: (state: Record<string, string | number | boolean | null | undefined>, replace?: boolean) => void
  // navigate switches to a page and clears the previous page's selection.
  navigate: (key: string, viewState?: Record<string, string>) => void
}

const Context = createContext<PageContextValue | null>(null)

const pageHashes: Record<string, string> = {
  chat: '/chat',
  overview: '/today',
  tasks: '/work',
  progress: '/review',
  background: '/memory',
  todos: '/manage/clues',
  'scheduled-tasks': '/manage/automations',
  plugins: '/plugins',
  agents: '/agents',
  settings: '/manage/settings',
  debug: '/manage/runtime',
}

const pageKeysByHash = Object.fromEntries(
  Object.entries(pageHashes).map(([key, path]) => [path, key]),
) as Record<string, string>

interface HashRoute {
  key: string
  selection: PageSelection | null
  viewState: Record<string, string>
}

function isLegacyAgentSettingsHash(): boolean {
  const raw = window.location.hash.replace(/^#/, '')
  const [path, query = ''] = raw.split('?')
  return path === '/manage/settings' && new URLSearchParams(query).get('view') === 'agents'
}

function isLegacySecurityHash(): boolean {
  return window.location.hash.replace(/^#/, '').split('?')[0] === '/security'
}

function routeFromHash(initialKey: string): HashRoute {
  const raw = window.location.hash.replace(/^#/, '')
  const [path, query = ''] = raw.split('?')
  const viewState = Object.fromEntries(new URLSearchParams(query).entries())
  if (path === '/manage/settings' && viewState.view === 'agents') {
    const { view: _view, ...agentViewState } = viewState
    return { key: 'agents', selection: null, viewState: agentViewState }
  }
  if (path === '/security') {
    return { key: 'settings', selection: null, viewState: { view: 'security' } }
  }
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
  return { key: pageKeysByHash[path] || initialKey, selection: null, viewState }
}

function writePageHash(
  key: string,
  selection: PageSelection | null,
  viewState: Record<string, string>,
  replace = false,
) {
  const basePath = pageHashes[key]
  if (!basePath) throw new Error(`unknown page key: ${key}`)
  let path = basePath
  if (key === 'tasks' && selection?.kind === 'task') path = `/work/task/${selection.id}`
  if (key === 'todos' && selection?.kind === 'todo') path = `/manage/clues/${selection.id}`
  const query = new URLSearchParams(
    Object.entries(viewState).sort(([left], [right]) => left.localeCompare(right)),
  ).toString()
  const next = `#${path}${query ? `?${query}` : ''}`
  if (window.location.hash === next) return
  if (replace) window.history.replaceState(null, '', next)
  else window.location.hash = next.slice(1)
}

function pageKeyForSelection(selection: PageSelection, fallbackKey: string): string {
  if (selection.kind === 'task') return 'tasks'
  if (selection.kind === 'todo') return 'todos'
  return fallbackKey
}

export function PageContextProvider({
  initialKey,
  children,
}: {
  initialKey: string
  children: ReactNode
}) {
  const initialRoute = useMemo(() => routeFromHash(initialKey), [initialKey])
  const [activeKey, setActiveKeyState] = useState(initialRoute.key)
  const [selection, setSelectionState] = useState<PageSelection | null>(initialRoute.selection)
  const [viewState, setViewStateState] = useState<Record<string, string>>(initialRoute.viewState)

  useEffect(() => {
    if (!window.location.hash) {
      writePageHash(initialKey, null, {}, true)
    } else if (isLegacyAgentSettingsHash() || isLegacySecurityHash()) {
      writePageHash(initialRoute.key, initialRoute.selection, initialRoute.viewState, true)
    }
    const syncFromHash = () => {
      const route = routeFromHash(initialKey)
      setActiveKeyState(route.key)
      setSelectionState(route.selection)
      setViewStateState(route.viewState)
      if (isLegacyAgentSettingsHash() || isLegacySecurityHash()) {
        writePageHash(route.key, route.selection, route.viewState, true)
      }
    }
    window.addEventListener('hashchange', syncFromHash)
    return () => window.removeEventListener('hashchange', syncFromHash)
  }, [initialKey])

  const setActiveKey = useCallback((key: string) => {
    setActiveKeyState(key)
    setSelectionState(null)
    setViewStateState({})
    writePageHash(key, null, {})
  }, [])

  const setSelection = useCallback((next: PageSelection | null) => {
    const targetKey = next ? pageKeyForSelection(next, activeKey) : activeKey
    if (targetKey !== activeKey) setActiveKeyState(targetKey)
    setSelectionState(next)
    writePageHash(targetKey, next, viewState, next === null)
  }, [activeKey, viewState])

  const setViewState = useCallback((next: Record<string, string | number | boolean | null | undefined>, replace = true) => {
    const normalized = Object.fromEntries(
      Object.entries(next)
        .filter(([, value]) => value !== null && value !== undefined && value !== '')
        .map(([key, value]) => [key, String(value)]),
    )
    setViewStateState(normalized)
    writePageHash(activeKey, selection, normalized, replace)
  }, [activeKey, selection])

  const navigate = useCallback((key: string, nextViewState: Record<string, string> = {}) => {
    setActiveKeyState(key)
    setSelectionState(null)
    setViewStateState(nextViewState)
    writePageHash(key, null, nextViewState)
  }, [])

  const value = useMemo<PageContextValue>(
    () => ({
      context: { active_key: activeKey, selection, view_state: viewState },
      setActiveKey,
      setSelection,
      setViewState,
      navigate,
    }),
    [activeKey, selection, viewState, setActiveKey, setSelection, setViewState, navigate],
  )

  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function usePageContext(): PageContextValue {
  const value = useContext(Context)
  if (!value) throw new Error('usePageContext must be used within PageContextProvider')
  return value
}
