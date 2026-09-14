import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import type { PageContext, PageSelection } from './types'
import { pageHash, routeFromHash } from './pageRoutes'

// PageContextValue exposes the readable PageContext (active_key + selection) plus
// the setters left/right panels need: pages write `selection`, App drives
// navigation via `navigate` (switch tab + clear selection).
export interface PageContextValue {
  context: PageContext
  setActiveKey: (key: string) => void
  setSelection: (selection: PageSelection | null) => void
  setViewState: (state: Record<string, string | number | boolean | null | undefined>, replace?: boolean) => void
  // navigate switches to a page and clears the previous page's selection. A
  // module child can provide its complete initial view state in the same write.
  navigate: (key: string, viewState?: Record<string, string | number | boolean | null | undefined>) => void
}

const Context = createContext<PageContextValue | null>(null)

function isLegacySecurityHash(): boolean {
  return window.location.hash.replace(/^#/, '').split('?')[0] === '/security'
}

function isLegacyAgentSettingsHash(): boolean {
  const [path, query = ''] = window.location.hash.replace(/^#/, '').split('?')
  return path === '/manage/settings' && new URLSearchParams(query).get('view') === 'agents'
}

function writePageHash(
  key: string,
  selection: PageSelection | null,
  viewState: Record<string, string>,
  replace = false,
) {
  const next = pageHash(key, selection, viewState)
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
  const initialRoute = useMemo(() => routeFromHash(window.location.hash, initialKey), [initialKey])
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
      const route = routeFromHash(window.location.hash, initialKey)
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

  const navigate = useCallback((key: string, nextViewState: Record<string, string | number | boolean | null | undefined> = {}) => {
    const normalized = Object.fromEntries(
      Object.entries(nextViewState)
        .filter(([, value]) => value !== null && value !== undefined && value !== '')
        .map(([viewKey, value]) => [viewKey, String(value)]),
    )
    setActiveKeyState(key)
    setSelectionState(null)
    setViewStateState(normalized)
    writePageHash(key, null, normalized)
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
