import { createContext, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { getAgentIdentity } from './api'

interface AgentIdentityContextValue {
  name: string
  shortName: string
}

const AgentIdentityContext = createContext<AgentIdentityContextValue | null>(null)

export function AgentIdentityProvider({ children }: { children: ReactNode }) {
  const [name, setName] = useState('助手')

  useEffect(() => {
    const controller = new AbortController()
    getAgentIdentity(controller.signal)
      .then((identity) => setName(identity.display_name))
      .catch(() => undefined)
    return () => controller.abort()
  }, [])

  useEffect(() => {
    document.title = name
  }, [name])

  const value = useMemo(() => ({
    name,
    shortName: Array.from(name)[0] || 'J',
  }), [name])

  return <AgentIdentityContext.Provider value={value}>{children}</AgentIdentityContext.Provider>
}

export function useAgentIdentity(): AgentIdentityContextValue {
  const value = useContext(AgentIdentityContext)
  if (!value) throw new Error('useAgentIdentity must be used within AgentIdentityProvider')
  return value
}
