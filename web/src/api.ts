import type {
  ConfirmationDetail,
  DebugRecord,
  DebugStatus,
  Digest,
  FailureEvent,
  Group,
  LogTail,
  ModuleRun,
  ScanRow,
  WatermarkRow,
  GroupBackgroundInput,
  GroupList,
  GroupQuery,
  Overview,
  Paged,
  Person,
  PersonInput,
  ProfileInput,
  ProfileView,
  Project,
  ProjectInput,
  Resource,
  ResourceInput,
  ResolveResult,
  SharedMemory,
  Task,
  TaskList,
  TaskStatus,
  ExecutionRunList,
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

export function supplementConfirmation(id: number, expectedVersion: number, note: string): Promise<{ todo_id: number; status: string; version: number }> {
  return request(`/api/confirmations/${id}/supplement`, {
    method: 'POST', body: { expected_version: expectedVersion, note },
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

export interface ExecuteResult {
  task_id: number
  run_id: number
  status: string
  branch?: string
  commit?: string
  diff_path?: string
  merge_request_url?: string
  summary?: string
  skipped?: boolean
  skip_reason?: string
}

// executeTask kicks agent-driven codex execution in the background; the API
// returns once the Task is claimed, not when codex finishes.
export function executeTask(id: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/execute`, { method: 'POST' })
}

// rerunTask resets a finished Task and kicks execution in the background.
export function rerunTask(id: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/rerun`, { method: 'POST' })
}

// reapplyTask re-lands the SAME approved proposal for a Task whose apply stage
// failed, WITHOUT going through propose/approval again (区别于 rerun 会重新审批).
export function reapplyTask(id: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/reapply`, { method: 'POST' })
}

// approveTask lands a proposal the user accepted: the awaiting_approval Task runs
// the apply stage (a fresh codex run carrying the approved proposal) for real.
export function approveTask(id: number, expectedVersion: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/approve`, {
    method: 'POST', body: { expected_version: expectedVersion },
  })
}

// rejectTask declines a proposed external write: the Task moves to failed with an
// optional reason; it can later be rerun to re-propose.
export function rejectTask(id: number, expectedVersion: number, reason: string): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/reject`, {
    method: 'POST', body: { expected_version: expectedVersion, reason },
  })
}

export function supplementTask(id: number, expectedVersion: number, note: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/supplement`, {
    method: 'POST',
    body: { expected_version: expectedVersion, note },
  })
}

// listTaskRuns 拉某个 Task 的执行审计历史（ExecutionRun 列表），最新在前。
export function listTaskRuns(id: number, signal?: AbortSignal): Promise<ExecutionRunList> {
  return request<ExecutionRunList>(`/api/tasks/${id}/runs`, { signal })
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

export function listGroups(query: GroupQuery, signal?: AbortSignal): Promise<GroupList> {
  const params = new URLSearchParams({
    page: String(query.page),
    page_size: String(query.pageSize),
  })
  if (query.relatedOnly) params.set('related_only', 'true')
  if (query.keyword) params.set('keyword', query.keyword)
  if (query.chatMode) params.set('chat_mode', query.chatMode)
  if (query.tier) params.set('tier', query.tier)
  return request<GroupList>(`/api/groups?${params.toString()}`, { signal })
}

export function updateGroupBackground(id: number, body: GroupBackgroundInput): Promise<Group> {
  return request<Group>(`/api/groups/${id}`, { method: 'PUT', body })
}

export function getProfile(): Promise<ProfileView> {
  return request<ProfileView>('/api/profile')
}

export function updateProfile(body: ProfileInput): Promise<ProfileView> {
  return request<ProfileView>('/api/profile', { method: 'PUT', body })
}

// --- Shared memory ---

export function getSharedMemory(signal?: AbortSignal): Promise<SharedMemory> {
  return request<SharedMemory>('/api/shared-memory', { signal })
}

export function updateSharedMemory(content: string): Promise<SharedMemory> {
  return request<SharedMemory>('/api/shared-memory', { method: 'PUT', body: { content } })
}

// --- Overview & Progress ---

export function getOverview(signal?: AbortSignal): Promise<Overview> {
  return request<Overview>('/api/overview', { signal })
}

export function getDigests(days = 7, signal?: AbortSignal): Promise<Digest> {
  return request<Digest>(`/api/digests?days=${days}`, { signal })
}

export function summarizeDigest(days = 7): Promise<{ summary: string; days: number }> {
  return request<{ summary: string; days: number }>(`/api/digests/summarize?days=${days}`, { method: 'POST' })
}

// --- Debug panel ---

export function getDebugStatus(signal?: AbortSignal): Promise<DebugStatus> {
  return request<DebugStatus>('/api/debug/status', { signal })
}

export function getDebugModules(signal?: AbortSignal): Promise<{ items: ModuleRun[] }> {
  return request<{ items: ModuleRun[] }>('/api/debug/modules', { signal })
}

export function getDebugFailures(hours = 24, signal?: AbortSignal): Promise<{ items: FailureEvent[] }> {
  return request<{ items: FailureEvent[] }>(`/api/debug/failures?hours=${hours}`, { signal })
}

export function getDebugScans(limit = 50, signal?: AbortSignal): Promise<{ items: ScanRow[] }> {
  return request<{ items: ScanRow[] }>(`/api/debug/scans?limit=${limit}`, { signal })
}

export function getDebugTodos(limit = 20, signal?: AbortSignal): Promise<{ items: DebugRecord[] }> {
  return request<{ items: DebugRecord[] }>(`/api/debug/todos?limit=${limit}`, { signal })
}

export function getDebugTasks(limit = 20, signal?: AbortSignal): Promise<{ items: DebugRecord[] }> {
  return request<{ items: DebugRecord[] }>(`/api/debug/tasks?limit=${limit}`, { signal })
}

export function getDebugWatermarks(signal?: AbortSignal): Promise<{ items: WatermarkRow[] }> {
  return request<{ items: WatermarkRow[] }>('/api/debug/watermarks', { signal })
}

export function getDebugLogs(lines = 300, signal?: AbortSignal): Promise<LogTail> {
  return request<LogTail>(`/api/debug/logs?lines=${lines}`, { signal })
}

// --- Debug 手动采集触发 ---

export function captureDiscover(): Promise<{ action: string; ok: boolean }> {
  return request(`/api/debug/capture/discover`, { method: 'POST' })
}

export function captureScanRelated(): Promise<{ action: string; ok: boolean }> {
  return request(`/api/debug/capture/scan-related`, { method: 'POST' })
}

export function captureScanChat(chatId: string): Promise<{ action: string; chat_id: string; ok: boolean }> {
  return request(`/api/debug/capture/scan-chat`, { method: 'POST', body: { chat_id: chatId } })
}

export function listResources(page = 1, pageSize = 100, signal?: AbortSignal): Promise<Paged<Resource>> {
  return request<Paged<Resource>>(`/api/resources?page=${page}&page_size=${pageSize}`, { signal })
}

export function createResource(body: ResourceInput): Promise<Resource> {
  return request<Resource>('/api/resources', { method: 'POST', body })
}

export function updateResource(id: number, body: ResourceInput): Promise<Resource> {
  return request<Resource>(`/api/resources/${id}`, { method: 'PUT', body })
}

export function deleteResource(id: number): Promise<{ id: number; deleted: boolean }> {
  return request(`/api/resources/${id}`, { method: 'DELETE' })
}
