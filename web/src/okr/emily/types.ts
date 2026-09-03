export type Status =
  | 'in_progress'
  | 'done'
  | 'not_started'
  | 'at_risk'
  | 'delayed'
  | 'blocked'

export type PointKind = 'strategy' | 'product'
export type WeekTemplateKey = 'classic' | 'okr_weekly_preview_v1'

export interface WeeklyScore {
  value: number
  version: number
}

export interface DocLink {
  id: string
  title: string
  url: string
}

export interface ImageRef {
  id: string
  name: string
  url: string
  /** 显示宽度（px），拖拽右下角改，存下来刷新还在 */
  width?: number
}

/** 进展列里的一条 bullet */
export interface Entry {
  id: string
	version?: number
  status: Status
  text: string
  docs: DocLink[]
  images: ImageRef[]
  source?: string
  needsReview?: boolean
}

export type Light = 'green' | 'yellow' | 'red'
export type KrPriority = 'p0' | 'p1' | 'p2'

/** 核心数据里的一条，内容自由填写，灯可选可不选 */
export interface MetricLine {
  id: string
  text: string
  light?: Light
  images?: ImageRef[]
}

/** 具体事项，表格里的一行 */
export interface Point {
  id: string
  kind: PointKind
  title: string
  meegoWorkItemId?: string
  meegoUrl?: string
  tags?: KrTag[]
  owners?: KrOwner[]
  entries: Entry[]
  previousEntries?: Entry[]
  score?: WeeklyScore
}

export interface MeegoPreview {
  pointId: string
  workItemId: string
  url: string
  mode: 'preview_only'
  writeEnabled: false
  local: MeegoPreviewContent
  remote: MeegoPreviewContent
  diff: { statusChanged: boolean; progressChanged: boolean }
  needsReview: boolean
}

export interface MeegoPreviewContent {
  title?: string
  status: string
  progress: string
  updatedAt?: string
}

export interface MeegoBatchPreview {
  quarter: string
  week: string
  mode: 'preview_only'
  writeEnabled: false
  summary: {
    linkedCount: number
    comparedCount: number
    riskCount: number
    needsReviewCount: number
    errorCount: number
    unsyncedCount: number
    staleCount: number
  }
  items: MeegoBatchPreviewItem[]
}

export interface MeegoBatchPreviewItem {
  objectiveId: string
  objectiveTitle: string
  krId: string
  krTitle: string
	progressVersion: number
  ownerName: string
  pointId: string
  pointTitle: string
  risk: boolean
  preview?: MeegoPreview
  sync?: {
    week: string
    status: 'healthy' | 'error' | 'stale'
    lastAttemptAt: string
    lastSuccessAt?: string
    lastError?: string
  }
  error?: string
}

export interface Kr {
  id: string
  title: string
  /** 核心数据的口径说明，可选，如「6 月 vs 7 月」 */
  metricNote: string
  metrics: MetricLine[]
  points: Point[]
  ownerOpenId?: string
  ownerName?: string
	owners?: KrOwner[]
	version?: number
	weeklyCoreVersion?: number
  tags?: KrTag[]
  score?: WeeklyScore
}

export interface KrOwner {
  openId: string
  name: string
}

export interface KrTag {
  type: string
  value: string
}

export interface PersonAvatarItem {
  openId: string
  name: string
  avatarUrl: string
}

export interface PersonSearchItem {
  openId: string
  name: string
  department: string
	email: string
	isExternal: boolean
	hasChatted: boolean
}

export interface PersonSearchResult {
  users: PersonSearchItem[]
  hasMore: boolean
}

export interface AuthUser {
  openId: string
  name: string
  avatarUrl?: string
  email?: string
}

export interface AuthStatus {
  authenticated: boolean
  configured: boolean
  expiresAt?: string
  user?: AuthUser
}

export interface FeishuDeviceLogin {
  loginId: string
  verificationUrl: string
  userCode?: string
  expiresAt: string
  pollIntervalSeconds: number
}

export interface FeishuDeviceLoginPoll {
  status: 'pending' | 'completed' | 'denied' | 'expired'
  retryAfterSeconds?: number
  user?: AuthUser
}

export interface FeishuDocumentResult {
	documentId: string
	url: string
	warnings: string[]
	linkShareEntity: 'tenant_editable'
}

export interface Objective {
  id: string
  title: string
  krs: Kr[]
}

export interface OKRPlanContent {
  objectives: Objective[]
}

export interface OKRPlan {
  id: string
  quarter: string
  title: string
  version: number
  content: OKRPlanContent
  createdBy: string
  updatedBy: string
  createdAt: string
  updatedAt: string
}

export interface OKRPlanSummary {
  id: string
  quarter: string
  title: string
  version: number
  objectiveCount: number
  krCount: number
  updatedAt: string
}

export interface OKRPlanList {
  quarter: string
  availableQuarters: string[]
  plans: OKRPlanSummary[]
}

export interface PageComment {
  id: string
  parentId?: string
  targetType: 'page' | 'kr' | 'metric' | 'point' | 'entry'
  targetId?: string
  targetTitle?: string
  selectedText?: string
  selectionStart?: number
  selectionEnd?: number
  selectionPrefix?: string
  selectionSuffix?: string
  authorOpenId?: string
  authorName: string
  content: string
  todo: boolean
  resolved: boolean
  createdAt: string
  updatedAt: string
  replies: PageComment[]
}

export interface PageCommentList {
  quarter: string
  week: string
  count: number
  comments: PageComment[]
}

export interface CommentTarget {
  type: PageComment['targetType']
  id: string
  title: string
  context?: string
  selection?: TextSelection
}

export interface TextSelection {
  text: string
  start: number
  end: number
  prefix?: string
  suffix?: string
}

export interface EnumValues {
	statuses: Status[]
	pointKinds: PointKind[]
	lights: Light[]
}

export interface ReminderPreview {
  quarter: string
  week: string
  mode: 'preview_only'
  sendEnabled: false
  summary: {
    ownerCount: number
    needsReminderOwnerCount: number
    dueCount: number
    filledCount: number
    missingCount: number
  }
  recipients: ReminderRecipient[]
}

export interface ReminderRecipient {
  ownerOpenId: string
  ownerName: string
  dueCount: number
  filledCount: number
  missingCount: number
  needsReminder: boolean
  canRemind: boolean
  missingKrs: Array<{ id: string; title: string }>
  message: string
}

export interface ReminderBatch {
  id: string
  quarter: string
  week: string
  trigger: 'manual' | 'scheduler'
  status: 'running' | 'succeeded' | 'failed'
  sendEnabled: false
  recipientCount: number
  missingCount: number
  summary: ReminderPreview['summary']
  recipients: ReminderRecipient[]
  lastError?: string
  startedAt: string
  finishedAt?: string
}

export interface ReminderBatchList {
  mode: 'preview_only'
  sendEnabled: false
  batches: ReminderBatch[]
}
