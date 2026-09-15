export type PeopleSearchResult<C> = { candidates: C[]; has_more: boolean }

const QUERY_TTL_MS = 300 * 60 * 1000
const EMPTY_TTL_MS = 2 * 60 * 1000
const CACHE_LIMIT = 512

function queryKey(query: string) {
  return query.trim().toLocaleLowerCase()
}

function awaitWithAbort<T>(promise: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return promise
  if (signal.aborted) return Promise.reject(new DOMException('Aborted', 'AbortError'))
  return new Promise<T>((resolve, reject) => {
    const abort = () => reject(new DOMException('Aborted', 'AbortError'))
    signal.addEventListener('abort', abort, { once: true })
    promise.then(resolve, reject).finally(() => signal.removeEventListener('abort', abort))
  })
}

export function createPeopleSearchCache<C>(load: (query: string) => Promise<PeopleSearchResult<C>>) {
  const cache = new Map<string, { result: PeopleSearchResult<C>; expiresAt: number }>()
  const flights = new Map<string, Promise<PeopleSearchResult<C>>>()

  return async (query: string, signal?: AbortSignal): Promise<PeopleSearchResult<C>> => {
    const clean = query.trim()
    const key = queryKey(clean)
    const cached = cache.get(key)
    if (cached && cached.expiresAt > Date.now()) return cached.result
    if (cached) cache.delete(key)

    let flight = flights.get(key)
    if (!flight) {
      flight = load(clean).then((result) => {
        cache.set(key, { result, expiresAt: Date.now() + (result.candidates.length === 0 ? EMPTY_TTL_MS : QUERY_TTL_MS) })
        while (cache.size > CACHE_LIMIT) cache.delete(cache.keys().next().value!)
        return result
      }).finally(() => flights.delete(key))
      flights.set(key, flight)
    }
    return awaitWithAbort(flight, signal)
  }
}
