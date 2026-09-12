import { useEffect, useState } from 'react'

import { getDebugFailures } from '../api'
import { countUnrecoveredFailures } from '../runtimeFailures'

export function useRuntimeFailureCount(enabled: boolean, intervalMs = 60_000) {
  const [count, setCount] = useState<number>()
  const [error, setError] = useState<string>()

  useEffect(() => {
    setCount(undefined)
    setError(undefined)
    if (!enabled) return
    let active = true
    let request: AbortController | undefined

    const load = () => {
      request?.abort()
      const controller = new AbortController()
      request = controller
      getDebugFailures(24, controller.signal)
        .then(({ items }) => {
          if (!active || controller.signal.aborted) return
          setCount(countUnrecoveredFailures(items))
          setError(undefined)
        })
        .catch((cause: unknown) => {
          if (!active || controller.signal.aborted || cause instanceof DOMException) return
          setError(cause instanceof Error ? cause.message : String(cause))
        })
    }

    load()
    const timer = window.setInterval(load, intervalMs)
    return () => {
      active = false
      request?.abort()
      window.clearInterval(timer)
    }
  }, [enabled, intervalMs])

  return enabled ? { count, error } : {}
}
