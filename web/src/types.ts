export type TodoStatus =
  | 'extracted'
  | 'scoring'
  | 'need_info'
  | 'need_decision'
  | 'confirmed'
  | 'dismissed'
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

export interface Todo {
  id: number
  title: string
  description: string
  action_type: ActionType
  slots: Record<string, unknown>
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
  missing_info: string[] | null
  revision: number
  version: number
  first_seen_at: string
  last_evidence_at: string
  created_at: string
  updated_at: string
  group: TodoGroup | null
  project: TodoProject | null
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

export interface ConfirmationDetail {
  todo: Todo
  source_messages: ConfirmationMessage[]
  assigner: ConfirmationAssigner | null
  proposed_plan: ProposedPlan | null
}

export type TaskStatus = 'pending' | 'done' | 'failed'

export interface Task {
  id: number
  todo_id: number
  title: string
  action_type: ActionType
  background: Record<string, unknown>
  plan: Record<string, unknown>
  slots: Record<string, unknown>
  confirmed_by: string
  confirmed_at: string
  action_hash: string
  status: TaskStatus
  execution_result: Record<string, unknown> | null
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
