import { createContext, useCallback, useContext, useMemo, useState } from 'react'
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

export function PageContextProvider({
  initialKey,
  children,
}: {
  initialKey: string
  children: ReactNode
}) {
  const [activeKey, setActiveKeyState] = useState(initialKey)
  const [selection, setSelection] = useState<PageSelection | null>(null)

  const setActiveKey = useCallback((key: string) => setActiveKeyState(key), [])

  const navigate = useCallback((key: string) => {
    setActiveKeyState(key)
    setSelection(null)
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
