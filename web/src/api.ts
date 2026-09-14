import { appPath } from './appPath.ts'
import type {
  Delegation,
  DelegationCheck,
  AgentSkill,
  AgentSkillContent,
  AgentSkillContentInput,
  AgentSkillInput,
  AgentProcessSnapshot,
  CommitWorklog,
  DailyDigest,
  DailyDigestKickResult,
  DailyDigestScope,
  Digest,
  DocumentWorklog,
  FailureEvent,
  Group,
  MeetingReviewList,
  ModuleRun,
  MorningBrief,
  WatermarkRow,
  GroupBackgroundInput,
  GroupList,
  GroupQuery,
  KeyMatter,
  KeyMatterInput,
  KeyMatterList,
  Paged,
  Person,
  PersonCreateInput,
  PersonUpdateInput,
  ProfileInput,
  ProfileView,
  PageType,
  PageIndexItem,
  PageUpdateInput,
  PageView,
  Project,
  ProjectBundle,
  ProjectImportPreview,
  ProjectInput,
  RepositoryBinding,
  Resource,
  ResourceInput,
  ResourceList,
  ResolveResult,
  SharedMemory,
  Task,
  CreateTaskInput,
  CreateTaskResult,
  TaskList,
  TaskStatus,
  ExecutionRunList,
  ExecutionRun,
  TaskRunOutput,
  Fact,
  FactSearchQuery,
  FactSearchResult,
  FactTimeline,
  TaskEvent,
  Todo,
  TodoList,
  TodoQuery,
  TodoStatus,
  WorkRule,
  WorkRuleInput,
  TextFile,
  TextFileInput,
  AgentConfigPreview,
  AgentConfigStage,
  ScheduledTask,
  ScheduledTaskInput,
  ScheduledTaskListItem,
  RuntimeSettings,
  RuntimeSettingsView,
  AppModule,
  AppModuleInput,
  SecuritySettings,
  SecuritySettingsView,
  AccessAuditActorKind,
  AccessAuditEventList,
  AccessAuditOperation,
  AgentIdentity,
  Plugin,
  PluginAuthorization,
  WorldProgress,
  WorldProgressSignal,
  EntityRelation,
  ProactiveRun,
  ProactiveRunDetail,
  MonitoringSnapshot,
  SystemTaskRunList,
  WebConfig,
  AuthView,
  SetupFlow,
  SetupStatus,
} from './types'

interface APIResponse<T> {
  code: number
  data?: T
  msg?: string
}

interface RequestOptions {
  signal?: AbortSignal
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  timeoutMs?: number
}

export const authEvents = new EventTarget()

let authRecoveryHandler: (() => Promise<void>) | null = null

export function setAuthRecoveryHandler(handler: (() => Promise<void>) | null): void {
  authRecoveryHandler = handler
}

function isAuthPath(path: string): boolean {
  return path.startsWith('/api/auth/')
}

function canRetryAfterAuth(options?: RequestInit): boolean {
  const method = (options?.method || 'GET').toUpperCase()
  return method === 'GET' || method === 'HEAD'
}

export async function apiFetch(path: string, options?: RequestInit): Promise<Response> {
  const response = await fetch(appPath(path), options)
  if (response.status === 401 && !isAuthPath(path)) {
    authEvents.dispatchEvent(new Event('expired'))
    if (authRecoveryHandler && canRetryAfterAuth(options)) {
      await authRecoveryHandler()
      if (!options?.signal?.aborted) return fetch(appPath(path), options)
    }
  }
  return response
}

export class APIRequestError extends Error {
  readonly status: number
  readonly code: number

  constructor(message: string, status: number, code: number) {
    super(message)
    this.name = 'APIRequestError'
    this.status = status
    this.code = code
  }
}

// ServiceUnavailableError 表示服务暂时不可用：后端重启期间网关会返回一个 HTML
// 错误页，或返回不带 JSON 正文的 5xx。前端据此显示「正在重连」而不是把 HTML
// 丢给 JSON.parse 抛出看不懂的 "Unexpected token '<'"。
export class ServiceUnavailableError extends Error {
  readonly status: number

  constructor(message: string, status: number) {
    super(message)
    this.name = 'ServiceUnavailableError'
    this.status = status
  }
}

export function isServiceUnavailableError(cause: unknown): cause is ServiceUnavailableError {
  return cause instanceof ServiceUnavailableError
}

// looksLikeServiceRestart 判断一个响应是否来自「服务重启 / 暂时不可用」：网关
// 回了 HTML 错误页，或响应体不是 JSON。用于在解析前给出人话提示。
export function looksLikeServiceRestart(response: Response): boolean {
  if (response.status === 502 || response.status === 503 || response.status === 504) return true
  const contentType = response.headers.get('content-type') || ''
  return !contentType.includes('application/json') && !contentType.includes('text/event-stream')
}

// pingHealth 探一次后端健康检查，用于自动 / 手动重连时确认服务是否恢复。
export async function pingHealth(signal?: AbortSignal): Promise<boolean> {
  try {
    const response = await fetch(appPath('/healthz'), { signal, headers: { Accept: 'application/json' } })
    return response.ok
  } catch {
    return false
  }
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const controller = new AbortController()
  const abort = () => controller.abort(options.signal?.reason)
  if (options.signal?.aborted) abort()
  else options.signal?.addEventListener('abort', abort, { once: true })
  const timer = options.timeoutMs
    ? setTimeout(() => controller.abort(new Error('请求超时，请检查网络后重试')), options.timeoutMs)
    : undefined
  try {
    const response = await apiFetch(path, {
      signal: controller.signal,
      method: options.method || 'GET',
      headers: { Accept: 'application/json', ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }) },
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
    })
    // 网关 HTML 错误页不能交给 JSON.parse；服务端 JSON 错误应保留具体原因，
    // 例如 SSO 上游连接失败的 502，不能一律解释成 Jarvis 正在重启。
    if (looksLikeServiceRestart(response) && !response.headers.get('content-type')?.includes('application/json')) {
      throw new ServiceUnavailableError('与服务的连接中断，可能正在重启', response.status)
    }
    const payload = (await response.json()) as APIResponse<T>
    if (!response.ok || payload.code !== 0 || payload.data === undefined) {
      throw new APIRequestError(payload.msg || `请求失败：HTTP ${response.status}`, response.status, payload.code)
    }
    return payload.data
  } finally {
    clearTimeout(timer)
    options.signal?.removeEventListener('abort', abort)
  }
}

export function getAuthStatus(signal?: AbortSignal): Promise<AuthView> {
  return request<AuthView>('/api/auth/status', { signal, timeoutMs: 25000 })
}

export function loginWithByteDance(): Promise<AuthView> {
  return request<AuthView>('/api/auth/login', { method: 'POST', timeoutMs: 45000 })
}

export function completeByteDanceLogin(flowId: string): Promise<AuthView> {
  return request<AuthView>('/api/auth/login/complete', {
    method: 'POST',
    body: { flow_id: flowId },
    timeoutMs: 45000,
  })
}

export function logoutFromJarvis(): Promise<AuthView> {
  return request<AuthView>('/api/auth/logout', { method: 'POST', timeoutMs: 25000 })
}

export function getSetupStatus(signal?: AbortSignal): Promise<SetupStatus> {
  return request<SetupStatus>('/api/setup/status', { signal, timeoutMs: 60000 })
}

export function getSetupPermissionConfig(): Promise<{ scopes: { tenant: string[]; user: string[] } }> {
  return request('/api/setup/lark/permissions', { timeoutMs: 10000 })
}

export function getSetupBootstrap(signal?: AbortSignal): Promise<{ machine_configuration_ready: boolean }> {
  return request('/api/setup/bootstrap', { signal, timeoutMs: 5000 })
}

export function beginSetupLarkConnection(): Promise<SetupFlow> {
  return request<SetupFlow>('/api/setup/lark/connect', { method: 'POST', timeoutMs: 30000 })
}

export function beginSetupLarkLogin(): Promise<SetupFlow> {
  return request<SetupFlow>('/api/setup/lark/login', { method: 'POST', timeoutMs: 30000 })
}

export function beginSetupAgentLogin(): Promise<SetupFlow> {
  return request<SetupFlow>('/api/setup/agent/login', { method: 'POST', timeoutMs: 30000 })
}

export function getSetupFlow(flowId: string, signal?: AbortSignal): Promise<SetupFlow> {
  return request<SetupFlow>(`/api/setup/flows/${encodeURIComponent(flowId)}`, { signal, timeoutMs: 25000 })
}

export function cancelSetupFlow(flowId: string): Promise<SetupFlow> {
  return request<SetupFlow>(`/api/setup/flows/${encodeURIComponent(flowId)}/cancel`, { method: 'POST', timeoutMs: 25000 })
}

export function finalizeSetup(appSecret: string): Promise<SetupStatus> {
  return request<SetupStatus>('/api/setup/finalize', {
    method: 'POST',
    body: { app_secret: appSecret },
    timeoutMs: 120000,
  })
}

export function repairSetupLarkCredentials(appSecret: string): Promise<{ saved: boolean }> {
  return request<{ saved: boolean }>('/api/setup/lark/credentials', {
    method: 'POST', body: { app_secret: appSecret }, timeoutMs: 60000,
  })
}

export function bootstrapSetupWorldModel(): Promise<{ task_id: number; status: string }> {
  return request<{ task_id: number; status: string }>('/api/setup/world-model', { method: 'POST' })
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
  return request<Todo>(`/api/todos/${id}?context=full`, { signal })
}

// 只在 observing 和 extracted 之间搬动：把线索按下不表，或重新交给 Task 固化与执行流水线。
export function setTodoStatus(id: number, status: TodoStatus, reason: string): Promise<Todo> {
  return request<Todo>(`/api/todos/${id}/status`, {
    method: 'PATCH',
    body: { status, actor: 'principal', reason },
  })
}

export interface TaskListQuery {
  query?: string
  actionType?: string
  excludeActionType?: string
  plugin?: string
}

export function listTasks(
  statuses: TaskStatus[],
  page = 1,
  pageSize = 20,
  signal?: AbortSignal,
  filters: TaskListQuery | string = {},
): Promise<TaskList> {
  const params = new URLSearchParams({ status: statuses.join(','), page: String(page), page_size: String(pageSize) })
  const normalized = typeof filters === 'string' ? { query: filters } : filters
  if (normalized.query?.trim()) params.set('query', normalized.query.trim())
  if (normalized.actionType) params.set('action_type', normalized.actionType)
  if (normalized.excludeActionType) params.set('exclude_action_type', normalized.excludeActionType)
  if (normalized.plugin) params.set('plugin', normalized.plugin)
  return request<TaskList>(`/api/tasks?${params.toString()}`, { signal })
}

export function getTask(id: number, signal?: AbortSignal): Promise<Task> {
  return request<Task>(`/api/tasks/${id}?context=full`, { signal })
}

export function createTask(body: CreateTaskInput): Promise<CreateTaskResult> {
  return request<CreateTaskResult>('/api/tasks', { method: 'POST', body })
}

export function getWebConfig(signal?: AbortSignal): Promise<WebConfig> {
  return request<WebConfig>('/api/web-config', { signal })
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
  summary?: string
  skipped?: boolean
  skip_reason?: string
}

// executeTask kicks agent-driven codex execution in the background; the API
// returns once the Task is claimed, not when codex finishes.
export function executeTask(id: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/execute`, { method: 'POST' })
}

export function interruptTask(id: number, expectedVersion: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/interrupt`, {
    method: 'POST', body: { expected_version: expectedVersion },
  })
}

// rerunTask resets a finished Task and kicks execution in the background.
export function rerunTask(id: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/rerun`, { method: 'POST' })
}

// resumeTask continues the exact Codex session that parked at needs_human.
export function resumeTask(id: number, expectedVersion: number, response: string): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/resume`, {
    method: 'POST', body: { expected_version: expectedVersion, response },
  })
}

// recallEffectMessage 撤回该任务「对外产出」里的一条飞书消息（真实调 lark-cli，
// 不可恢复），并把「已撤回」标记写回对应 effect；返回更新后的任务。
export function recallEffectMessage(id: number, messageID: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/effects/recall-message`, {
    method: 'POST', body: { message_id: messageID },
  })
}

export function supplementTask(id: number, expectedVersion: number, note: string): Promise<Task> {
  return request<Task>(`/api/tasks/${id}/supplement`, {
    method: 'POST',
    body: { expected_version: expectedVersion, note },
  })
}

// listTaskRuns 拉某个 Task 的执行审计历史（ExecutionRun 列表），最新在前。
export function listTaskRuns(id: number, page = 1, pageSize = 20, signal?: AbortSignal): Promise<ExecutionRunList> {
  return request<ExecutionRunList>(`/api/tasks/${id}/runs?page=${page}&page_size=${pageSize}`, { signal })
}

export function getTaskRun(id: number, signal?: AbortSignal): Promise<ExecutionRun> {
  return request<ExecutionRun>(`/api/task-runs/${id}`, { signal })
}

export function getTaskRunOutput(id: number, signal?: AbortSignal): Promise<TaskRunOutput> {
  return request<TaskRunOutput>(`/api/tasks/${id}/output`, { signal })
}

export function listTaskEvents(id: number, signal?: AbortSignal): Promise<{ items: TaskEvent[] }> {
  return request<{ items: TaskEvent[] }>(`/api/tasks/${id}/events`, { signal })
}

export class PageConflictError extends Error {
  readonly current: PageView

  constructor(current: PageView, message: string) {
    super(message)
    this.name = 'PageConflictError'
    this.current = current
  }
}

export function isPageConflictError(cause: unknown): cause is PageConflictError {
  return cause instanceof PageConflictError
}

export function getPage(type: PageType, id: number, signal?: AbortSignal): Promise<PageView> {
  return request<PageView>(`/api/pages/${type}/${id}`, { signal })
}

export async function listPages(all = false, signal?: AbortSignal, q = ''): Promise<PageIndexItem[]> {
  const params = new URLSearchParams({ page_size: '200' })
  if (all) params.set('all', 'true')
  if (q.trim()) params.set('q', q.trim())
  const items: PageIndexItem[] = []
  let cursor = ''
  do {
    if (cursor) params.set('cursor', cursor)
    const page = await request<{ items: PageIndexItem[]; next_cursor?: string }>(`/api/pages?${params.toString()}`, { signal })
    items.push(...page.items)
    cursor = page.next_cursor ?? ''
  } while (cursor)
  return items
}

export async function updatePage(type: PageType, id: number, body: PageUpdateInput): Promise<PageView> {
  const response = await apiFetch(`/api/pages/${type}/${id}`, {
    method: 'PUT',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const payload = (await response.json()) as APIResponse<PageView>
  if (response.status === 409) {
    if (payload.data === undefined) {
      throw new Error(payload.msg || '页面已被其他人更新，但响应未返回当前全文')
    }
    throw new PageConflictError(payload.data, payload.msg || '页面已被其他人更新')
  }
  if (!response.ok || payload.code !== 0 || payload.data === undefined) {
    throw new APIRequestError(payload.msg || `请求失败：HTTP ${response.status}`, response.status, payload.code)
  }
  return payload.data
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

export function deleteProject(id: number): Promise<{ id: number; archived: boolean }> {
  return request(`/api/projects/${id}`, { method: 'DELETE' })
}

export function exportProject(id: number): Promise<ProjectBundle> {
  return request<ProjectBundle>(`/api/projects/${id}/export`)
}

export function previewProjectImport(bundle: ProjectBundle, name?: string, code?: string | null): Promise<ProjectImportPreview> {
  return request<ProjectImportPreview>('/api/projects/import/preview', {
    method: 'POST', body: { bundle, name, code },
  })
}

export function importProject(bundle: ProjectBundle, name?: string, code?: string | null): Promise<Project> {
  return request<Project>('/api/projects/import', {
    method: 'POST', body: { bundle, name, code },
  })
}

export function duplicateProject(id: number, name: string, code: string | null): Promise<Project> {
  return request<Project>(`/api/projects/${id}/duplicate`, {
    method: 'POST', body: { name, code },
  })
}

export function resolveProjectRepositories(id: number): Promise<{ items: RepositoryBinding[] }> {
  return request<{ items: RepositoryBinding[] }>(`/api/projects/${id}/repositories/resolve`, {
    method: 'POST',
  })
}

export function listKeyMatters(page = 1, pageSize = 100, includeClosed = false, signal?: AbortSignal): Promise<KeyMatterList> {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (includeClosed) params.set('include_closed', 'true')
  return request<KeyMatterList>(`/api/key-matters?${params.toString()}`, { signal })
}

export function createKeyMatter(body: KeyMatterInput): Promise<KeyMatter> {
  return request<KeyMatter>('/api/key-matters', { method: 'POST', body })
}

export function updateKeyMatter(id: number, body: KeyMatterInput): Promise<KeyMatter> {
  return request<KeyMatter>(`/api/key-matters/${id}`, { method: 'PUT', body })
}

export function touchKeyMatter(id: number): Promise<KeyMatter> {
  return request<KeyMatter>(`/api/key-matters/${id}/touch`, { method: 'POST' })
}

export function closeKeyMatter(id: number): Promise<{ id: number; closed: boolean }> {
  return request(`/api/key-matters/${id}`, { method: 'DELETE' })
}

export function listSubjectFacts(subjectType: string, id: number, signal?: AbortSignal, options: {
  from?: string
  until?: string
  sourceKind?: string
  limit?: number
} = {}): Promise<{ items: Fact[] }> {
  const params = new URLSearchParams({
    subject_type: subjectType,
    subject_id: String(id),
    limit: String(options.limit ?? 200),
  })
  if (options.from) params.set('from', options.from)
  if (options.until) params.set('until', options.until)
  if (options.sourceKind) params.set('source_kind', options.sourceKind)
  return request<{ items: Fact[] }>(`/api/facts?${params.toString()}`, { signal })
}

export function appendProjectFact(id: number, description: string): Promise<Fact> {
	return appendFact({ subject_type: 'project', subject_id: id, description })
}

function appendFact(body: {
  subject_type: string
  subject_id: number
  description: string
  occurred_at?: string
  source_kind?: string
}): Promise<Fact> {
	return request<Fact>('/api/facts', {
    method: 'POST',
		body,
  })
}

export function getFactTimeline(days = 3, subject?: { type: string; id: number }, signal?: AbortSignal): Promise<FactTimeline> {
  const params = new URLSearchParams({ days: String(days) })
  if (subject) {
    params.set('subject_type', subject.type)
    params.set('subject_id', String(subject.id))
  }
  return request<FactTimeline>(`/api/facts/timeline?${params.toString()}`, { signal })
}

export function searchFacts(query: FactSearchQuery, signal?: AbortSignal): Promise<FactSearchResult> {
  const params = new URLSearchParams({
    page: String(query.page ?? 1),
    page_size: String(query.pageSize ?? 50),
  })
  if (query.q) params.set('q', query.q)
  if (query.from) params.set('from', query.from)
  if (query.until) params.set('until', query.until)
  if (query.subjectType) params.set('subject_type', query.subjectType)
  if (query.subjectId) params.set('subject_id', String(query.subjectId))
  if (query.sourceKind) params.set('source_kind', query.sourceKind)
  return request<FactSearchResult>(`/api/facts/search?${params.toString()}`, { signal })
}

export function listPersons(page = 1, pageSize = 100, signal?: AbortSignal): Promise<Paged<Person>> {
  return request<Paged<Person>>(`/api/persons?page=${page}&page_size=${pageSize}`, { signal })
}

export function searchFeishuPeople(query: string, signal?: AbortSignal): Promise<ResolveResult> {
  const params = new URLSearchParams({ q: query })
  return request<ResolveResult>(`/api/people/search?${params.toString()}`, { signal })
}

export function createPerson(body: PersonCreateInput): Promise<Person> {
  return request<Person>('/api/persons', { method: 'POST', body })
}

export function updatePerson(id: number, body: PersonUpdateInput): Promise<Person> {
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
  if (query.keyOnly) params.set('key_only', 'true')
  if (query.keyword) params.set('keyword', query.keyword)
  if (query.chatMode) params.set('chat_mode', query.chatMode)
  if (query.tier) params.set('tier', query.tier)
  if (query.captureState) params.set('capture_state', query.captureState)
  return request<GroupList>(`/api/groups?${params.toString()}`, { signal })
}

export function updateGroupBackground(id: number, body: GroupBackgroundInput): Promise<Group> {
  return request<Group>(`/api/groups/${id}`, { method: 'PUT', body })
}

export function updateGroupCaptureExclusion(groupIds: number[], excluded: boolean): Promise<{ updated: number; excluded: boolean }> {
  return request('/api/groups/capture-exclusion', { method: 'PUT', body: { group_ids: groupIds, excluded } })
}

export function getProfile(signal?: AbortSignal): Promise<ProfileView> {
  return request<ProfileView>('/api/profile', { signal })
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

export function getDigests(days = 7, signal?: AbortSignal): Promise<Digest> {
  return request<Digest>(`/api/digests?days=${days}`, { signal })
}

export function getDailyDigests(date: string, signal?: AbortSignal): Promise<{ items: DailyDigest[] }> {
  return request<{ items: DailyDigest[] }>(`/api/daily-digests?date=${encodeURIComponent(date)}`, { signal })
}

export function getMeetingReviews(date: string, signal?: AbortSignal): Promise<MeetingReviewList> {
  return request<MeetingReviewList>(`/api/review/meetings?date=${encodeURIComponent(date)}`, { signal })
}

export function getMorningBriefs(limit = 14, signal?: AbortSignal): Promise<{ items: MorningBrief[] }> {
  return request<{ items: MorningBrief[] }>(`/api/morning-briefs?limit=${limit}`, { signal })
}

export function generateDailyDigest(scope: DailyDigestScope, scopeId: string, date: string): Promise<DailyDigestKickResult> {
  return request<DailyDigestKickResult>('/api/daily-digests/generate', {
    method: 'POST',
    body: { scope, scope_id: scopeId, date },
  })
}

// date 为空时后端默认取今天（YYYY-MM-DD，本地时区）。
export function getCommitWorklog(date?: string, signal?: AbortSignal): Promise<CommitWorklog> {
  const query = date ? `?date=${date}` : ''
  return request<CommitWorklog>(`/api/worklog/commits${query}`, { signal })
}

export function getDocumentWorklog(date?: string, signal?: AbortSignal): Promise<DocumentWorklog> {
  const query = date ? `?date=${date}` : ''
  return request<DocumentWorklog>(`/api/worklog/documents${query}`, { signal })
}

// --- Debug panel ---

export function getDebugModules(signal?: AbortSignal): Promise<{ items: ModuleRun[] }> {
  return request<{ items: ModuleRun[] }>('/api/debug/modules', { signal })
}

export function getDebugAgentProcesses(signal?: AbortSignal): Promise<AgentProcessSnapshot> {
  return request<AgentProcessSnapshot>('/api/debug/agent-processes', { signal })
}

export function getDebugFailures(hours = 24, signal?: AbortSignal): Promise<{ items: FailureEvent[] }> {
  return request<{ items: FailureEvent[] }>(`/api/debug/failures?hours=${hours}`, { signal })
}

export function getDebugMonitoring(from: string, until: string, signal?: AbortSignal): Promise<MonitoringSnapshot> {
  const params = new URLSearchParams({ from, until })
  return request<MonitoringSnapshot>(`/api/debug/monitoring?${params.toString()}`, { signal })
}

export function getDebugWatermarks(signal?: AbortSignal): Promise<{ items: WatermarkRow[] }> {
  return request<{ items: WatermarkRow[] }>('/api/debug/watermarks', { signal })
}

export function getDebugProactiveRuns(limit = 50, signal?: AbortSignal): Promise<{ items: ProactiveRun[] }> {
  return request<{ items: ProactiveRun[] }>(`/api/debug/proactive-runs?limit=${limit}`, { signal })
}

export function getDebugProactiveRun(id: number, signal?: AbortSignal): Promise<ProactiveRunDetail> {
  return request<ProactiveRunDetail>(`/api/debug/proactive-runs/${id}`, { signal })
}

export function getSystemTaskRuns(job: string, limit = 100, signal?: AbortSignal): Promise<SystemTaskRunList> {
  const params = new URLSearchParams({ job, limit: String(limit) })
  return request<SystemTaskRunList>(`/api/system-tasks/runs?${params.toString()}`, { signal })
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

export function listResources(page = 1, pageSize = 100, signal?: AbortSignal): Promise<ResourceList> {
  return request<ResourceList>(`/api/resources?page=${page}&page_size=${pageSize}`, { signal })
}

export function listProjectResources(projectId: number, signal?: AbortSignal): Promise<ResourceList> {
  const params = new URLSearchParams({ page: '1', page_size: '100', project_id: String(projectId), active_only: 'true' })
  return request<ResourceList>(`/api/resources?${params.toString()}`, { signal })
}

export function createResource(body: ResourceInput): Promise<Resource> {
  return request<Resource>('/api/resources', { method: 'POST', body })
}

export function updateResource(id: number, body: ResourceInput): Promise<Resource> {
  return request<Resource>(`/api/resources/${id}`, { method: 'PUT', body })
}

export function touchResource(id: number): Promise<Resource> {
  return request<Resource>(`/api/resources/${id}/touch`, { method: 'POST' })
}

export function deleteResource(id: number): Promise<{ id: number; deleted: boolean }> {
  return request(`/api/resources/${id}`, { method: 'DELETE' })
}

export function listWorkRules(signal?: AbortSignal): Promise<{ items: WorkRule[] }> {
  return request<{ items: WorkRule[] }>('/api/work-rules', { signal })
}

export function updateWorkRule(key: WorkRule['key'], body: WorkRuleInput): Promise<WorkRule> {
  return request<WorkRule>(`/api/work-rules/${encodeURIComponent(key)}`, { method: 'PUT', body })
}

export function listTextFiles(signal?: AbortSignal): Promise<{ items: TextFile[] }> {
  return request<{ items: TextFile[] }>('/api/text-files', { signal })
}

export function updateTextFile(key: string, body: TextFileInput): Promise<TextFile> {
  return request<TextFile>(`/api/text-files/${encodeURIComponent(key)}`, { method: 'PUT', body })
}

export function getAgentConfigPreview(stage: AgentConfigStage, signal?: AbortSignal): Promise<AgentConfigPreview> {
  return request<AgentConfigPreview>(`/api/agent-config/stages/${encodeURIComponent(stage)}/preview`, { signal })
}

export function listScheduledTasks(status = '', signal?: AbortSignal, pluginID?: string): Promise<{ items: ScheduledTaskListItem[] }> {
  const params = new URLSearchParams({ limit: '200' })
  if (status) params.set('status', status)
  if (pluginID) params.set('plugin', pluginID)
  return request<{ items: ScheduledTaskListItem[] }>(`/api/scheduled-tasks?${params.toString()}`, { signal })
}

export function getScheduledTask(id: number, signal?: AbortSignal): Promise<ScheduledTask> {
  return request<ScheduledTask>(`/api/scheduled-tasks/${id}`, { signal })
}

export function createScheduledTask(body: ScheduledTaskInput): Promise<ScheduledTask> {
  return request<ScheduledTask>('/api/scheduled-tasks', { method: 'POST', body })
}

export function updateScheduledTask(id: number, body: ScheduledTaskInput): Promise<ScheduledTask> {
  return request<ScheduledTask>(`/api/scheduled-tasks/${id}`, { method: 'PUT', body })
}

export function deleteScheduledTask(id: number): Promise<{ id: number; deleted: boolean }> {
  return request(`/api/scheduled-tasks/${id}`, { method: 'DELETE' })
}

export function triggerScheduledTask(id: number): Promise<ScheduledTask> {
  return request<ScheduledTask>(`/api/scheduled-tasks/${id}/trigger`, { method: 'POST' })
}

export function listPlugins(signal?: AbortSignal): Promise<{ items: Plugin[] }> {
  return request<{ items: Plugin[] }>('/api/plugins', { signal })
}

export function getPlugin(id: string, signal?: AbortSignal): Promise<Plugin> {
  return request<Plugin>(`/api/plugins/${encodeURIComponent(id)}`, { signal })
}

export function updatePlugin(id: string, enabled: boolean, expectedRevision: number, config?: Record<string, unknown>): Promise<Plugin> {
  return request<Plugin>(`/api/plugins/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: { enabled, expected_revision: expectedRevision, ...(config === undefined ? {} : { config }) },
  })
}

export function authorizePlugin(id: string): Promise<PluginAuthorization> {
  return request<PluginAuthorization>(`/api/plugins/${encodeURIComponent(id)}/authorize`, { method: 'POST', timeoutMs: 45000 })
}

export function completePluginAuthorization(
  id: string,
  flowId: string,
): Promise<{ authorization: PluginAuthorization; plugin: Plugin | null }> {
  return request<{ authorization: PluginAuthorization; plugin: Plugin | null }>(
    `/api/plugins/${encodeURIComponent(id)}/authorize/complete`,
    { method: 'POST', body: { flow_id: flowId }, timeoutMs: 45000 },
  )
}

export function triggerPlugin(id: string): Promise<Plugin> {
  return request<Plugin>(`/api/plugins/${encodeURIComponent(id)}/trigger`, { method: 'POST' })
}

export async function findWorldProgress(
  subjectType: string,
  subjectId: string,
  periodKey: string,
  signal?: AbortSignal,
): Promise<WorldProgress | null> {
  const params = new URLSearchParams({
    subject_type: subjectType,
    subject_id: subjectId,
    period_key: periodKey,
  })
  try {
    return await request<WorldProgress>(`/api/world-progress?${params.toString()}`, { signal })
  } catch (cause) {
    if (cause instanceof APIRequestError && cause.status === 404) return null
    throw cause
  }
}

export function listWorldProgress(periodKey: string, signal?: AbortSignal): Promise<{ items: WorldProgress[] }> {
  return request<{ items: WorldProgress[] }>(`/api/world-progress/period/${encodeURIComponent(periodKey)}`, { signal })
}

export interface WorldProgressCreateInput {
  expected_version: 0
  subject_type: string
  subject_id: string
  period_key: string
  signal: WorldProgressSignal
  summary: string
  evidence: Record<string, unknown>
  evidence_until: string
}

export interface WorldProgressUpdateInput {
  expected_version: number
  signal: WorldProgressSignal
  summary: string
  evidence: Record<string, unknown>
  evidence_until: string
}

export function createWorldProgress(body: WorldProgressCreateInput): Promise<WorldProgress> {
  return request<WorldProgress>('/api/world-progress', { method: 'POST', body })
}

export function updateWorldProgress(id: number, body: WorldProgressUpdateInput): Promise<WorldProgress> {
  return request<WorldProgress>(`/api/world-progress/${id}`, { method: 'PUT', body })
}

export interface RelationQuery {
  sourceType?: string
  sourceId?: string
  relationType?: string
  targetType?: string
  targetId?: string
  nodeType?: string
  nodeId?: string
  nodeTypes?: readonly string[]
  limit?: number
}

export async function listRelations(query: RelationQuery, signal?: AbortSignal): Promise<{ items: EntityRelation[] }> {
  const params = new URLSearchParams({ limit: String(query.limit ?? 200) })
  if (query.sourceType) params.set('source_type', query.sourceType)
  if (query.sourceId) params.set('source_id', query.sourceId)
  if (query.relationType) params.set('relation_type', query.relationType)
  if (query.targetType) params.set('target_type', query.targetType)
  if (query.targetId) params.set('target_id', query.targetId)
  if (query.nodeType) params.set('node_type', query.nodeType)
  if (query.nodeId) params.set('node_id', query.nodeId)
  if (query.nodeTypes?.length) params.set('node_types', query.nodeTypes.join(','))

  const items: EntityRelation[] = []
  let cursor = ''
  do {
    if (cursor) params.set('cursor', cursor)
    const page = await request<{ items: EntityRelation[]; next_cursor?: string }>(`/api/relations?${params.toString()}`, { signal })
    items.push(...page.items)
    cursor = page.next_cursor ?? ''
  } while (cursor)
  return { items }
}

export function shutdownJarvis(): Promise<{ stopping: boolean }> {
  return request<{ stopping: boolean }>('/api/system/shutdown', { method: 'POST' })
}

export function listSkills(signal?: AbortSignal): Promise<{ items: AgentSkill[] }> {
  return request<{ items: AgentSkill[] }>('/api/skills', { signal })
}

export function scanSkills(): Promise<{ items: AgentSkill[] }> {
  return request<{ items: AgentSkill[] }>('/api/skills/scan', { method: 'POST' })
}

export function updateSkill(name: string, body: AgentSkillInput): Promise<AgentSkill> {
  return request<AgentSkill>(`/api/skills/${encodeURIComponent(name)}`, { method: 'PUT', body })
}

export function getSkillContent(name: string): Promise<AgentSkillContent> {
  return request<AgentSkillContent>(`/api/skills/${encodeURIComponent(name)}/content`)
}

export function getSkillSource(name: string): Promise<AgentSkillContent> {
  return request<AgentSkillContent>(`/api/skills/${encodeURIComponent(name)}/source`)
}

export function updateSkillSource(name: string, body: AgentSkillContentInput): Promise<AgentSkillContent> {
  return request<AgentSkillContent>(`/api/skills/${encodeURIComponent(name)}/source`, { method: 'PUT', body })
}

export function getRuntimeSettings(signal?: AbortSignal): Promise<RuntimeSettingsView> {
  return request<RuntimeSettingsView>('/api/runtime-settings', { signal })
}

export function getAgentIdentity(signal?: AbortSignal): Promise<AgentIdentity> {
  return request<AgentIdentity>('/api/agent-identity', { signal })
}

export function updateRuntimeSettings(body: RuntimeSettings): Promise<RuntimeSettingsView> {
  return request<RuntimeSettingsView>('/api/runtime-settings', { method: 'PUT', body })
}

export function listAppModules(signal?: AbortSignal): Promise<{ items: AppModule[] }> {
  return request<{ items: AppModule[] }>('/api/app-modules', { signal })
}

export function updateAppModule(key: string, body: AppModuleInput): Promise<AppModule> {
  return request<AppModule>(`/api/app-modules/${encodeURIComponent(key)}`, { method: 'PUT', body })
}

export function getSecuritySettings(signal?: AbortSignal): Promise<SecuritySettingsView> {
  return request<SecuritySettingsView>('/api/security-settings', { signal })
}

export function updateSecuritySettings(body: SecuritySettings): Promise<SecuritySettingsView> {
  return request<SecuritySettingsView>('/api/security-settings', { method: 'PUT', body })
}

export function listSecurityAuditEvents(
  query: {
    days: 1 | 7 | 30
    actorKind?: AccessAuditActorKind
    operation?: AccessAuditOperation
    route?: string
    resource?: string
    limit?: number
  },
  signal?: AbortSignal,
): Promise<AccessAuditEventList> {
  const params = new URLSearchParams({ days: String(query.days), limit: String(query.limit ?? 200) })
  if (query.actorKind) params.set('actor_kind', query.actorKind)
  if (query.operation) params.set('operation', query.operation)
  if (query.route?.trim()) params.set('route', query.route.trim())
  if (query.resource?.trim()) params.set('resource', query.resource.trim())
  return request<AccessAuditEventList>(`/api/security-audit-events?${params.toString()}`, { signal })
}

export function listPluginInstallations(signal?: AbortSignal): Promise<{ items: Array<Pick<Plugin, 'id' | 'name' | 'kind' | 'enabled'>> }> {
  return request('/api/plugin-installations', { signal })
}
export function listDelegations(state: string, query: string, page = 1, signal?: AbortSignal): Promise<{ items: Delegation[]; total: number }> {
  const params = new URLSearchParams({ state, query, page: String(page), page_size: '20' })
  return request(`/api/delegations?${params}`, { signal })
}
export function getDelegation(id: number, signal?: AbortSignal): Promise<Delegation> {
  return request(`/api/delegations/${id}`, { signal })
}
export function updateDelegation(id: number, expectedVersion: number, content: unknown, closed: boolean): Promise<Delegation> {
  return request(`/api/delegations/${id}`, { method: 'PATCH', body: { expected_version: expectedVersion, content, closed, actor: 'user' } })
}
export function listDelegationTasks(id: number, page = 1, signal?: AbortSignal): Promise<{ items: DelegationCheck[] }> {
  return request(`/api/delegations/${id}/tasks?page=${page}&page_size=20`, { signal })
}
