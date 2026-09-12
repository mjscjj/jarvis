import { useEffect, useState } from 'react'
import { listTasks } from '../api'

export interface ExecutingTaskState {
  count?: number
  error?: string
}

export function useExecutingTaskCount(enabled: boolean): ExecutingTaskState {
  const [state, setState] = useState<ExecutingTaskState>({})

  useEffect(() => {
    setState({})
    if (!enabled) return
    let active = true
    let timer: number | undefined
    let request: AbortController | undefined

    const stop = () => {
      window.clearTimeout(timer)
      request?.abort()
    }

    const load = async () => {
      const controller = new AbortController()
      request = controller
      try {
        const { total } = await listTasks(['executing'], 1, 1, controller.signal)
        if (active && !controller.signal.aborted) setState({ count: total })
      } catch (cause: unknown) {
        if (active && !controller.signal.aborted) {
          setState({ error: cause instanceof Error ? cause.message : String(cause) })
        }
      } finally {
        if (active && !controller.signal.aborted && !document.hidden) {
          timer = window.setTimeout(() => void load(), 3000)
        }
      }
    }

    const onVisibilityChange = () => {
      stop()
      if (!document.hidden) void load()
    }

    if (!document.hidden) void load()
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      active = false
      stop()
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [enabled])

  return enabled ? state : {}
}
