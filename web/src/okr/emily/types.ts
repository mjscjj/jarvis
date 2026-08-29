export type Status =
  | 'in_progress'
  | 'done'
  | 'not_started'
  | 'at_risk'
  | 'delayed'
  | 'blocked'

export type PointKind = 'strategy' | 'product'

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
  status: Status
  text: string
  docs: DocLink[]
  images: ImageRef[]
  source?: string
  needsReview?: boolean
}

export type Light = 'green' | 'yellow' | 'red'

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
  entries: Entry[]
  previousEntries?: Entry[]
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
  krVersion: number
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
  priority?: 'p0' | 'p1' | 'p2'
  version?: number
  tags?: KrTag[]
}

export interface KrOwner {
  openId: string
  name: string
}

export interface KrTag {
  type: string
  value: string
}

export interface PersonSearchItem {
  openId: string
  name: string
  department: string
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
  user?: AuthUser
}

export interface FeishuDocumentResult {
	documentId: string
	url: string
	warnings: string[]
}

export interface Objective {
  id: string
  title: string
  krs: Kr[]
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
  priorities: Array<NonNullable<Kr['priority']>>
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

export type PMODigestKind = 'risk' | 'sync_issue' | 'missing' | 'changed'

export interface PMODigest {
  quarter: string
  week: string
  previousWeek?: string
  mode: 'preview_only'
  sendEnabled: false
  summary: {
    riskCount: number
    syncIssueCount: number
    missingCount: number
    changedCount: number
  }
  sections: Array<{
    kind: PMODigestKind
    title: string
    items: PMODigestItem[]
  }>
}

export interface PMODigestItem {
  objectiveId: string
  objectiveTitle: string
  krId: string
  krTitle: string
  ownerName: string
  pointId: string
  pointTitle: string
  status?: string
  detail: string
  source: 'page' | 'page_history' | 'meego'
}

export type ReportType = 'middle_platform_weekly' | 'biweekly_review'

export interface ReportDraft {
  id: string
  quarter: string
  week: string
  reportType: ReportType
  tagType: string
  tagValue: string
  title: string
  sections: ReportDraftSection[]
  version: number
  saved: boolean
  publishMode: 'unpublished'
}

export interface ReportDraftSection {
  kind: 'progress' | 'risk' | 'change'
  title: string
  items: ReportDraftItem[]
}

export interface ReportDraftItem {
  id: string
  objectiveId: string
  objectiveTitle: string
  krId: string
  krTitle: string
  ownerName: string
  pointId: string
  pointTitle: string
  week: string
  status?: string
  source: string
  detail: string
}

export interface ReportDraftHistory {
  draftId: string
  currentVersion: number
  publishMode: 'unpublished'
  revisions: ReportDraftRevision[]
}

export interface ReportDraftRevision {
  version: number
  title: string
  sections: ReportDraftSection[]
  updatedBy: string
  createdAt: string
  titleChanged: boolean
  changedItemCount: number
  changedItems: Array<{
    id: string
    pointId: string
    pointTitle: string
    before: string
    after: string
  }>
}

export interface RegionSyncDraft {
  quarter: string
  week: string
  region: string
  mode: 'preview_only'
  publishMode: 'unpublished'
  summary: { krCount: number; pointCount: number; riskCount: number; missingCount: number }
  rows: RegionSyncRow[]
}

export interface RegionSyncRow {
  objectiveId: string
  objectiveTitle: string
  krId: string
  krTitle: string
  ownerName: string
  pointId: string
  pointTitle: string
  status: string
  detail: string
  source: string
  risk: boolean
  missing: boolean
}

export interface QuarterlyOKRDraft {
  quarter: string
  week: string
  mode: 'preview_only'
  publishMode: 'unpublished'
  summary: { objectiveCount: number; krCount: number; riskCount: number; missingCount: number }
  objectives: QuarterlyObjective[]
}

export interface QuarterlyObjective {
  id: string
  title: string
  krs: QuarterlyKR[]
}

export interface QuarterlyKR {
  id: string
  title: string
  ownerName: string
  priority: string
  status: string
  risk: boolean
  missing: boolean
  metrics: string[]
  keyProgress: string
  points: Array<{ id: string; title: string; status: string; detail: string; source: string; risk: boolean; missing: boolean }>
}
