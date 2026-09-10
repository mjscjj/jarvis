import { useCallback, useEffect, useState } from 'react'
import { getChatRuntimeConfig, listChatThreads, resolveChatBaseURL } from './api'

// Retry only when requested or when the browser comes back online. A chat POST
// is never replayed: an interrupted turn may already have performed actions.
export function useChatConnection() {
  const [baseURL, setBaseURL] = useState<string>()
  const [state, setState] = useState<'connecting' | 'ready' | 'disconnected' | 'offline'>('connecting')
  const [attempt, setAttempt] = useState(0)
  const reconnect = useCallback(() => setAttempt((value) => value + 1), [])

  useEffect(() => {
    let disposed = false
    const controller = new AbortController()
    const deadline = setTimeout(() => controller.abort(), 10_000)
    const connect = async () => {
      setState(navigator.onLine ? 'connecting' : 'offline')
      if (!navigator.onLine) return
      try {
        const { port } = await getChatRuntimeConfig(controller.signal)
        const nextURL = resolveChatBaseURL(window.location.origin, port)
        await listChatThreads(nextURL, controller.signal)
        if (disposed || controller.signal.aborted) return
        setBaseURL(nextURL)
        setState('ready')
      } catch {
        if (!disposed) setState(navigator.onLine ? 'disconnected' : 'offline')
      } finally {
        clearTimeout(deadline)
      }
    }
    const offline = () => { controller.abort(); setState('offline') }
    void connect()
    window.addEventListener('online', reconnect)
    window.addEventListener('offline', offline)
    return () => {
      disposed = true
      clearTimeout(deadline)
      controller.abort()
      window.removeEventListener('online', reconnect)
      window.removeEventListener('offline', offline)
    }
  }, [attempt, reconnect])

  return { baseURL, state, reconnect }
}
