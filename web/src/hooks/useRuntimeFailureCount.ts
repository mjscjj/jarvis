import { useEffect, useState } from 'react'

import { getDebugFailures } from '../api'
import { countUnrecoveredFailures } from '../runtimeFailures'

export function useRuntimeFailureCount(intervalMs = 60_000, enabled = true) {
  const [count, setCount] = useState<number>()
  const [error, setError] = useState<string>()

  useEffect(() => {
    if (!enabled) return
    let active = true
    let request: AbortController | undefined

    const load = () => {
      request?.abort()
      request = new AbortController()
      getDebugFailures(24, request.signal)
        .then(({ items }) => {
          if (!active) return
          setCount(countUnrecoveredFailures(items))
          setError(undefined)
        })
        .catch((cause: unknown) => {
          if (!active || cause instanceof DOMException) return
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
  }, [intervalMs, enabled])

  return { count, error }
}
