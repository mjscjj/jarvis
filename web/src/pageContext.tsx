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
  // navigate switches to a page and clears the previous page's selection.
  navigate: (key: string) => void
}

const Context = createContext<PageContextValue | null>(null)

const pageHashes: Record<string, string> = {
  overview: '/today',
  tasks: '/work',
  progress: '/review',
  background: '/memory',
  todos: '/manage/clues',
  'scheduled-tasks': '/manage/automations',
  settings: '/manage/settings',
  debug: '/manage/runtime',
}

const pageKeysByHash = Object.fromEntries(
  Object.entries(pageHashes).map(([key, path]) => [path, key]),
) as Record<string, string>

interface HashRoute {
  key: string
  selection: PageSelection | null
}

function routeFromHash(initialKey: string): HashRoute {
  const path = window.location.hash.replace(/^#/, '').split('?')[0]
  const taskMatch = path.match(/^\/work\/task\/(\d+)$/)
  if (taskMatch) {
    const id = Number(taskMatch[1])
    return { key: 'tasks', selection: { kind: 'task', id, label: `Task #${id}` } }
  }
  const todoMatch = path.match(/^\/manage\/clues\/(\d+)$/)
  if (todoMatch) {
    const id = Number(todoMatch[1])
    return { key: 'todos', selection: { kind: 'todo', id, label: `线索 #${id}` } }
  }
  return { key: pageKeysByHash[path] || initialKey, selection: null }
}

function writePageHash(key: string, selection: PageSelection | null, replace = false) {
  const basePath = pageHashes[key]
  if (!basePath) throw new Error(`unknown page key: ${key}`)
  let path = basePath
  if (key === 'tasks' && selection?.kind === 'task') path = `/work/task/${selection.id}`
  if (key === 'todos' && selection?.kind === 'todo') path = `/manage/clues/${selection.id}`
  const next = `#${path}`
  if (window.location.hash === next) return
  if (replace) window.history.replaceState(null, '', next)
  else window.location.hash = path
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

  useEffect(() => {
    if (!window.location.hash) writePageHash(initialKey, null, true)
    const syncFromHash = () => {
      const route = routeFromHash(initialKey)
      setActiveKeyState(route.key)
      setSelectionState(route.selection)
    }
    window.addEventListener('hashchange', syncFromHash)
    return () => window.removeEventListener('hashchange', syncFromHash)
  }, [initialKey])

  const setActiveKey = useCallback((key: string) => {
    setActiveKeyState(key)
    setSelectionState(null)
    writePageHash(key, null)
  }, [])

  const setSelection = useCallback((next: PageSelection | null) => {
    const targetKey = next ? pageKeyForSelection(next, activeKey) : activeKey
    if (targetKey !== activeKey) setActiveKeyState(targetKey)
    setSelectionState(next)
    writePageHash(targetKey, next, next === null)
  }, [activeKey])

  const navigate = useCallback((key: string) => {
    setActiveKeyState(key)
    setSelectionState(null)
    writePageHash(key, null)
  }, [])

  const value = useMemo<PageContextValue>(
    () => ({
      context: { active_key: activeKey, selection },
      setActiveKey,
      setSelection,
      navigate,
    }),
    [activeKey, selection, setActiveKey, setSelection, navigate],
  )

  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function usePageContext(): PageContextValue {
  const value = useContext(Context)
  if (!value) throw new Error('usePageContext must be used within PageContextProvider')
  return value
}
