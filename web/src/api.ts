import type {
  ConfirmationDetail,
  Group,
  GroupBackgroundInput,
  Paged,
  Person,
  PersonInput,
  Project,
  ProjectInput,
  ResolveResult,
  Task,
  TaskList,
  TaskStatus,
  Todo,
  TodoList,
  TodoQuery,
} from './types'

interface APIResponse<T> {
  code: number
  data?: T
  msg?: string
}

interface RequestOptions {
  signal?: AbortSignal
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const response = await fetch(path, {
    signal: options.signal,
    method: options.method || 'GET',
    headers: { Accept: 'application/json', ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: options.body === undefined ? undefined : JSON.stringify(options.body),
  })
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
  return request<TodoList>(`/api/todos?${params.toString()}`, { signal })
}

export function getTodo(id: number, signal?: AbortSignal): Promise<Todo> {
  return request<Todo>(`/api/todos/${id}`, { signal })
}

export function listConfirmations(page = 1, pageSize = 20, signal?: AbortSignal): Promise<TodoList> {
  return request<TodoList>(`/api/confirmations?page=${page}&page_size=${pageSize}`, { signal })
}

export function getConfirmation(id: number, signal?: AbortSignal): Promise<ConfirmationDetail> {
  return request<ConfirmationDetail>(`/api/confirmations/${id}`, { signal })
}

export function approveConfirmation(id: number, expectedVersion: number, plan: Record<string, unknown>): Promise<Task> {
  return request<Task>(`/api/confirmations/${id}/approve`, {
    method: 'POST', body: { expected_version: expectedVersion, plan },
  })
}

export function rejectConfirmation(id: number, expectedVersion: number, reason: string): Promise<{ todo_id: number; status: string; version: number }> {
  return request(`/api/confirmations/${id}/reject`, {
    method: 'POST', body: { expected_version: expectedVersion, reason },
  })
}

export function listTasks(statuses: TaskStatus[], page = 1, pageSize = 20, signal?: AbortSignal): Promise<TaskList> {
  const params = new URLSearchParams({ status: statuses.join(','), page: String(page), page_size: String(pageSize) })
  return request<TaskList>(`/api/tasks?${params.toString()}`, { signal })
}

export function finishTask(id: number, expectedVersion: number, status: 'done' | 'failed', result: Record<string, unknown>): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/finish`, {
    method: 'POST', body: { expected_version: expectedVersion, status, result },
  })
}

// --- M1 background management ---

export function listProjects(page = 1, pageSize = 100, signal?: AbortSignal): Promise<Paged<Project>> {
  return request<Paged<Project>>(`/api/projects?page=${page}&page_size=${pageSize}`, { signal })
}

export function createProject(body: ProjectInput): Promise<Project> {
  return request<Project>('/api/projects', { method: 'POST', body })
}

export function updateProject(id: number, body: ProjectInput): Promise<Project> {
  return request<Project>(`/api/projects/${id}`, { method: 'PUT', body })
}

export function deleteProject(id: number): Promise<{ id: number; deleted: boolean }> {
  return request(`/api/projects/${id}`, { method: 'DELETE' })
}

export function listPersons(page = 1, pageSize = 100, signal?: AbortSignal): Promise<Paged<Person>> {
  return request<Paged<Person>>(`/api/persons?page=${page}&page_size=${pageSize}`, { signal })
}

export function resolvePerson(query: string): Promise<ResolveResult> {
  return request<ResolveResult>('/api/persons/resolve', { method: 'POST', body: { query } })
}

export function createPerson(body: PersonInput): Promise<Person> {
  return request<Person>('/api/persons', { method: 'POST', body })
}

export function updatePerson(id: number, body: PersonInput): Promise<Person> {
  return request<Person>(`/api/persons/${id}`, { method: 'PUT', body })
}

export function deletePerson(id: number): Promise<{ id: number; deleted: boolean }> {
  return request(`/api/persons/${id}`, { method: 'DELETE' })
}

export function listGroups(page = 1, pageSize = 100, signal?: AbortSignal): Promise<Paged<Group>> {
  return request<Paged<Group>>(`/api/groups?page=${page}&page_size=${pageSize}`, { signal })
}

export function updateGroupBackground(id: number, body: GroupBackgroundInput): Promise<Group> {
  return request<Group>(`/api/groups/${id}`, { method: 'PUT', body })
}
