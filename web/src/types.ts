export type TodoStatus =
  | 'extracted'
  | 'scoring'
  | 'auto'
  | 'need_info'
  | 'need_decision'
  | 'confirmed'
  | 'dismissed'
  | 'dropped'
  | 'expired'

export type ActionType =
  | 'code_change'
  | 'summary_post'
  | 'investigate'
  | 'schedule_meeting'
  | 'reply_message'
  | 'doc_write'
  | 'manual_followup'

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
  project_name: string | null
  repos_hint: string | null
  confidence: number | null
  basis: string | null
}

// ContextSnapshot is the M3-frozen background that M4/M5 replay unchanged.
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
  confidence: number | null
  risk: number | null
  route: string | null
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

export interface ConfirmationMessage {
  message_id: string
  sender_open_id: string
  sender_name: string
  content: string
  create_time: number
}

export interface ConfirmationAssigner {
  open_id: string
  name: string | null
  role: string | null
  title: string | null
}

export interface ProposedPlan {
  summary: string
  steps: string[]
  parameters: Array<{ name: string; value: string }>
  basis: string[]
}

export interface Clarification {
  question: string
  hint?: string
}

export interface DecisionFactor {
  name: string
  score: number
  basis: string
}

export interface DecisionAuditView {
  id: number
  ts: string
  route: string
  route_reason: string
  confidence: number | null
  confidence_factors: DecisionFactor[] | null
  risk: number | null
  risk_factors: DecisionFactor[] | null
  matched_rules: string[] | null
  decision_engine: string
  codex_session_id: string | null
  threshold_config_version: string
  final_status: string
}

export interface ConfirmationDetail {
  todo: Todo
  source_messages: ConfirmationMessage[]
  assigner: ConfirmationAssigner | null
  proposed_plan: ProposedPlan | null
  clarifications: Clarification[] | null
  audits: DecisionAuditView[] | null
}

export type TaskStatus = 'pending' | 'executing' | 'awaiting_approval' | 'done' | 'failed'

// TaskProposal is the high-risk external write codex prepared during the propose
// stage, awaiting human approval. It is stored in execution_result while the Task
// sits at awaiting_approval (stage="proposal").
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
  todo_id: number
  title: string
  action_type: ActionType
  background: Record<string, unknown>
  plan: Record<string, unknown>
  confirmed_by: string
  confirmed_at: string
  action_hash: string
  status: TaskStatus
  execution_result: Record<string, unknown> | null
  execution_supplements?: Array<{ note: string; at: string; channel?: string }>
  autonomy_mode: string
  project_id: number | null
  version: number
  created_at: string
  updated_at: string
}

export interface TaskList {
  items: Task[]
  total: number
  page: number
  page_size: number
}

// RunEnrichment 是 codex 主动"多做一步"准备的一条结构化产物：
//   kind=context      正文/结论段落（如"会议一页纸"）
//   kind=doc_link     引用的文件/文档路径
//   kind=code_link    引用的代码位置
//   kind=commit_digest 仓库 commit 摘要
// 未知 kind 一律按纯文本 detail 展示，不丢信息。
export interface RunEnrichment {
  kind: string
  label: string
  detail: string
}

// RunOutput 是 execution_run.output 的强类型：codex 执行结束时输出的结构化裁决。
// summary 已单独存在 ExecutionRun.summary，这里主要用 needs_followup 与 enrichments。
export interface RunOutput {
  success?: boolean
  summary?: string
  failure_reason?: string
  needs_followup?: string
  enrichments?: RunEnrichment[]
}

// ExecutionRun 是一次 M5 执行的审计记录，一个 Task 可有多条（重试）。
export interface ExecutionRun {
  id: number
  task_id: number
  action_type: ActionType
  sandbox: string
  status: string
  codex_session_id: string | null
  summary: string | null
  output: RunOutput | null
  error_detail: string | null
  repo_path: string | null
  branch: string | null
  commit: string | null
  diff_path: string | null
  merge_request_url: string | null
  started_at: string
  finished_at: string | null
  duration_ms: number | null
}

export interface ExecutionRunList {
  items: ExecutionRun[]
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
  mem0_synced_at: string | null
  created_at: string
  updated_at: string
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
  mem0_synced_at: string | null
  created_at: string
  updated_at: string
}

export interface Group {
  id: number
  chat_id: string
  chat_mode: string
  name: string | null
  description: string | null
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

export interface PersonInput {
  open_id: string
  name: string
  role: PersonRole
  priority_weight: number
  department?: string | null
  title?: string | null
  relation?: string | null
  comm_style?: string | null
  p2p_chat_id?: string | null
  notes?: string | null
  is_active?: boolean
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
  project_id: number | null
  related_group: boolean
  pinned: boolean
  include_in_memory: boolean
  is_key_group: boolean
}

// ProfileView is the decision-maker ("me") background. open_id is fixed by
// backend config; saved=false means the row has not been filled yet.
// SharedMemory 是全局单例的「共享记忆」大文本视图，对齐后端 sharedmem.SharedMemoryView。
export interface SharedMemory {
  content: string
  updated_by: string
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
    pending: number
    leader_open: number
    by_status: StatusCount[]
  }
  tasks: {
    total: number
    pending: number
    done: number
    failed: number
    by_status: StatusCount[]
  }
}

export interface MyDay {
  date: string
  todos_created: number
  confirmed: number
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

// DailyDigest 是某个自然日的一条内容总结；同一天 person 一条、每个关键群一条。
export interface DailyDigest {
  id: number
  scope: DailyDigestScope
  scope_id: string
  digest_date: string
  summary: string
  status: DailyDigestStatus
  source_count: number
  engine: 'codex' | 'qwen'
  error_detail: string | null
  generated_at: string | null
  updated_at: string
}

export interface DailyDigestKickResult {
  scope: DailyDigestScope
  scope_id: string
  date: string
  status: 'generating'
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
  repo: string // 形如 chujiejie.1/jarvis_bot
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

export interface Dependency {
  name: string
  status: 'ok' | 'error'
  detail?: string
}

export interface TableCount {
  table: string
  count: number
}

export interface StatusCount {
  status: string
  count: number
}

export interface BacklogMetric {
  key: string
  label: string
  value: number
  detail?: string
}

export interface DebugStatus {
  time: string
  dependencies: Dependency[]
  tables: TableCount[]
  backlog: BacklogMetric[]
  todo_by_status: StatusCount[]
  task_by_status: StatusCount[]
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

export interface FailureEvent {
  time: string
  module: string
  job: string
  error: string
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
  last_scanned_at: string
  updated_at: string
}

export interface LogLine {
  source: string
  time: string
  text: string
}

export interface LogTail {
  sources: string[]
  lines: LogLine[]
  truncated: boolean
  notes: string[]
}

// Debug todo/task rows are the raw Go domain structs marshaled with Go field
// names; the panel only renders them as expandable JSON, so a loose record type
// is enough.
export type DebugRecord = Record<string, unknown>

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

export type WorkRuleType = 'all' | 'selected'
export type WorkRuleStage = 'extract' | 'decide' | 'execute'

export interface WorkRule {
  id: number
  name: string
  content: string
  rule_type: WorkRuleType
  stages: WorkRuleStage[]
  priority: number
  is_enabled: boolean
}

export interface WorkRuleInput {
  name: string
  content: string
  rule_type: WorkRuleType
  stages: WorkRuleStage[]
  priority: number
  is_enabled: boolean
}

export interface TextStorage {
  id: number
  storage_key: string
  name: string
  content: string
}

export interface TextStorageInput {
  storage_key: string
  name: string
  content: string
}

export type ScheduledTaskStatus = 'active' | 'running' | 'completed'
export type ScheduledTaskLastRunStatus = 'done' | 'failed'
export type ScheduledTaskScheduleType = 'once' | 'daily' | 'interval'

export interface ScheduledTask {
  id: number
  title: string
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
  last_result: string | null
  last_error_detail: string | null
  last_started_at: string | null
  last_finished_at: string | null
  created_at: string
  updated_at: string
}

export interface ScheduledTaskInput {
  title: string
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
  id: number
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
  content: string
}

// --- codex 对话框契约（跨 agent 冻结，A/B/C 共用）---

// PageContext 是右侧对话框对左侧页面的单向感知：当前所在 Tab + 选中项摘要。
// 由各页面写入 PageContext（React Context），发送对话时随请求带给后端注入 prompt。
export interface PageContext {
  // 当前左侧导航 key：overview/todos/confirmations/tasks/scheduled-tasks/background/settings/progress/debug
  active_key: string
  // 当前选中项的可读摘要（如 "Todo #12 修复登录超时"）；无选中则 null
  selection: PageSelection | null
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
