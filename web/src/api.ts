import type {
  AgentSkill,
  AgentSkillContent,
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
  ScanRow,
  WatermarkRow,
  GroupBackgroundInput,
  GroupList,
  GroupQuery,
  KeyMatter,
  KeyMatterInput,
  KeyMatterList,
  Overview,
  Paged,
  Person,
  PersonCreateInput,
  PersonUpdateInput,
  ProfileInput,
  ProfileView,
  Project,
  ProjectInput,
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
  TaskRunOutput,
  Fact,
  FactSearchQuery,
  FactSearchResult,
  FactTimeline,
  RelationEntityType,
  RelationFactList,
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
  RuntimeSettings,
  RuntimeSettingsView,
  ProactiveRun,
  ProactiveRunDetail,
  MonitoringSnapshot,
  SystemTaskRunList,
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

// 只在 observing 和 extracted 之间搬动：把线索按下不表，或重新交给 Task 固化与执行流水线。
export function setTodoStatus(id: number, status: TodoStatus, reason: string): Promise<Todo> {
  return request<Todo>(`/api/todos/${id}/status`, {
    method: 'PATCH',
    body: { status, actor: 'principal', reason },
  })
}

export function listTasks(statuses: TaskStatus[], page = 1, pageSize = 20, signal?: AbortSignal): Promise<TaskList> {
  const params = new URLSearchParams({ status: statuses.join(','), page: String(page), page_size: String(pageSize) })
  return request<TaskList>(`/api/tasks?${params.toString()}`, { signal })
}

export function getTask(id: number, signal?: AbortSignal): Promise<Task> {
  return request<Task>(`/api/tasks/${id}`, { signal })
}

export function createTask(body: CreateTaskInput): Promise<CreateTaskResult> {
  return request<CreateTaskResult>('/api/tasks', { method: 'POST', body })
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

// approveTask lands a proposal the user accepted: the awaiting_approval Task runs
// the apply stage (a fresh codex run carrying the approved proposal) for real.
export function approveTask(id: number, expectedVersion: number): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/approve`, {
    method: 'POST', body: { expected_version: expectedVersion },
  })
}

// rejectTask declines a proposed external write: the Task moves to failed with an
// optional reason; it can later be rerun to investigate again and form a new proposal.
export function rejectTask(id: number, expectedVersion: number, reason: string): Promise<ExecuteResult> {
  return request<ExecuteResult>(`/api/tasks/${id}/reject`, {
    method: 'POST', body: { expected_version: expectedVersion, reason },
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
export function listTaskRuns(id: number, signal?: AbortSignal): Promise<ExecutionRunList> {
  return request<ExecutionRunList>(`/api/tasks/${id}/runs`, { signal })
}

export function getTaskRunOutput(id: number, signal?: AbortSignal): Promise<TaskRunOutput> {
  return request<TaskRunOutput>(`/api/tasks/${id}/output`, { signal })
}

export function listTaskEvents(id: number, signal?: AbortSignal): Promise<{ items: TaskEvent[] }> {
  return request<{ items: TaskEvent[] }>(`/api/tasks/${id}/events`, { signal })
}

export function listEntityRelations(entityType: RelationEntityType, entityId: number, signal?: AbortSignal): Promise<RelationFactList> {
  const params = new URLSearchParams({
    entity_type: entityType,
    entity_id: String(entityId),
    page: '1',
    page_size: '100',
  })
  return request<RelationFactList>(`/api/relation-facts?${params.toString()}`, { signal })
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

export function listKeyMatters(page = 1, pageSize = 100, includeClosed = false, signal?: AbortSignal): Promise<KeyMatterList> {
  const params = new URLSearchParams({ page: String(page), page_size: String(pageSize) })
  if (includeClosed) params.set('include_closed', 'true')
  return request<KeyMatterList>(`/api/key-matters?${params.toString()}`, { signal })
}

export function createKeyMatter(body: KeyMatterInput): Promise<KeyMatter> {
  return request<KeyMatter>('/api/key-matters', { method: 'POST', body })
}

export function getKeyMatter(id: number, signal?: AbortSignal): Promise<KeyMatter> {
  return request<KeyMatter>(`/api/key-matters/${id}`, { signal })
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

export function listProjectFacts(id: number, signal?: AbortSignal): Promise<{ items: Fact[] }> {
  return listSubjectFacts('project', id, signal)
}

export function listSubjectFacts(subjectType: string, id: number, signal?: AbortSignal, options: {
  from?: string
  until?: string
  sourceKind?: string
  excludeSourceKind?: string
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
  if (options.excludeSourceKind) params.set('exclude_source_kind', options.excludeSourceKind)
  return request<{ items: Fact[] }>(`/api/facts?${params.toString()}`, { signal })
}

export function appendProjectFact(id: number, description: string): Promise<Fact> {
	return appendFact({ subject_type: 'project', subject_id: id, description })
}

export function appendFact(body: {
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
    layer: query.layer ?? 'all',
  })
  if (query.q) params.set('q', query.q)
  if (query.from) params.set('from', query.from)
  if (query.until) params.set('until', query.until)
  if (query.subjectType) params.set('subject_type', query.subjectType)
  if (query.subjectId) params.set('subject_id', String(query.subjectId))
  if (query.sourceKind) params.set('source_kind', query.sourceKind)
  return request<FactSearchResult>(`/api/facts/search?${params.toString()}`, { signal })
}

export function generateFactRollup(date: string, subject?: { type: string; id: number }): Promise<{
  Subjects: number
  Batches: number
  FailedBatches: number
  Written: number
  Skipped: number
  Day: string
}> {
  return request('/api/fact-rollups/generate', {
    method: 'POST',
    body: subject ? { date, subject_type: subject.type, subject_id: subject.id } : { date },
  })
}

export function listPersons(page = 1, pageSize = 100, signal?: AbortSignal): Promise<Paged<Person>> {
  return request<Paged<Person>>(`/api/persons?page=${page}&page_size=${pageSize}`, { signal })
}

export function resolvePerson(query: string): Promise<ResolveResult> {
  return request<ResolveResult>('/api/persons/resolve', { method: 'POST', body: { query } })
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

export function getDebugScans(limit = 50, signal?: AbortSignal): Promise<{ items: ScanRow[] }> {
  return request<{ items: ScanRow[] }>(`/api/debug/scans?limit=${limit}`, { signal })
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

export function getTextFile(key: string, signal?: AbortSignal): Promise<TextFile> {
  return request<TextFile>(`/api/text-files/${encodeURIComponent(key)}`, { signal })
}

export function updateTextFile(key: string, body: TextFileInput): Promise<TextFile> {
  return request<TextFile>(`/api/text-files/${encodeURIComponent(key)}`, { method: 'PUT', body })
}

export function getAgentConfigPreview(stage: AgentConfigStage, signal?: AbortSignal): Promise<AgentConfigPreview> {
  return request<AgentConfigPreview>(`/api/agent-config/stages/${encodeURIComponent(stage)}/preview`, { signal })
}

export function listScheduledTasks(status = '', signal?: AbortSignal): Promise<{ items: ScheduledTask[] }> {
  const params = new URLSearchParams({ limit: '200' })
  if (status) params.set('status', status)
  return request<{ items: ScheduledTask[] }>(`/api/scheduled-tasks?${params.toString()}`, { signal })
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

export function getRuntimeSettings(signal?: AbortSignal): Promise<RuntimeSettingsView> {
  return request<RuntimeSettingsView>('/api/runtime-settings', { signal })
}

export function updateRuntimeSettings(body: RuntimeSettings): Promise<RuntimeSettingsView> {
  return request<RuntimeSettingsView>('/api/runtime-settings', { method: 'PUT', body })
}
