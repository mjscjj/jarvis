import { useCallback, useEffect, useRef, useState } from 'react'
import { searchFeishuPeople } from './api'
import type { ResolveCandidate } from './types'

const EMPTY_CANDIDATES: never[] = []

interface SearchResult<C> { candidates: C[]; has_more: boolean }

interface FeishuPeopleSearchOptions<C> {
  searchFn?: (query: string, signal?: AbortSignal) => Promise<SearchResult<C>>
  active?: boolean
  debounceMs?: number | null
}

// useFeishuPeopleSearch owns the browser-side Feishu directory workflow for
// every single- and multi-select surface: cancellation, loading, result shape,
// ambiguity and upstream errors. Callers only decide how a selected person is
// displayed and persisted in their own domain.
export function useFeishuPeopleSearch<C = ResolveCandidate>(options: FeishuPeopleSearchOptions<C> = {}) {
  const { active = true, debounceMs = null } = options
  const [query, setQueryState] = useState('')
  const [result, setResult] = useState<SearchResult<C> | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [searchedQuery, setSearchedQuery] = useState('')
  const controllerRef = useRef<AbortController | null>(null)

  const cancel = useCallback(() => {
    controllerRef.current?.abort()
    controllerRef.current = null
    setLoading(false)
  }, [])

  const setQuery = useCallback((value: string) => {
    controllerRef.current?.abort()
    controllerRef.current = null
    setQueryState(value)
    setResult(null)
    setSearchedQuery('')
    setLoading(false)
    setError('')
  }, [])

  const reset = useCallback(() => {
    cancel()
    setQueryState('')
    setResult(null)
    setSearchedQuery('')
    setError('')
  }, [cancel])

  const search = useCallback(async () => {
    const clean = query.trim()
    if (!active || !clean) {
      reset()
      return
    }

    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    setResult(null)
    setSearchedQuery('')
    setLoading(true)
    setError('')
    try {
      const next = await (options.searchFn ?? searchFeishuPeople)(clean, controller.signal)
      if (controller.signal.aborted) return
      setResult(next as SearchResult<C>)
      setSearchedQuery(clean)
    } catch (cause) {
      if (!controller.signal.aborted) {
        setResult(null)
        setError(cause instanceof Error ? cause.message : '飞书人员搜索失败')
      }
    } finally {
      if (controllerRef.current === controller) {
        controllerRef.current = null
        setLoading(false)
      }
    }
  }, [active, query, reset, options.searchFn])

  useEffect(() => {
    if (!active) {
      cancel()
      setResult(null)
      setSearchedQuery('')
      setError('')
    }
  }, [active, cancel])

  useEffect(() => {
    if (!active || debounceMs == null || !query.trim()) return
    const timer = window.setTimeout(() => void search(), debounceMs)
    return () => window.clearTimeout(timer)
  }, [active, debounceMs, query, search])

  useEffect(() => () => {
    controllerRef.current?.abort()
    controllerRef.current = null
  }, [])

  return {
    query,
    setQuery,
    search,
    reset,
    candidates: result?.candidates ?? EMPTY_CANDIDATES,
    hasMore: result?.has_more ?? false,
    hasSearched: result !== null,
    searchedQuery,
    loading,
    error,
  }
}
