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
