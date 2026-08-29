import type { AuthStatus, Entry, EnumValues, FeishuDocumentResult, ImageRef, Kr, KrOwner, KrTag, Light, MeegoBatchPreview, MeegoPreview, Objective, PageComment, PageCommentList, PersonSearchResult, PMODigest, PointKind, QuarterlyOKRDraft, RegionSyncDraft, ReminderBatch, ReminderBatchList, ReminderPreview, ReportDraft, ReportDraftHistory, ReportDraftSection, ReportType, Status } from './types'

interface Envelope<T> {
  code: number
  data?: T
  msg?: string
  logid?: string
}

interface APIEntry {
  id: string
  status: Status
  text: string
  docs: Entry['docs']
  images: Entry['images']
  source: string
  needs_review: boolean
}

interface APIKr {
  id: string
  title: string
  owner_open_id: string
  owner_name: string
  owners: Array<{ open_id: string; name: string }>
  priority: NonNullable<Kr['priority']>
  metric_note: string
  version: number
  metrics: Array<{ id: string; text: string; light?: Light; images?: Entry['images'] }>
  points: Array<{ id: string; kind: PointKind; title: string; meego_work_item_id?: string; meego_url?: string; entries: APIEntry[]; previous_entries: APIEntry[] }>
  tags: KrTag[]
}

interface APIBoard {
  quarter: string
  week: string
  previous_week?: string
  available_weeks: string[]
  objectives: Array<{ id: string; title: string; krs: APIKr[] }>
}

interface APIPageComment {
  id: string
  parent_id?: string
  target_type: 'page' | 'kr' | 'metric' | 'point' | 'entry'
  target_id?: string
  target_title?: string
  selected_text?: string
  selection_start?: number
  selection_end?: number
  selection_prefix?: string
  selection_suffix?: string
  author_open_id?: string
  author_name: string
  content: string
  todo?: boolean
  resolved?: boolean
  created_at: string
  updated_at: string
  replies: APIPageComment[]
}

interface APIPageCommentList {
  quarter: string
  week: string
  count: number
  comments: APIPageComment[]
}

interface APIAuthStatus {
  authenticated: boolean
  configured: boolean
  user?: {
    open_id: string
    name: string
    avatar_url?: string
    email?: string
  }
}

interface APIEnums {
  statuses: Status[]
  point_kinds: PointKind[]
  lights: Light[]
  priorities: Array<NonNullable<Kr['priority']>>
}

interface APIReminderPreview {
  quarter: string
  week: string
  mode: 'preview_only'
  send_enabled: false
  summary: {
    owner_count: number
    needs_reminder_owner_count: number
    due_count: number
    filled_count: number
    missing_count: number
  }
  recipients: Array<{
    owner_open_id: string
    owner_name: string
    due_count: number
    filled_count: number
    missing_count: number
    needs_reminder: boolean
    can_remind: boolean
    missing_krs: Array<{ id: string; title: string }>
    message: string
  }>
}

interface APIReminderBatch {
  id: string
  quarter: string
  week: string
  trigger: 'manual' | 'scheduler'
  status: 'running' | 'succeeded' | 'failed'
  send_enabled: false
  recipient_count: number
  missing_count: number
  summary: APIReminderPreview['summary']
  recipients: APIReminderPreview['recipients']
  last_error?: string
  started_at: string
  finished_at?: string
}

interface APIReminderBatchList {
  mode: 'preview_only'
  send_enabled: false
  batches: APIReminderBatch[]
}

interface APIPMODigest {
  quarter: string
  week: string
  previous_week?: string
  mode: 'preview_only'
  send_enabled: false
  summary: {
    risk_count: number
    sync_issue_count: number
    missing_count: number
    changed_count: number
  }
  sections: Array<{
    kind: 'risk' | 'sync_issue' | 'missing' | 'changed'
    title: string
    items: Array<{
      objective_id: string
      objective_title: string
      kr_id: string
      kr_title: string
      owner_name: string
      point_id: string
      point_title: string
      status?: string
      detail: string
      source: 'page' | 'page_history' | 'meego'
    }>
  }>
}

interface APIReportDraft {
  id: string
  quarter: string
  week: string
  report_type: ReportType
  tag_type: string
  tag_value: string
  title: string
  sections: Array<{
    kind: 'progress' | 'risk' | 'change'
    title: string
    items: Array<{
      id: string
      objective_id: string
      objective_title: string
      kr_id: string
      kr_title: string
      owner_name: string
      point_id: string
      point_title: string
      week: string
      status?: string
      source: string
      detail: string
    }>
  }>
  version: number
  saved: boolean
  publish_mode: 'unpublished'
}

interface APIReportDraftHistory {
  draft_id: string
  current_version: number
  publish_mode: 'unpublished'
  revisions: Array<{
    version: number
    title: string
    sections: APIReportDraft['sections']
    updated_by: string
    created_at: string
    title_changed: boolean
    changed_item_count: number
    changed_items: Array<{
      id: string
      point_id: string
      point_title: string
      before: string
      after: string
    }>
  }>
}

interface APIRegionSyncDraft {
  quarter: string
  week: string
  region: string
  mode: 'preview_only'
  publish_mode: 'unpublished'
  summary: { kr_count: number; point_count: number; risk_count: number; missing_count: number }
  rows: Array<{
    objective_id: string
    objective_title: string
    kr_id: string
    kr_title: string
    owner_name: string
    point_id: string
    point_title: string
    status: string
    detail: string
    source: string
    risk: boolean
    missing: boolean
  }>
}

interface APIQuarterlyOKRDraft {
  quarter: string
  week: string
  mode: 'preview_only'
  publish_mode: 'unpublished'
  summary: { objective_count: number; kr_count: number; risk_count: number; missing_count: number }
  objectives: Array<{
    id: string
    title: string
    krs: Array<{
      id: string
      title: string
      owner_name: string
      priority: string
      status: string
      risk: boolean
      missing: boolean
      metrics: string[]
      key_progress: string
      points: Array<{ id: string; title: string; status: string; detail: string; source: string; risk: boolean; missing: boolean }>
    }>
  }>
}

interface APIMeegoPreview {
  point_id: string
  work_item_id: string
  url: string
  mode: 'preview_only'
  write_enabled: false
  local: { title?: string; status: string; progress: string; updated_at?: string }
  remote: { title?: string; status: string; progress: string; updated_at?: string }
  diff: { status_changed: boolean; progress_changed: boolean }
  needs_review: boolean
}

interface APIMeegoBatchPreview {
  quarter: string
  week: string
  mode: 'preview_only'
  write_enabled: false
  summary: {
    linked_count: number
    compared_count: number
    risk_count: number
    needs_review_count: number
    error_count: number
    unsynced_count: number
    stale_count: number
  }
  items: Array<{
    objective_id: string
    objective_title: string
    kr_id: string
    kr_title: string
    kr_version: number
    owner_name: string
    point_id: string
    point_title: string
    risk: boolean
    preview?: APIMeegoPreview
    sync?: {
      week: string
      status: 'healthy' | 'error' | 'stale'
      last_attempt_at: string
      last_success_at?: string
      last_error?: string
    }
    error?: string
  }>
}

export class APIError<T = unknown> extends Error {
  readonly status: number
  readonly code?: number
  readonly data?: T
  readonly logid?: string

  constructor(
    message: string,
    status: number,
    code?: number,
    data?: T,
    logid?: string,
  ) {
    super(message)
    this.status = status
    this.code = code
    this.data = data
    this.logid = logid
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let response: Response
  try {
    const headers = new Headers(init?.headers)
    if (!(init?.body instanceof FormData) && init?.body != null && !headers.has('Content-Type')) {
      headers.set('Content-Type', 'application/json')
    }
    response = await fetch(path, {
      ...init,
      headers,
    })
  } catch {
    throw new APIError('暂时无法连接服务，请检查后端是否已启动。', 0)
  }
  let payload: Envelope<T>
  try {
    payload = (await response.json()) as Envelope<T>
  } catch {
    throw new APIError('服务返回了无法识别的内容。', response.status)
  }
  if (!response.ok || payload.code !== 0 || payload.data === undefined) {
    throw new APIError(payload.msg || '请求失败，请稍后重试。', response.status, payload.code, payload.data, payload.logid)
  }
  return payload.data
}

export type BoardSurface = 'okr' | 'weekly-report'

export async function uploadImage(file: File): Promise<ImageRef> {
  const body = new FormData()
  body.append('image', file, file.name || '截图.png')
  return request<ImageRef>('/api/okr/images', { method: 'POST', body })
}

function fromAPIKr(value: APIKr): Kr {
  return {
    id: value.id,
    title: value.title,
    ownerOpenId: value.owner_open_id,
    ownerName: value.owner_name,
    owners: (value.owners ?? []).map((owner): KrOwner => ({ openId: owner.open_id, name: owner.name })),
    priority: value.priority,
    metricNote: value.metric_note,
    version: value.version,
    metrics: value.metrics.map((metric) => ({ ...metric, images: metric.images ?? [] })),
    points: value.points.map((point) => ({
      id: point.id,
      kind: point.kind,
      title: point.title,
      meegoWorkItemId: point.meego_work_item_id ?? '',
      meegoUrl: point.meego_url ?? '',
      entries: point.entries.map((entry) => ({
        id: entry.id,
        status: entry.status,
        text: entry.text,
        docs: entry.docs ?? [],
        images: entry.images ?? [],
        source: entry.source,
        needsReview: entry.needs_review,
      })),
      previousEntries: (point.previous_entries ?? []).map((entry) => ({
        id: entry.id,
        status: entry.status,
        text: entry.text,
        docs: entry.docs ?? [],
        images: entry.images ?? [],
        source: entry.source,
        needsReview: entry.needs_review,
      })),
    })),
    tags: value.tags ?? [],
  }
}

export interface BoardData {
  quarter: string
  week: string
  previousWeek?: string
  availableWeeks: string[]
  objectives: Objective[]
}

export async function getBoard(quarter: string, week: string, surface: BoardSurface = 'okr'): Promise<BoardData> {
  const params = new URLSearchParams({ quarter, week })
  const board = await request<APIBoard>(`/api/${surface}/board?${params}`)
  return {
    quarter: board.quarter,
    week: board.week,
    previousWeek: board.previous_week,
    availableWeeks: board.available_weeks,
    objectives: board.objectives.map((objective) => ({ id: objective.id, title: objective.title, krs: objective.krs.map(fromAPIKr) })),
  }
}

function fromAPIComment(value: APIPageComment): PageComment {
  return {
    id: value.id,
    parentId: value.parent_id,
    targetType: value.target_type,
    targetId: value.target_id,
    targetTitle: value.target_title,
    selectedText: value.selected_text,
    selectionStart: value.selection_start,
    selectionEnd: value.selection_end,
    selectionPrefix: value.selection_prefix,
    selectionSuffix: value.selection_suffix,
    authorOpenId: value.author_open_id,
    authorName: value.author_name,
    content: value.content,
    todo: value.todo ?? false,
    resolved: value.resolved ?? false,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
    replies: (value.replies ?? []).map(fromAPIComment),
  }
}

export async function getComments(quarter: string, week: string): Promise<PageCommentList> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIPageCommentList>(`/api/weekly-report/comments?${params}`)
  return { quarter: value.quarter, week: value.week, count: value.count, comments: value.comments.map(fromAPIComment) }
}

export async function createComment(input: {
  quarter: string
  week: string
  parentId?: string
  content: string
  targetType?: PageComment['targetType']
  targetId?: string
  targetTitle?: string
  selectedText?: string
  selectionStart?: number
  selectionEnd?: number
  selectionPrefix?: string
  selectionSuffix?: string
}): Promise<PageComment> {
  const value = await request<APIPageComment>('/api/weekly-report/comments', {
    method: 'POST',
    body: JSON.stringify({
      quarter: input.quarter,
      week: input.week,
      parent_id: input.parentId ?? '',
      target_type: input.targetType ?? 'page',
      target_id: input.targetId ?? '',
      target_title: input.targetTitle ?? '',
      selected_text: input.selectedText ?? '',
      selection_start: input.selectionStart ?? 0,
      selection_end: input.selectionEnd ?? 0,
      selection_prefix: input.selectionPrefix ?? '',
      selection_suffix: input.selectionSuffix ?? '',
      content: input.content,
    }),
  })
  return fromAPIComment(value)
}

export async function getAuthStatus(): Promise<AuthStatus> {
  const value = await request<APIAuthStatus>('/api/okr/me')
  return {
    authenticated: value.authenticated,
    configured: value.configured,
    user: value.user ? {
      openId: value.user.open_id,
      name: value.user.name,
      avatarUrl: value.user.avatar_url,
      email: value.user.email,
    } : undefined,
  }
}

export function beginFeishuLogin() {
  const returnTo = `${window.location.pathname}${window.location.search}${window.location.hash}`
  window.location.assign(`/api/okr/auth/feishu/login?return_to=${encodeURIComponent(returnTo)}`)
}

export async function logout(): Promise<void> {
  await request<{ logged_out: boolean }>('/api/okr/auth/logout', { method: 'POST' })
}

export async function updateComment(id: string, patch: { content?: string; todo?: boolean; resolved?: boolean }): Promise<PageComment> {
  const value = await request<APIPageComment>(`/api/weekly-report/comments/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(patch),
  })
  return fromAPIComment(value)
}

export async function deleteComment(id: string): Promise<void> {
  await request<{ id: string }>(`/api/weekly-report/comments/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function getEnums(): Promise<EnumValues> {
  const value = await request<APIEnums>('/api/okr/enums')
  return { statuses: value.statuses, pointKinds: value.point_kinds, lights: value.lights, priorities: value.priorities }
}

export async function getReminderPreview(quarter: string, week: string): Promise<ReminderPreview> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIReminderPreview>(`/api/weekly-report/reminder-preview?${params}`)
  return {
    quarter: value.quarter,
    week: value.week,
    mode: value.mode,
    sendEnabled: value.send_enabled,
    summary: {
      ownerCount: value.summary.owner_count,
      needsReminderOwnerCount: value.summary.needs_reminder_owner_count,
      dueCount: value.summary.due_count,
      filledCount: value.summary.filled_count,
      missingCount: value.summary.missing_count,
    },
    recipients: value.recipients.map((recipient) => ({
      ownerOpenId: recipient.owner_open_id,
      ownerName: recipient.owner_name,
      dueCount: recipient.due_count,
      filledCount: recipient.filled_count,
      missingCount: recipient.missing_count,
      needsReminder: recipient.needs_reminder,
      canRemind: recipient.can_remind,
      missingKrs: recipient.missing_krs,
      message: recipient.message,
    })),
  }
}

function fromAPIReminderBatch(value: APIReminderBatch): ReminderBatch {
  return {
    id: value.id,
    quarter: value.quarter,
    week: value.week,
    trigger: value.trigger,
    status: value.status,
    sendEnabled: value.send_enabled,
    recipientCount: value.recipient_count,
    missingCount: value.missing_count,
    summary: {
      ownerCount: value.summary.owner_count,
      needsReminderOwnerCount: value.summary.needs_reminder_owner_count,
      dueCount: value.summary.due_count,
      filledCount: value.summary.filled_count,
      missingCount: value.summary.missing_count,
    },
    recipients: value.recipients.map((recipient) => ({
      ownerOpenId: recipient.owner_open_id,
      ownerName: recipient.owner_name,
      dueCount: recipient.due_count,
      filledCount: recipient.filled_count,
      missingCount: recipient.missing_count,
      needsReminder: recipient.needs_reminder,
      canRemind: recipient.can_remind,
      missingKrs: recipient.missing_krs,
      message: recipient.message,
    })),
    lastError: value.last_error,
    startedAt: value.started_at,
    finishedAt: value.finished_at,
  }
}

export async function getReminderBatches(quarter: string, week: string): Promise<ReminderBatchList> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIReminderBatchList>(`/api/weekly-report/reminder-batches?${params}`)
  return { mode: value.mode, sendEnabled: value.send_enabled, batches: value.batches.map(fromAPIReminderBatch) }
}

export async function generateReminderBatch(quarter: string, week: string): Promise<ReminderBatch> {
  const value = await request<APIReminderBatch>('/api/weekly-report/reminder-batches/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ quarter, week }),
  })
  return fromAPIReminderBatch(value)
}

export async function getPMODigest(quarter: string, week: string): Promise<PMODigest> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIPMODigest>(`/api/weekly-report/pmo-digest?${params}`)
  return {
    quarter: value.quarter,
    week: value.week,
    previousWeek: value.previous_week,
    mode: value.mode,
    sendEnabled: value.send_enabled,
    summary: {
      riskCount: value.summary.risk_count,
      syncIssueCount: value.summary.sync_issue_count,
      missingCount: value.summary.missing_count,
      changedCount: value.summary.changed_count,
    },
    sections: value.sections.map((section) => ({
      kind: section.kind,
      title: section.title,
      items: section.items.map((item) => ({
        objectiveId: item.objective_id,
        objectiveTitle: item.objective_title,
        krId: item.kr_id,
        krTitle: item.kr_title,
        ownerName: item.owner_name,
        pointId: item.point_id,
        pointTitle: item.point_title,
        status: item.status,
        detail: item.detail,
        source: item.source,
      })),
    })),
  }
}

export async function getRegionSyncDraft(quarter: string, week: string, region: string): Promise<RegionSyncDraft> {
  const params = new URLSearchParams({ quarter, week, region })
  const value = await request<APIRegionSyncDraft>(`/api/weekly-report/region-sync-draft?${params}`)
  return {
    quarter: value.quarter,
    week: value.week,
    region: value.region,
    mode: value.mode,
    publishMode: value.publish_mode,
    summary: {
      krCount: value.summary.kr_count,
      pointCount: value.summary.point_count,
      riskCount: value.summary.risk_count,
      missingCount: value.summary.missing_count,
    },
    rows: value.rows.map((row) => ({
      objectiveId: row.objective_id,
      objectiveTitle: row.objective_title,
      krId: row.kr_id,
      krTitle: row.kr_title,
      ownerName: row.owner_name,
      pointId: row.point_id,
      pointTitle: row.point_title,
      status: row.status,
      detail: row.detail,
      source: row.source,
      risk: row.risk,
      missing: row.missing,
    })),
  }
}

export async function getQuarterlyOKRDraft(quarter: string, week: string): Promise<QuarterlyOKRDraft> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIQuarterlyOKRDraft>(`/api/okr/quarterly-okr-draft?${params}`)
  return {
    quarter: value.quarter,
    week: value.week,
    mode: value.mode,
    publishMode: value.publish_mode,
    summary: { objectiveCount: value.summary.objective_count, krCount: value.summary.kr_count, riskCount: value.summary.risk_count, missingCount: value.summary.missing_count },
    objectives: value.objectives.map((objective) => ({
      id: objective.id,
      title: objective.title,
      krs: objective.krs.map((kr) => ({
        id: kr.id,
        title: kr.title,
        ownerName: kr.owner_name,
        priority: kr.priority,
        status: kr.status,
        risk: kr.risk,
        missing: kr.missing,
        metrics: kr.metrics,
        keyProgress: kr.key_progress,
        points: kr.points,
      })),
    })),
  }
}

export async function generateReportDraft(input: { quarter: string; week: string; reportType: ReportType; tagType?: string; tagValue?: string }): Promise<ReportDraft> {
  const value = await request<APIReportDraft>('/api/weekly-report/report-drafts/generate', {
    method: 'POST',
    body: JSON.stringify({ quarter: input.quarter, week: input.week, report_type: input.reportType, tag_type: input.tagType ?? '', tag_value: input.tagValue ?? '' }),
  })
  return fromAPIReportDraft(value)
}

export async function saveReportDraft(draft: ReportDraft): Promise<ReportDraft> {
  const value = await request<APIReportDraft>(`/api/weekly-report/report-drafts/${encodeURIComponent(draft.id)}`, {
    method: 'PUT',
    body: JSON.stringify({
      quarter: draft.quarter,
      week: draft.week,
      report_type: draft.reportType,
      tag_type: draft.tagType,
      tag_value: draft.tagValue,
      expected_version: draft.version,
      title: draft.title,
      sections: draft.sections.map(toAPIReportDraftSection),
    }),
  })
  return fromAPIReportDraft(value)
}

export async function getReportDraftHistory(draftId: string): Promise<ReportDraftHistory> {
  const value = await request<APIReportDraftHistory>(`/api/weekly-report/report-drafts/${encodeURIComponent(draftId)}/history`)
  return {
    draftId: value.draft_id,
    currentVersion: value.current_version,
    publishMode: value.publish_mode,
    revisions: value.revisions.map((revision) => ({
      version: revision.version,
      title: revision.title,
      sections: fromAPIReportDraftSections(revision.sections),
      updatedBy: revision.updated_by,
      createdAt: revision.created_at,
      titleChanged: revision.title_changed,
      changedItemCount: revision.changed_item_count,
      changedItems: revision.changed_items.map((item) => ({
        id: item.id,
        pointId: item.point_id,
        pointTitle: item.point_title,
        before: item.before,
        after: item.after,
      })),
    })),
  }
}

export async function restoreReportDraft(draftId: string, expectedVersion: number, revisionVersion: number): Promise<ReportDraft> {
  const value = await request<APIReportDraft>(`/api/weekly-report/report-drafts/${encodeURIComponent(draftId)}/restore`, {
    method: 'POST',
	body: JSON.stringify({ expected_version: expectedVersion, revision_version: revisionVersion }),
  })
  return fromAPIReportDraft(value)
}

export async function createFeishuDocument(title: string, content: string): Promise<FeishuDocumentResult> {
	const value = await request<{ document_id: string; url: string; warnings?: string[] }>('/api/weekly-report/feishu-documents', {
		method: 'POST',
		body: JSON.stringify({ title, content }),
	})
	return { documentId: value.document_id, url: value.url, warnings: value.warnings ?? [] }
}

function fromAPIReportDraft(value: APIReportDraft): ReportDraft {
  return {
    id: value.id,
    quarter: value.quarter,
    week: value.week,
    reportType: value.report_type,
    tagType: value.tag_type,
    tagValue: value.tag_value,
    title: value.title,
    sections: fromAPIReportDraftSections(value.sections),
    version: value.version,
    saved: value.saved,
    publishMode: value.publish_mode,
  }
}

function fromAPIReportDraftSections(sections: APIReportDraft['sections']): ReportDraftSection[] {
  return sections.map((section) => ({
      kind: section.kind,
      title: section.title,
      items: section.items.map((item) => ({
        id: item.id,
        objectiveId: item.objective_id,
        objectiveTitle: item.objective_title,
        krId: item.kr_id,
        krTitle: item.kr_title,
        ownerName: item.owner_name,
        pointId: item.point_id,
        pointTitle: item.point_title,
        week: item.week,
        status: item.status,
        source: item.source,
        detail: item.detail,
      })),
    }))
}

function toAPIReportDraftSection(section: ReportDraftSection) {
  return {
    kind: section.kind,
    title: section.title,
    items: section.items.map((item) => ({
      id: item.id,
      objective_id: item.objectiveId,
      objective_title: item.objectiveTitle,
      kr_id: item.krId,
      kr_title: item.krTitle,
      owner_name: item.ownerName,
      point_id: item.pointId,
      point_title: item.pointTitle,
      week: item.week,
      status: item.status,
      source: item.source,
      detail: item.detail,
    })),
  }
}

export async function getMeegoPreview(pointId: string, week: string): Promise<MeegoPreview> {
  const params = new URLSearchParams({ week })
  const value = await request<APIMeegoPreview>(`/api/weekly-report/points/${encodeURIComponent(pointId)}/meego-preview?${params}`)
  return {
    pointId: value.point_id,
    workItemId: value.work_item_id,
    url: value.url,
    mode: value.mode,
    writeEnabled: value.write_enabled,
    local: { title: value.local.title, status: value.local.status, progress: value.local.progress, updatedAt: value.local.updated_at },
    remote: { title: value.remote.title, status: value.remote.status, progress: value.remote.progress, updatedAt: value.remote.updated_at },
    diff: { statusChanged: value.diff.status_changed, progressChanged: value.diff.progress_changed },
    needsReview: value.needs_review,
  }
}

function fromAPIMeegoPreview(value: APIMeegoPreview): MeegoPreview {
  return {
    pointId: value.point_id,
    workItemId: value.work_item_id,
    url: value.url,
    mode: value.mode,
    writeEnabled: value.write_enabled,
    local: { title: value.local.title, status: value.local.status, progress: value.local.progress, updatedAt: value.local.updated_at },
    remote: { title: value.remote.title, status: value.remote.status, progress: value.remote.progress, updatedAt: value.remote.updated_at },
    diff: { statusChanged: value.diff.status_changed, progressChanged: value.diff.progress_changed },
    needsReview: value.needs_review,
  }
}

export async function getMeegoBatchPreview(quarter: string, week: string): Promise<MeegoBatchPreview> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIMeegoBatchPreview>(`/api/weekly-report/meego-preview?${params}`)
  return fromAPIMeegoBatchPreview(value)
}

function fromAPIMeegoBatchPreview(value: APIMeegoBatchPreview): MeegoBatchPreview {
  return {
    quarter: value.quarter,
    week: value.week,
    mode: value.mode,
    writeEnabled: value.write_enabled,
    summary: {
      linkedCount: value.summary.linked_count,
      comparedCount: value.summary.compared_count,
      riskCount: value.summary.risk_count,
      needsReviewCount: value.summary.needs_review_count,
      errorCount: value.summary.error_count,
      unsyncedCount: value.summary.unsynced_count,
      staleCount: value.summary.stale_count,
    },
    items: value.items.map((item) => ({
      objectiveId: item.objective_id,
      objectiveTitle: item.objective_title,
      krId: item.kr_id,
      krTitle: item.kr_title,
      krVersion: item.kr_version,
      ownerName: item.owner_name,
      pointId: item.point_id,
      pointTitle: item.point_title,
      risk: item.risk,
      preview: item.preview ? fromAPIMeegoPreview(item.preview) : undefined,
      sync: item.sync ? {
        week: item.sync.week,
        status: item.sync.status,
        lastAttemptAt: item.sync.last_attempt_at,
        lastSuccessAt: item.sync.last_success_at,
        lastError: item.sync.last_error,
      } : undefined,
      error: item.error,
    })),
  }
}

export async function confirmMeegoProgress(input: {
  pointId: string
  expectedVersion: number
  week: string
  meegoWorkItemId: string
  status: Status
  text: string
}): Promise<Kr> {
  try {
    const value = await request<APIKr>(`/api/weekly-report/points/${encodeURIComponent(input.pointId)}/meego-confirm`, {
      method: 'POST',
      body: JSON.stringify({
        expected_version: input.expectedVersion,
        week: input.week,
        meego_work_item_id: input.meegoWorkItemId,
        status: input.status,
        text: input.text,
      }),
    })
    return fromAPIKr(value)
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIKr(error.data as APIKr), error.logid)
    }
    throw error
  }
}

export async function replaceKR(kr: Kr, week: string, surface: BoardSurface = 'okr'): Promise<Kr> {
  try {
    const value = await request<APIKr>(`/api/${surface}/krs/${encodeURIComponent(kr.id)}`, {
      method: 'PUT',
      body: JSON.stringify({
      expected_version: kr.version ?? 0,
      week,
      title: kr.title,
      owner_open_id: kr.ownerOpenId ?? '',
      owner_name: kr.ownerName ?? '',
      owners: (kr.owners ?? []).map((owner) => ({ open_id: owner.openId, name: owner.name })),
      priority: kr.priority ?? 'p1',
      metric_note: kr.metricNote,
      metrics: kr.metrics,
      points: kr.points.map((point) => ({
        id: point.id,
        kind: point.kind,
        title: point.title,
        meego_work_item_id: point.meegoWorkItemId ?? '',
        meego_url: point.meegoUrl ?? '',
        entries: point.entries.map((entry) => ({
          id: entry.id,
          status: entry.status,
          text: entry.text,
          docs: entry.docs,
          images: entry.images,
          source: entry.source ?? 'manual',
          needs_review: entry.needsReview ?? false,
        })),
      })),
      tags: kr.tags ?? [],
      }),
    })
    return fromAPIKr(value)
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIKr(error.data as APIKr), error.logid)
    }
    throw error
  }
}

export async function searchPeople(query: string, signal?: AbortSignal): Promise<PersonSearchResult> {
  const value = await request<{ users: Array<{ open_id: string; name: string; department: string }>; has_more: boolean }>(`/api/okr/people/search?q=${encodeURIComponent(query)}`, { signal })
  return {
    users: value.users.map((item) => ({ openId: item.open_id, name: item.name, department: item.department })),
    hasMore: value.has_more,
  }
}

export async function createKR(objectiveId: string, input: { week: string; title: string; ownerName?: string; priority?: NonNullable<Kr['priority']> }): Promise<Kr> {
  const value = await request<APIKr>(`/api/okr/objectives/${encodeURIComponent(objectiveId)}/krs`, {
    method: 'POST',
    body: JSON.stringify({
      week: input.week,
      title: input.title,
      owner_name: input.ownerName ?? '',
      priority: input.priority ?? 'p1',
    }),
  })
  return fromAPIKr(value)
}

export async function deleteKR(kr: Kr): Promise<void> {
  await request<{ id: string }>(`/api/okr/krs/${encodeURIComponent(kr.id)}`, {
    method: 'DELETE',
    body: JSON.stringify({ expected_version: kr.version ?? 0 }),
  })
}
