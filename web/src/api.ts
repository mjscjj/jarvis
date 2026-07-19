import type { Todo, TodoList, TodoQuery } from './types'

interface APIResponse<T> {
  code: number
  data?: T
  msg?: string
}

async function request<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal, headers: { Accept: 'application/json' } })
  const payload = (await response.json()) as APIResponse<T>
  if (!response.ok || payload.code !== 0 || payload.data === undefined) {
    throw new Error(payload.msg || `请求失败：HTTP ${response.status}`)
  }
  return payload.data
}

export function listTodos(query: TodoQuery, signal?: AbortSignal): Promise<TodoList> {
  const params = new URLSearchParams({
    page: String(query.page),
    page_size: String(query.pageSize),
  })
  if (query.statuses.length > 0) params.set('status', query.statuses.join(','))
  if (query.actionType) params.set('action_type', query.actionType)
  if (query.leaderOnly) params.set('leader_only', 'true')
  return request<TodoList>(`/api/todos?${params.toString()}`, signal)
}

export function getTodo(id: number, signal?: AbortSignal): Promise<Todo> {
  return request<Todo>(`/api/todos/${id}`, signal)
}
