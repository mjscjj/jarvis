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

function pageKeyFromHash(initialKey: string): string {
  const path = window.location.hash.replace(/^#/, '').split('?')[0]
  return pageKeysByHash[path] || initialKey
}

function writePageHash(key: string, replace = false) {
  const path = pageHashes[key]
  if (!path) throw new Error(`unknown page key: ${key}`)
  const next = `#${path}`
  if (window.location.hash === next) return
  if (replace) window.history.replaceState(null, '', next)
  else window.location.hash = path
}

export function PageContextProvider({
  initialKey,
  children,
}: {
  initialKey: string
  children: ReactNode
}) {
  const [activeKey, setActiveKeyState] = useState(() => pageKeyFromHash(initialKey))
  const [selection, setSelection] = useState<PageSelection | null>(null)

  useEffect(() => {
    if (!window.location.hash) writePageHash(initialKey, true)
    const syncFromHash = () => {
      setActiveKeyState(pageKeyFromHash(initialKey))
      setSelection(null)
    }
    window.addEventListener('hashchange', syncFromHash)
    return () => window.removeEventListener('hashchange', syncFromHash)
  }, [initialKey])

  const setActiveKey = useCallback((key: string) => {
    setActiveKeyState(key)
    writePageHash(key)
  }, [])

  const navigate = useCallback((key: string) => {
    setActiveKeyState(key)
    setSelection(null)
    writePageHash(key)
  }, [])

  const value = useMemo<PageContextValue>(
    () => ({
      context: { active_key: activeKey, selection },
      setActiveKey,
      setSelection,
      navigate,
    }),
    [activeKey, selection, setActiveKey, navigate],
  )

  return <Context.Provider value={value}>{children}</Context.Provider>
}

export function usePageContext(): PageContextValue {
  const value = useContext(Context)
  if (!value) throw new Error('usePageContext must be used within PageContextProvider')
  return value
}
