// M3 writes extracted/observing clues. The task materializer moves extracted to
// materialized after creating its Task.
export type TodoStatus =
  | 'extracted'
  | 'observing'
  | 'materialized'

export type ActionType = string

export interface TodoGroup {
  id: number
  chat_id: string
  name: string | null
}

export interface TodoProject {
  id: number
  code: string | null
  name: string
}

// Resolution is the M3-frozen trace of how the project/repo was inferred, shown
// so the user understands "why this project/repo".
export interface Resolution {
  method: 'group_bound' | 'project_hint' | 'codex_cli' | 'unresolved'
  project_id: number | null
  repo_path: string | null
  project_name: string | null
  repos_hint: string | null
  confidence: number | null
  basis: string | null
}

// ContextSnapshot is the M3-frozen background that M5 replays unchanged.
export interface ContextSnapshot {
  snapshot_version: string
  captured_at: string
  principal: {
    open_id: string
    name: string
    department: string | null
    title: string | null
    leader_name: string | null
  } | null
  project: {
    id: number
    code: string | null
    name: string
    role: string
    description: string | null
    repos: unknown
    key_decisions: unknown
  } | null
  group: {
    id: number
    chat_id: string
    name: string | null
    description: string | null
    background_note: string | null
  } | null
  assigner: {
    open_id: string
    name: string | null
    role: string | null
    relation: string | null
  } | null
  messages: Array<{
    message_id: string
    sender_name: string
    content: string
    create_time: number
  }>
  memories: Array<Record<string, unknown>>
  supplements?: Array<{ note: string; at: string }>
}

export interface Todo {
  id: number
  title: string
  description: string
  action_type: ActionType
  target: string
  context: string
  open_questions: string[] | null
  commitment_strength: 'firm' | 'tentative' | 'mentioned'
  source_message_ids: string[]
  source_quote: string
  assigner_open_id: string | null
  is_leader_assigned: boolean
  due_at: string | null
  status: TodoStatus
  revision: number
  version: number
  first_seen_at: string
  last_evidence_at: string
  created_at: string
  updated_at: string
  group: TodoGroup | null
  project: TodoProject | null
  resolution: Resolution | null
  context_snapshot: ContextSnapshot | null
}

export interface TodoList {
  items: Todo[]
  total: number
  page: number
  page_size: number
}

export interface TodoQuery {
  statuses: TodoStatus[]
  actionType?: ActionType
  leaderOnly: boolean
  page: number
  pageSize: number
}

// observing is terminal like done/failed: M5 investigated and found the matter
// real but asking nothing of anyone, so it changed nothing and nothing went
// wrong. The originating clue goes back to observing with it.
export type TaskStatus = 'pending' | 'executing' | 'waiting' | 'needs_human' | 'awaiting_approval' | 'done' | 'failed' | 'observing'

// TaskProposal is the controlled side effect Codex prepared during execution,
// awaiting human approval. It is stored in execution_result while the Task sits
// at awaiting_approval (stage="proposal").
export interface TaskProposal {
  action: string
  target: string
  artifact: string
}

// ProposalResult is the shape of execution_result while a Task is awaiting_approval.
export interface ProposalResult {
  stage: 'proposal'
  action_type?: string
  summary?: string
  proposal: TaskProposal
  needs_followup?: string
  enrichments?: RunEnrichment[]
  codex_session_id?: string
}

export interface Task {
  id: number
  todo_id: number | null
  title: string
  action_type: ActionType
  target: string
  background: Record<string, unknown>
  source_payload: unknown
  status: TaskStatus
  execution_result: Record<string, unknown> | null
  // Where the matter itself now stands, spanning every run. Distinct from a run's
  // own summary, which covers only that attempt.
  summary: string | null
  // Moves only when summary actually changes, so a Task that keeps resuming
  // without moving does not look alive.
  last_progress_at: string | null
  execution_supplements?: Array<{ note: string; at: string; channel?: string }>
  project_id: number | null
  source_type: 'todo' | 'scheduled_task' | 'manual' | 'proactive'
  source_id: number | null
  occurrence_key: string | null
  version: number
  created_at: string
  updated_at: string
  // Projection of the latest terminal TaskEvent. task_event remains the audit
  // truth; this lets list views distinguish human and model resolution.
  resolution: {
    event_type: string
    actor_type: string
    actor_ref: string | null
    occurred_at: string
  } | null
}

export interface TaskList {
  items: Task[]
  total: number
  page: number
  page_size: number
}

export interface CreateTaskInput {
  title: string
  action_type: 'agent_task'
  target: string
  background: Record<string, unknown>
  source_payload: unknown
  project_id?: number
}

export interface CreateTaskResult {
  id: number
  todo_id: number | null
  title: string
  action_type: ActionType
  target: string
  status: TaskStatus
  source_type: 'manual'
  source_id: number | null
  occurrence_key: string | null
  version: number
}

// RunEnrichment 是 codex 主动"多做一步"准备的一条开放语义块：
//   kind=context      正文/结论段落（如"会议一页纸"）
//   kind=doc_link     引用的文件/文档路径
//   kind=code_link    引用的代码位置
//   kind=commit_digest 仓库 commit 摘要
// kind/label 只服务轻量展示；content 可以是任意 JSON。未知 kind 使用通用
// JSON/Text renderer 展示，不能丢信息。
export interface RunEnrichment {
  kind: string
  label: string
  content: unknown
}

// Effect 是 agent 申报的一条"对外真实影响"（发了飞书消息、建了文档、约了会、
// 提了 MR、申请了权限……）。这是一个**开放、只展示不核验**的载荷：kind 是自由
// 字符串，agent 可以自造新类型；除下面几个已知字段外的任何额外字段都会被原样
// 保留并友好展示，未知 kind / 未知字段绝不丢弃、绝不报错。因此类型上用宽松的
// 索引签名兜底，已知字段只是可选的“建议字段”。
export interface Effect {
  kind: string
  title?: string
  url?: string
  target?: string
  preview?: string
  [key: string]: unknown
}

// RunOutput 是 execution_run.output 的强类型：codex 执行结束时输出的结构化裁决。
// summary 已单独存在 ExecutionRun.summary，这里主要用 needs_followup 与 enrichments。
export interface RunOutput {
  outcome?: 'completed' | 'observing' | 'waiting' | 'needs_human' | 'failed'
  summary?: string
  failure_reason?: string
  needs_followup?: string
  enrichments?: RunEnrichment[]
  effects?: Effect[]
  waiting?: {
    scheduled_task_id: number
    wake_at: string
    reason: string
  } | null
}

// ExecutionRun 是一次 M5 执行的审计记录，一个 Task 可有多条（重试）。
export interface ExecutionRun {
  id: number
  task_id: number
  action_type: ActionType
  // propose remains readable for historical runs created before execute became
  // the single initial stage.
  stage: 'execute' | 'propose' | 'apply'
  sandbox: string
  status: string
  prompt: string
  codex_session_id: string | null
  summary: string | null
  output: RunOutput | null
  effects: Effect[] | null
  error_detail: string | null
  repo_path: string | null
  started_at: string
  finished_at: string | null
  duration_ms: number | null
}

export interface ExecutionRunList {
  items: ExecutionRun[]
}

export interface TaskRunOutput {
  task_id: number
  task_status: TaskStatus
  available: boolean
  running: boolean
  run_key?: string
  // propose is a historical run value; new initial runs use execute.
  stage?: 'execute' | 'propose' | 'apply'
  prompt?: string
  stdout?: string
  stderr?: string
  updated_at?: string
}

export interface TaskEvent {
  id: number
  task_id: number
  task_version: number
  event_type: string
  from_status: TaskStatus | null
  to_status: TaskStatus
  actor_type: string
  actor_ref: string | null
  run_id: number | null
  detail: unknown
  occurred_at: string
  created_at: string
}

export interface Fact {
  id: number
  subject_type: string
  subject_id: number
  description: string
  occurred_at: string
  source_kind: string | null
  source_id: number | null
  created_at: string
}

export interface LabeledFact extends Fact {
  subject_label: string
}

export type FactRollupState = 'fresh' | 'stale' | 'missing'

export interface FactSubjectDay {
  subject_type: string
  subject_id: number
  subject_label: string
  rollup: LabeledFact | null
  rollup_state: FactRollupState
  detail_count: number
  late_detail_count: number
  latest_occurred_at: string
}

export interface FactTimelineDay {
  date: string
  is_today: boolean
  detail_count: number
  details: LabeledFact[]
  subjects: FactSubjectDay[]
}

export interface FactTimeline {
  timezone: string
  days: FactTimelineDay[]
}

export interface FactSearchResult {
  items: LabeledFact[]
  total: number
  page: number
  page_size: number
}

export interface FactSearchQuery {
  q?: string
  from?: string
  until?: string
  subjectType?: string
  subjectId?: number
  sourceKind?: string
  layer?: 'all' | 'detail' | 'rollup'
  page?: number
  pageSize?: number
}

export type RelationEntityType = 'project' | 'key_matter' | 'person' | 'principal' | 'group' | 'todo' | 'task' | 'resource' | 'managed_resource'

export interface RelationEntityRef {
  type: RelationEntityType
  id: number
  label: string
}

export interface RelationFact {
  id: number
  entity_a: RelationEntityRef
  entity_b: RelationEntityRef
  description: string
  /** 关系成立的起点；null 表示起点未知 */
  valid_from: string | null
  /** 关系成立的终点；null 表示关系仍然有效 */
  valid_until: string | null
  created_at: string
  updated_at: string
}

export interface RelationFactList {
  items: RelationFact[]
  total: number
  page: number
  page_size: number
}


export type ProjectRole = 'owner' | 'participant'
export type ProjectStatus = 'planning' | 'active' | 'paused' | 'archived' | 'done'
export type PersonRole = 'leader' | 'key' | 'colleague' | 'other'

export interface Project {
  id: number
  code: string | null
  name: string
  role: ProjectRole
  status: ProjectStatus
  priority: number
  description: string | null
  repos: unknown
  tech_stack: unknown
  key_decisions: unknown
  timeline: unknown
  notes: string | null
  created_at: string
  updated_at: string
}

export interface KeyMatter {
  id: number
  title: string
  status: string
  summary: string | null
  project_id: number | null
  due_at: string | null
  closed_at: string | null
  last_progress_at: string | null
  last_active_at: string
  created_at: string
  updated_at: string
  project: Project | null
}

export interface Person {
  id: number
  open_id: string
  union_id: string | null
  feishu_user_id: string | null
  name: string
  en_name: string | null
  avatar_url: string | null
  department: string | null
  title: string | null
  role: PersonRole
  priority_weight: number
  relation: string | null
  comm_style: string | null
  p2p_chat_id: string | null
  notes: string | null
  is_active: boolean
  created_at: string
  updated_at: string
}

export interface Group {
  id: number
  chat_id: string
  chat_mode: string
  name: string | null
  description: string | null
  background_note: string | null
  owner_open_id: string | null
  external: boolean
  tenant_key: string | null
  project_id: number | null
  related_group: boolean
  tier: string
  pinned: boolean
  include_in_memory: boolean
  is_key_group: boolean
  last_active_at: number | null
  created_at: string
  updated_at: string
  project: Project | null
  last_scan_at: string | null
  last_scan_status: string | null
  message_count: number
}

export interface GroupQuery {
  page: number
  pageSize: number
  relatedOnly: boolean
  keyOnly?: boolean
  keyword?: string
  chatMode?: string
  tier?: string
}

export interface Paged<T> {
  items: T[]
  total: number
  page: number
  page_size: number
}

export interface KeyMatterList extends Paged<KeyMatter> {
  max_open: number
}

export interface ResourceList extends Paged<Resource> {
  active_total: number
  max_active: number
}

// GroupList adds broadened: the backend sets it when a keyword search escaped
// the related-only view (searched all chats, not just monitored ones).
export interface GroupList extends Paged<Group> {
  broadened: boolean
}

export interface ProjectInput {
  code?: string | null
  name: string
  role: ProjectRole
  status: ProjectStatus
  priority: number
  description?: string | null
  notes?: string | null
}

export interface KeyMatterInput {
  title: string
  status: string
  summary: string | null
  project_id: number | null
  due_at: string | null
}

export interface PersonUpdateInput {
  name: string
  role: PersonRole
  priority_weight: number
  department?: string | null
  title?: string | null
  relation?: string | null
  comm_style?: string | null
  notes?: string | null
  is_active?: boolean
}

export interface PersonCreateInput extends PersonUpdateInput {
  open_id: string
  union_id?: string | null
  feishu_user_id?: string | null
  en_name?: string | null
  avatar_url?: string | null
  p2p_chat_id?: string | null
}

export interface ResolveCandidate {
  open_id: string
  name: string
  email: string
  department: string
  p2p_chat_id: string
  is_external: boolean
  has_chatted: boolean
}

export interface ResolveResult {
  candidates: ResolveCandidate[]
  has_more: boolean
}

export interface GroupBackgroundInput {
  background_note?: string | null
  project_id: number | null
  related_group: boolean
  pinned: boolean
  include_in_memory: boolean
  is_key_group: boolean
}

// ProfileView is the principal ("me") background. open_id is fixed by
// backend config; saved=false means the row has not been filled yet.
// SharedMemory 是全局单例的「共享记忆」大文本视图，对齐后端 sharedmem.SharedMemoryView。
export interface SharedMemory {
  content: string
  path: string
  modified_at: string
  saved: boolean
}

export interface ProfileView {
  open_id: string
  name: string
  department?: string | null
  title?: string | null
  background?: string | null
  preferences?: string | null
  leader_open_id?: string | null
  leader_name?: string | null
  saved: boolean
}

export interface ProfileInput {
  name: string
  department?: string | null
  title?: string | null
  background?: string | null
  preferences?: string | null
  leader_open_id?: string | null
  leader_name?: string | null
}

export interface StatusCount {
  status: string
  count: number
}

// Overview is the dashboard aggregation (live counts, no cache).
export interface Overview {
  todos: {
    total: number
    open: number
    leader_open: number
    by_status: StatusCount[]
  }
  tasks: {
    total: number
    pending: number
    needs_me: number
    done: number
    failed: number
    by_status: StatusCount[]
  }
}

export interface MyDay {
  date: string
  todos_created: number
  tasks_created: number
  tasks_done: number
  tasks_failed: number
}

export interface GroupDay {
  date: string
  messages: number
  todos_extracted: number
}

export interface GroupProgress {
  group_id: number
  chat_id: string
  name: string
  days: GroupDay[]
}

// Digest is the Progress tab payload: per-day timeline over `days` days.
export interface Digest {
  days: number
  mine: MyDay[]
  key_groups: GroupProgress[]
}

export type DailyDigestScope = 'person' | 'group'
export type DailyDigestStatus = 'pending' | 'generating' | 'done' | 'failed'
export type DailyDigestTrigger = 'manual' | 'schedule'

export interface DailyDigestSourceCoverage {
  status: 'ok' | 'complete' | 'partial' | 'empty' | 'error' | 'unavailable'
  count: number
  note?: string
}

// DailyDigest 是某个自然日的一条内容总结；同一天 person 一条、每个关键群一条。
export interface DailyDigest {
  id: number
  scope: DailyDigestScope
  scope_id: string
  digest_date: string
  summary: string
  status: DailyDigestStatus
  trigger_type: DailyDigestTrigger
  source_count: number
  source_coverage: Record<string, DailyDigestSourceCoverage>
  engine: 'codex'
  error_detail: string | null
  started_at: string | null
  cutoff_at: string | null
  generated_at: string | null
  updated_at: string
}

export interface DailyDigestKickResult {
  scope: DailyDigestScope
  scope_id: string
  date: string
  status: 'generating'
  trigger_type: 'manual'
}

export interface MeetingReviewItem {
  meeting_id: string
  title: string
  occurred_at: string
  start_at: string
  end_at: string
  host: string
  participants: string
  meeting_url: string
  task_id: number | null
  task_status: string
  summary: string
  summary_generated_at: string | null
  effects: Array<Record<string, unknown>>
}

export interface MeetingReviewList {
  date: string
  items: MeetingReviewItem[]
}

// MorningBrief is the canonical Markdown artifact for one local day.
export interface MorningBrief {
  date: string
  content: string
  generated_at: string
}

// --- 进度页「今天的文档」「项目代码」两个 Tab ---

// CommitMR 是我在某仓库的一条 MR（字节 protected-branch 走 MR，以 MR 为提交粒度）。
export interface CommitMR {
  title: string
  url: string
  status: string // open | merged | closed
  commits_count: number
  changes_count: number
  created_at: string
  updated_at: string
  merged_at: string | null
  target_branch: string
  check_run_summary: string
}

export interface CommitRepo {
  repo: string // 形如 team/jarvis_bot
  mrs: CommitMR[]
}

export interface CommitWorklog {
  date: string
  repos: CommitRepo[]
}

// WorkDoc 是一条文档：我写的（编辑时间）或我收到的（采集时间 + 来源群/人）。
export interface WorkDoc {
  title: string
  url: string
  doc_type: string
  time: string
  from_who?: string
  from_chat?: string
}

export interface DocumentWorklog {
  date: string
  authored: WorkDoc[]
  received: WorkDoc[]
}

export interface ModuleRun {
  module: string
  time: string
  status: string
  current_ok: boolean
  job: string
  fields: Record<string, string>
  runs: number
  failures: number
  last_error: string
  raw: string
}

export interface AgentProcess {
  kind: 'codex' | 'trae'
  mode: 'exec' | 'app-server' | 'cli' | 'desktop'
  source: 'jarvis' | 'cc-connect' | 'paseo' | 'chatgpt' | 'trae' | 'other'
  pid: number
  ppid: number
  pgid: number
  root_pid: number
  nested: boolean
  elapsed: string
  jarvis_owned: boolean
  command: string
}

export interface AgentProcessSnapshot {
  sampled_at: string
  summary: {
    codex_services: number
    codex_executing: number
    trae_desktop: number
    trae_cli: number
    jarvis_codex: number
    jarvis_trae: number
  }
  items: AgentProcess[]
}

export interface FailureEvent {
  time: string
  module: string
  stage: string
  job: string
  trigger: string
  scope_type: string
  scope_id: string
  logid: string
  error: string
  count: number
  recovered: boolean
  raw: string
}

export interface ScanRow {
  id: number
  scan_type: string
  chat_id: string | null
  status: string
  fetched_count: number
  inserted_count: number
  error_type: string | null
  error_message: string | null
  started_at: string
  duration_ms: number | null
}

export interface WatermarkRow {
  chat_id: string
  group_name: string
  last_message_id: string
  last_message_content: string
  last_scanned_at: string
  updated_at: string
}

export interface SystemTaskRun {
  time: string
  source: string
  module: string
  job: string
  status: string
  duration_ms: number | null
  fields: Record<string, string>
  raw: string
}

export interface SystemTaskRunList {
  items: SystemTaskRun[]
  sources: string[]
  truncated: boolean
  notes: string[]
}

export interface ProactiveRun {
  id: number
  trigger_type: 'schedule' | 'manual'
  engine: string
  model: string
  status: 'running' | 'succeeded' | 'failed'
  error_detail: string | null
  started_at: string
  finished_at: string | null
  duration_ms: number | null
}

export interface ProactiveRunDetail extends ProactiveRun {
  input: string
  output: string | null
}

export interface MonitoringSnapshot {
  from: string
  until: string
  bucket: string
  m2: {
    inserted_messages: number
    run_count: number
    average_duration_ms: number | null
    max_duration_ms: number | null
    finished_runs: number
    failed_runs: number
    failure_rate: number | null
    recorded_since: string | null
    series: MonitoringPoint[]
  }
  m3: {
    chat_count: number
    run_count: number
    processed_messages: number
    todos_created: number
    average_duration_ms: number | null
    max_duration_ms: number | null
    total_tokens: number | null
    token_coverage_complete: boolean
    finished_runs: number
    failed_runs: number
    failure_rate: number | null
    recorded_since: string | null
    series: MonitoringPoint[]
  }
  m5: {
    processed_tasks: number
    run_count: number
    average_duration_ms: number | null
    max_duration_ms: number | null
    total_tokens: number | null
    token_coverage_complete: boolean
    finished_runs: number
    failed_runs: number
    failure_rate: number | null
    recorded_since: string | null
    series: MonitoringPoint[]
  }
}

export interface MonitoringPoint {
  bucket_start: string
  recording_active: boolean
  scope_count: number
  run_count: number
  average_duration_ms: number | null
  finished_runs: number
  failed_runs: number
  failure_rate: number | null
  total_tokens: number | null
  token_coverage_complete: boolean
}

export type ResourceType = 'doc' | 'link' | 'repo' | 'note' | 'other'

// Resource is a manually curated reference that can be linked to a person, a
// project, and/or the principal ("me"). Distinct from message-derived resources.
export interface Resource {
  id: number
  title: string
  resource_type: ResourceType
  url: string | null
  description: string | null
  person_id: number | null
  person_name: string | null
  project_id: number | null
  project_name: string | null
  link_principal: boolean
  is_active: boolean
  last_active_at: string
}

export interface ResourceInput {
  title: string
  resource_type: ResourceType
  url?: string | null
  description?: string | null
  person_id?: number | null
  project_id?: number | null
  link_principal: boolean
  is_active?: boolean
}

export type WorkRuleStage = 'extract' | 'execute'

export interface WorkRule {
  key: WorkRuleStage
  name: string
  path: string
  content: string
}

export interface WorkRuleInput {
  content: string
}

export interface TextFile {
  key: string
  name: string
  description: string
  kind: 'system_prompt' | 'approval_policy'
  stage: string
  path: string
  content: string
}

export interface TextFileInput {
  content: string
}

export type AgentConfigStage = 'm3' | 'm5'

export interface AgentConfigPreview {
  stage: AgentConfigStage
  name: string
  content: string
  dynamic_blocks: string[]
}

export type ScheduledTaskStatus = 'binding' | 'active' | 'running' | 'completed'
export type ScheduledTaskLastRunStatus = 'done' | 'failed'
export type ScheduledTaskScheduleType = 'once' | 'daily' | 'interval'

export interface ScheduledTask {
  id: number
  dispatch_kind: 'create_task' | 'resume_task'
  subject_type: string | null
  subject_id: number | null
  source_run_id: number | null
  dispatch_payload: Record<string, unknown> | null
  title: string
  action_type: 'agent_task'
  instruction: string
  context_snapshot: Record<string, unknown>
  schedule_type: ScheduledTaskScheduleType
  daily_time: string | null
  interval_minutes: number | null
  run_at: string | null
  next_run_at: string
  enabled: boolean
  status: ScheduledTaskStatus
  last_run_status: ScheduledTaskLastRunStatus | null
  last_task_id: number | null
  last_result: string | null
  last_error_detail: string | null
  last_started_at: string | null
  last_finished_at: string | null
  created_at: string
  updated_at: string
}

export interface ScheduledTaskInput {
  title: string
  action_type: 'agent_task'
  instruction: string
  context_snapshot: Record<string, unknown>
  schedule_type: ScheduledTaskScheduleType
  daily_time: string | null
  interval_minutes: number | null
  run_at: string | null
  enabled: boolean
}

export type SkillStage = WorkRuleStage

export interface AgentSkill {
  name: string
  description: string
  file_path: string
  stages: SkillStage[]
  is_enabled: boolean
}

export interface AgentSkillInput {
  stages: SkillStage[]
  is_enabled: boolean
}

export interface AgentSkillContent {
  name: string
  path: string
  content: string
}

export type AgentCLI = 'codex' | 'traex'
export type ReasoningEffort = 'minimal' | 'low' | 'medium' | 'high' | 'xhigh'

export interface RuntimeSettings {
  analysis_cli: AgentCLI
  analysis_model: string
  analysis_timeout_seconds: number
  model_api_model: string
  model_api_timeout_seconds: number

  extract_enabled: boolean
  extract_engine: 'codex' | 'model_api'
  extract_schedule: string
  extract_concurrency: number
  extract_batch_messages: number
  extract_sandbox: 'read-only' | 'workspace-write' | 'danger-full-access'
  extract_network_enabled: boolean
  extract_reasoning_effort: ReasoningEffort
  extract_context_messages: number
  extract_context_window_minutes: number
  extract_open_todo_limit: number
  extract_fact_limit: number
  extract_key_person_limit: number
  extract_recent_task_limit: number
  extract_max_prompt_chars: number
  extract_semantic_threshold: number
  extract_semantic_neighbor_limit: number
  extract_tool_timeout_seconds: number
  extract_history_tool_limit: number
  extract_evidence_retry_max: number

  execute_auto_enabled: boolean
  execute_cli: AgentCLI
  execute_model: string
  execute_reasoning_effort: ReasoningEffort
  execute_schedule: string
  execute_batch_limit: number
  execute_timeout_seconds: number
  execute_stale_minutes: number
  execute_concurrency: number

  chat_enabled: boolean
  chat_model: string
  chat_sandbox: 'read-only' | 'workspace-write' | 'danger-full-access'
  chat_reasoning_effort: ReasoningEffort
  chat_timeout_seconds: number

  capture_page_size: number
  capture_scan_workers: number
  capture_discover_schedule: string
  capture_scan_schedule: string
  capture_auto_related_p2p_top_n: number

  fact_engine_enabled: boolean
  fact_engine_schedule: string
  fact_engine_rollup_schedule: string
  fact_engine_model: string
  fact_engine_rollup_model: string
  fact_engine_timeout_seconds: number
  fact_engine_batch_limit: number
  fact_engine_max_material_chars: number
  fact_engine_window_gap_minutes: number
  fact_engine_window_max_messages: number
  proactive_enabled: boolean
  proactive_schedule: string
  proactive_startup_delay_seconds: number
  proactive_cli: string
  proactive_model: string
  proactive_sandbox: string
  proactive_reasoning_effort: string
  proactive_timeout_seconds: number

  lark_rate_limit: number
  lark_burst: number
  lark_concurrency: number
  lark_timeout_seconds: number

  scheduled_task_enabled: boolean
  scheduled_task_schedule: string
  scheduled_task_batch_limit: number
  daily_digest_enabled: boolean
  daily_digest_schedule: string
  daily_digest_message_limit: number
  daily_digest_concurrency: number
}

export interface RuntimeSettingsView {
  settings: RuntimeSettings
  restart_required: boolean
  override_path: string
}

// --- codex 对话框契约（跨 agent 冻结，A/B/C 共用）---

// PageContext 是右侧对话框对左侧页面的单向感知：当前所在 Tab + 选中项摘要。
// 由各页面写入 PageContext（React Context），发送对话时随请求带给后端注入 prompt。
export interface PageContext {
  // 当前左侧导航 key：overview/todos/tasks/scheduled-tasks/background/settings/progress/debug
  active_key: string
  // 当前选中项的可读摘要（如 "Todo #12 修复登录超时"）；无选中则 null
  selection: PageSelection | null
  // 当前页面的页内视图、筛选和日期等宽松状态。页面自行写入，聊天只读取，
  // 不为不同页面复制一套严格 DTO。
  view_state: Record<string, string>
}

export interface PageSelection {
  // 选中项类型：todo/task/project/person/group/resource
  kind: string
  id: number
  // 一行可读摘要，直接进 prompt
  label: string
}

// POST /api/chat 请求体。thread_id 为空表示新会话；非空表示 codex resume 多轮。
export interface ChatRequest {
  message: string
  thread_id?: string | null
  page_context?: PageContext | null
}

// SSE 事件类型（event 字段）：
//   'thread'  data={thread_id}         —— 会话建立/恢复，前端记住以便多轮 resume
//   'delta'   data={text}              —— codex 增量输出，前端追加渲染
//   'done'    data={}                  —— 本轮结束，可关闭流
//   'error'   data={message}           —— 出错（fail-fast，前端直接展示）
export type ChatEventType = 'thread' | 'delta' | 'done' | 'error'

export interface ChatThreadEvent {
  thread_id: string
}

export interface ChatDeltaEvent {
  text: string
}

export interface ChatErrorEvent {
  message: string
}
