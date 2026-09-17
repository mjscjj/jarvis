import { appPath } from '../../appPath.ts'
import { normalizeKRTitle } from './krTitle'
import { createPeopleSearchCache } from './peopleSearchCache'
import type { AuthStatus, CommentDelivery, CommentMention, Entry, EnumValues, FeishuDeviceLogin, FeishuDeviceLoginPoll, FeishuDocumentResult, FollowUpItem, FollowUpList, FollowUpStatus, ImageRef, Kr, KrOwner, KrPriority, KrTag, Light, MeegoBatchPreview, MeegoPreview, Objective, OKRActivityEntry, OKRPlan, OKRPlanList, PageComment, PageCommentList, PersonAvatarItem, PointKind, ProductFeedback, ProductFeedbackList, RegionalAlignmentBoard, RegionalCode, RegionalDemand, RegionalPlanDecisionItem, RegionalRecapOverlay, ReminderBatch, ReminderBatchList, ReminderPreview, Status, WeekTemplateKey, WeeklyScore } from './types'

interface Envelope<T> {
  code: number
  data?: T
  msg?: string
  logid?: string
}

interface APIEntry {
  id: string
	version: number
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
	delete_token?: string
  owner_email: string
	owner_name: string
	owners: Array<{ email: string; name: string; union_id?: string }>
	metric_note: string
  version: number
	weekly_core_version: number
  metrics: Array<{ id: string; text: string; light?: Light; images?: Entry['images'] }>
	  points: Array<{ id: string; version: number; kind: PointKind; title: string; meego_work_item_id?: string; meego_url?: string; tags: KrTag[]; owners?: Array<{ email: string; name: string; union_id?: string }>; entries: APIEntry[]; previous_entries: APIEntry[]; score?: WeeklyScore }>
  tags: KrTag[]
  score?: WeeklyScore
}

interface APIBoard {
  quarter: string
  week: string
  template_key: WeekTemplateKey
	delete_token?: string
  previous_week?: string
  available_quarters: string[]
  available_weeks: string[]
  objectives: Array<{ id: string; title: string; version: number; owners?: Array<{ email: string; name: string; union_id?: string }>; krs: APIKr[] }>
}

interface APIPlanObjective {
  id: string
  title: string
  version?: number
	structure_token?: string
	owners?: Array<{ email: string; name: string; union_id?: string }>
  krs: Array<{
    id: string
    title: string
    version?: number
		structure_token?: string
    owners: Array<{ email: string; name: string; union_id?: string }>
    metric_note: string
    metrics: Array<{ id: string; text: string; light?: Light; images?: Entry['images'] }>
    points: Array<{ id: string; version?: number; kind?: PointKind; title?: string; meego_work_item_id?: string; meego_url?: string; owners?: Array<{ email: string; name: string; union_id?: string }>; tags?: KrTag[] }>
    tags: KrTag[]
  }>
}

interface APIPlan {
  id: string
  quarter: string
  title: string
  version: number
	delete_token: string
  objectives: APIPlanObjective[]
  created_by: string
  updated_by: string
  created_at: string
  updated_at: string
}

interface APIPlanSummary {
  id: string
  quarter: string
  title: string
  version: number
  objective_count: number
  kr_count: number
  updated_at: string
}

interface APIPointDefinitionPatchResult {
  point_id: string
	version: number
	structure_token?: string
	kr_structure_token?: string
	delete_token?: string
	plan_delete_token?: string
	title: string
	kind: PointKind
	meego_work_item_id: string
	meego_url: string
	owners: Array<{ email: string; name: string; union_id?: string }>
	tags: KrTag[]
}

export interface PointDefinitionPatchResult {
  pointId: string
	version: number
	structureToken?: string
	krStructureToken?: string
	deleteToken?: string
	planDeleteToken?: string
	title: string
	kind: PointKind
	meegoWorkItemId: string
	meegoUrl: string
	owners: KrOwner[]
	tags: KrTag[]
}

interface APIPlanList {
  quarter: string
  available_quarters: string[]
  plans: APIPlanSummary[]
}

interface APIRegionalDemand {
  id: string
  version: number
  regional_okr: string
  item: string
  requirement: string
  docs: Entry['docs']
  images: ImageRef[]
  priority: '' | KrPriority
  regional_pocs: Array<{ email: string; name: string }> | null
  platform_pocs: Array<{ email: string; name: string }> | null
  acceptance: 'yes' | 'no' | 'tbd'
  plan_kr_ids: string[]
  deliverable: string
  sort_order: number
}

interface APIRegionalDecision {
  plan_kr_id: string
  version: number
  onboard: '' | 'yes' | 'no'
  launch_regions: string[]
  regional_pocs: Array<{ email: string; name: string }> | null
  regional_okr: string
  hidden: boolean
}

interface APIRegionalRecapOverlay {
  bucket_key: string
  objective_id: string
  version: number
  sort_order: number
  hidden: boolean
}

interface APIRegionalBoard {
  alignment: { id: string; quarter: string; plan_id: string; recap_quarter: string; version: number }
  region: { region_code: RegionalCode; version: number; category_order: string[] }
  plan: APIPlan
  recap: APIBoard
  demands: APIRegionalDemand[]
  decisions: APIRegionalDecision[]
  recap_overlays: APIRegionalRecapOverlay[]
  translations: Record<string, string>
}

interface APIRegionalRefreshResult {
  board: APIRegionalBoard
  pending: boolean
}

interface APIActivityEntry {
  at: string
  actor_id: string
  actor_name: string
  surface: 'plan' | 'weekly'
  quarter?: string
  week?: string
  plan_id?: string
  action: string
  target_id?: string
  summary: string
}

interface APIWeek {
  quarter: string
  week: string
  template_key: WeekTemplateKey
  opened_by: string
  opened_at: string
}

export interface WeeklyReportWeek {
  quarter: string
  week: string
  templateKey: WeekTemplateKey
  openedBy: string
  openedAt: string
}

export interface WeeklyReportWeekList {
  quarter: string
  weeks: WeeklyReportWeek[]
}

interface APIPageComment {
  id: string
  version: number
  delete_token: string
  plan_id?: string
  alignment_id?: string
  region_code?: string
  parent_id?: string
  target_type: PageComment['targetType']
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
  mentions?: Array<{ email: string; name: string }>
  images?: ImageRef[]
  notification_errors?: string[]
  notifications?: CommentDelivery[]
  todo?: boolean
  resolved?: boolean
  created_at: string
  updated_at: string
  replies: APIPageComment[]
}

interface APIPageCommentList {
  quarter: string
  week?: string
  plan_id?: string
  count: number
  comments: APIPageComment[]
}

interface APIFollowUpItem {
  id: string
  quarter: string
  week: string
  version: number
  topic: string
  owners: Array<{ email: string; name: string; union_id?: string }>
  status: FollowUpStatus
  assign_date: string
  update: string
  source_key?: string
  source_payload: unknown
  sort_order: number
  created_by: string
  updated_by: string
  created_at: string
  updated_at: string
}

interface APIFollowUpList {
  quarter: string
  week: string
  count: number
  items: APIFollowUpItem[]
}

interface APIAuthStatus {
  authenticated: boolean
  configured: boolean
	management_access: boolean
	regional_auto_match_access: boolean
  expires_at?: string
  user?: {
    open_id: string
    union_id?: string
    name: string
    avatar_url?: string
    email?: string
  }
}

interface APIProductFeedbackPerson {
  name: string
  email?: string
  avatar_url?: string
}

interface APIProductFeedback {
  id: string
  version: number
  title: string
  content: string
  images?: ImageRef[]
  source_context?: Record<string, unknown>
  author: APIProductFeedbackPerson
  resolved: boolean
  resolved_by?: APIProductFeedbackPerson
  resolved_at?: string
  created_at: string
  updated_at: string
  replies?: Array<{ id: string; content: string; author: APIProductFeedbackPerson; created_at: string }>
  plus_ones?: APIProductFeedbackPerson[]
  my_plus_one: boolean
  can_resolve: boolean
}

interface APIProductFeedbackList {
  total: number
  items: APIProductFeedback[]
}

interface APIFeishuDeviceLogin {
  login_id: string
  verification_url: string
  user_code?: string
  expires_at: string
  poll_interval_seconds: number
}

interface APIFeishuDeviceLoginPoll {
  status: 'pending' | 'completed' | 'denied' | 'expired'
  retry_after_seconds?: number
  user?: APIAuthStatus['user']
}

interface APIEnums {
	statuses: Status[]
	point_kinds: PointKind[]
	lights: Light[]
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
    owner_email: string
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
	progress_version: number
    owners: Array<{ email: string; name: string; union_id?: string }>
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
    response = await fetch(appPath(path), {
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
    title: normalizeKRTitle(value.title),
	deleteToken: value.delete_token,
    ownerEmail: value.owner_email,
    ownerName: value.owner_name,
    owners: (value.owners ?? []).map((owner): KrOwner => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
		metricNote: value.metric_note,
    version: value.version,
		weeklyCoreVersion: value.weekly_core_version,
    metrics: (value.metrics ?? []).map((metric) => ({ ...metric, images: metric.images ?? [] })),
	    points: (value.points ?? []).map((point) => ({
	      id: point.id,
	      version: point.version ?? 0,
      kind: point.kind,
	          title: point.title ?? '',
      meegoWorkItemId: point.meego_work_item_id ?? '',
      meegoUrl: point.meego_url ?? '',
      tags: point.tags ?? [],
      owners: (point.owners ?? []).map((owner): KrOwner => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
      score: point.score,
      entries: (point.entries ?? []).map((entry) => ({
        id: entry.id,
		version: entry.version,
        status: entry.status,
        text: entry.text,
        docs: entry.docs ?? [],
        images: entry.images ?? [],
        source: entry.source,
        needsReview: entry.needs_review,
      })),
      previousEntries: (point.previous_entries ?? []).map((entry) => ({
        id: entry.id,
		version: entry.version,
        status: entry.status,
        text: entry.text,
        docs: entry.docs ?? [],
        images: entry.images ?? [],
        source: entry.source,
        needsReview: entry.needs_review,
      })),
    })),
    tags: value.tags ?? [],
    score: value.score,
  }
}

function fromAPIPlanObjectives(value: APIPlanObjective[]): Objective[] {
  return (value ?? []).map((objective) => ({
      id: objective.id,
      title: objective.title,
      version: objective.version ?? 0,
		structureToken: objective.structure_token ?? '',
		owners: (objective.owners ?? []).map((owner): KrOwner => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
      krs: (objective.krs ?? []).map((kr) => ({
        id: kr.id,
        title: normalizeKRTitle(kr.title),
        version: kr.version ?? 0,
		structureToken: kr.structure_token ?? '',
        owners: (kr.owners ?? []).map((owner): KrOwner => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
        ownerName: (kr.owners ?? []).map((owner) => owner.name).filter(Boolean).join('、'),
        ownerEmail: (kr.owners ?? []).find((owner) => owner.email)?.email ?? '',
        metricNote: kr.metric_note,
        weeklyCoreVersion: 0,
        metrics: (kr.metrics ?? []).map((metric) => ({ ...metric, images: metric.images ?? [] })),
	        points: (kr.points ?? []).map((point) => ({
	          id: point.id,
	          version: point.version ?? 0,
	          kind: point.kind!,
	          title: point.title ?? '',
          meegoWorkItemId: point.meego_work_item_id ?? '',
          meegoUrl: point.meego_url ?? '',
          tags: point.tags ?? [],
          owners: (point.owners ?? []).map((owner): KrOwner => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
          entries: [],
          previousEntries: [],
        })),
        tags: kr.tags ?? [],
      })),
    }))
}

function toAPIPlanKR(kr: Kr): APIPlanObjective['krs'][number] {
  return {
    id: kr.id,
    title: normalizeKRTitle(kr.title),
    version: kr.version ?? 0,
		structure_token: kr.structureToken,
    owners: (kr.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
    metric_note: kr.metricNote ?? '',
    metrics: kr.metrics.map((metric) => ({ id: metric.id, text: metric.text, light: metric.light, images: metric.images ?? [] })),
    points: kr.points.map((point) => ({
      id: point.id,
			version: point.version ?? 0,
			...(point.version === undefined ? {
				kind: point.kind,
				title: point.title,
				meego_work_item_id: point.meegoWorkItemId ?? '',
				meego_url: point.meegoUrl ?? '',
				owners: (point.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
				tags: point.tags ?? [],
			} : {}),
    })),
    tags: kr.tags ?? [],
  }
}

function toAPIPlanObjective(objective: Objective): APIPlanObjective {
  return {
    id: objective.id,
    title: objective.title,
    version: objective.version ?? 0,
		structure_token: objective.structureToken,
		owners: (objective.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
    krs: objective.krs.map(toAPIPlanKR),
  }
}

function fromAPIPlan(value: APIPlan): OKRPlan {
  return {
    id: value.id,
    quarter: value.quarter,
    title: value.title,
    version: value.version,
	deleteToken: value.delete_token,
    objectives: fromAPIPlanObjectives(value.objectives),
    createdBy: value.created_by,
    updatedBy: value.updated_by,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
  }
}

function fromAPIPlanSummary(value: APIPlanSummary) {
  return {
    id: value.id,
    quarter: value.quarter,
    title: value.title,
    version: value.version,
    objectiveCount: value.objective_count,
    krCount: value.kr_count,
    updatedAt: value.updated_at,
  }
}

export interface BoardData {
  quarter: string
  week: string
  templateKey: WeekTemplateKey
	deleteToken?: string
  previousWeek?: string
  availableQuarters: string[]
  availableWeeks: string[]
  objectives: Objective[]
}

export interface OpenWeekResult {
  week: WeeklyReportWeek
  created: boolean
}

export interface DeleteWeekResult {
  quarter: string
  week: string
  nextWeek?: string
}

export async function openWeeklyReportWeek(input: { quarter: string; week: string; templateKey: WeekTemplateKey }): Promise<OpenWeekResult> {
  const value = await request<{ week: APIWeek; created: boolean }>('/api/okr/weeks', {
    method: 'POST',
    body: JSON.stringify({ quarter: input.quarter, week: input.week, template_key: input.templateKey }),
  })
  return {
    created: value.created,
    week: {
      quarter: value.week.quarter,
      week: value.week.week,
      templateKey: value.week.template_key,
      openedBy: value.week.opened_by,
      openedAt: value.week.opened_at,
    },
  }
}

export async function listWeeklyReportWeeks(quarter = ''): Promise<WeeklyReportWeekList> {
  const params = new URLSearchParams()
  if (quarter) params.set('quarter', quarter)
  const value = await request<{ quarter: string; weeks: APIWeek[] }>(`/api/okr/weeks?${params}`)
  return {
    quarter: value.quarter,
    weeks: value.weeks.map((week) => ({
      quarter: week.quarter,
      week: week.week,
      templateKey: week.template_key,
      openedBy: week.opened_by,
      openedAt: week.opened_at,
    })),
  }
}

export async function deleteWeeklyReportWeek(quarter: string, week: string, deleteToken: string): Promise<DeleteWeekResult> {
  const value = await request<{
    quarter: string
    week: string
    next_week?: string
	}>(`/api/biz-okr/weeks/${encodeURIComponent(week)}?quarter=${encodeURIComponent(quarter)}`, {
		method: 'DELETE',
		body: JSON.stringify({ delete_token: deleteToken }),
	})
  return {
    quarter: value.quarter,
    week: value.week,
    nextWeek: value.next_week,
  }
}

export async function getBoard(quarter: string, week: string, surface: BoardSurface = 'okr'): Promise<BoardData> {
  const params = new URLSearchParams()
  if (quarter) params.set('quarter', quarter)
  if (surface === 'weekly-report' && week) params.set('week', week)
  const path = surface === 'weekly-report' ? '/api/biz-okr/board' : '/api/biz-okr/core-board'
  const board = await request<APIBoard>(`${path}?${params}`)
  return {
    quarter: board.quarter,
    week: board.week,
    templateKey: board.template_key,
	deleteToken: board.delete_token,
    previousWeek: board.previous_week,
    availableQuarters: board.available_quarters,
    availableWeeks: board.available_weeks,
    objectives: board.objectives.map((objective) => ({ id: objective.id, title: objective.title, version: objective.version, owners: (objective.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: objective.krs.map(fromAPIKr) })),
  }
}

export async function getGenericOKRBoard(quarter = '', signal?: AbortSignal): Promise<BoardData> {
  const params = new URLSearchParams()
  if (quarter) params.set('quarter', quarter)
  const board = await request<APIBoard>(`/api/okr/board?${params}`, { signal })
  return {
    quarter: board.quarter,
    week: board.week,
    templateKey: board.template_key,
    previousWeek: board.previous_week,
    availableQuarters: board.available_quarters,
    availableWeeks: board.available_weeks,
    objectives: board.objectives.map((objective) => ({ id: objective.id, title: objective.title, version: objective.version, owners: (objective.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: objective.krs.map(fromAPIKr) })),
  }
}

export async function getGenericOKRProgressBoard(quarter: string, week: string): Promise<BoardData> {
  const params = new URLSearchParams({ quarter, week })
  const board = await request<APIBoard>(`/api/okr/progress/board?${params}`)
  return {
    quarter: board.quarter,
    week: board.week,
    templateKey: board.template_key,
    previousWeek: board.previous_week,
    availableQuarters: board.available_quarters,
    availableWeeks: board.available_weeks,
    objectives: board.objectives.map((objective) => ({ id: objective.id, title: objective.title, version: objective.version, owners: (objective.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: objective.krs.map(fromAPIKr) })),
  }
}

export async function listOKRPlans(quarter = ''): Promise<OKRPlanList> {
  const params = new URLSearchParams()
  if (quarter) params.set('quarter', quarter)
  const value = await request<APIPlanList>(`/api/biz-okr/plans?${params}`)
  return {
    quarter: value.quarter,
    availableQuarters: value.available_quarters,
    plans: value.plans.map(fromAPIPlanSummary),
  }
}

function fromAPIRegionalDemand(value: APIRegionalDemand): RegionalDemand {
  const owners = (items: Array<{ email: string; name: string }> | null | undefined) => (items ?? []).map((item) => ({ email: item.email, name: item.name }))
  return { id: value.id, version: value.version, regionalOkr: value.regional_okr, item: value.item, requirement: value.requirement, docs: value.docs ?? [], images: value.images ?? [], priority: value.priority, regionalPocs: owners(value.regional_pocs), platformPocs: owners(value.platform_pocs), acceptance: value.acceptance, planKrIds: value.plan_kr_ids ?? [], deliverable: value.deliverable, sortOrder: value.sort_order }
}

function regionalDemandBody(value: Omit<RegionalDemand, 'id'>) {
  const owners = (items: KrOwner[]) => items.map((item) => ({ email: item.email, name: item.name }))
  return { expected_version: value.version, regional_okr: value.regionalOkr, item: value.item, requirement: value.requirement, docs: value.docs, images: value.images, priority: value.priority, regional_pocs: owners(value.regionalPocs), platform_pocs: owners(value.platformPocs), acceptance: value.acceptance, plan_kr_ids: value.planKrIds, deliverable: value.deliverable, sort_order: value.sortOrder }
}

function fromAPIRegionalDecision(value: APIRegionalDecision): RegionalPlanDecisionItem {
  return { planKrId: value.plan_kr_id, version: value.version, onboard: value.onboard, launchRegions: value.launch_regions ?? [], regionalPocs: (value.regional_pocs ?? []).map((item) => ({ email: item.email, name: item.name })), regionalOkr: value.regional_okr, hidden: value.hidden }
}

function fromAPIRegionalOverlay(value: APIRegionalRecapOverlay): RegionalRecapOverlay {
  return { bucketKey: value.bucket_key, objectiveId: value.objective_id, version: value.version, sortOrder: value.sort_order, hidden: value.hidden }
}

function fromAPIRegionalBoard(value: APIRegionalBoard): RegionalAlignmentBoard {
  return {
    alignment: { id: value.alignment.id, quarter: value.alignment.quarter, planId: value.alignment.plan_id, recapQuarter: value.alignment.recap_quarter, version: value.alignment.version },
    region: { regionCode: value.region.region_code, version: value.region.version, categoryOrder: value.region.category_order ?? [] },
    plan: fromAPIPlan(value.plan),
		recap: { quarter: value.recap.quarter, objectives: value.recap.objectives.map((objective) => ({ id: objective.id, title: objective.title, version: objective.version, owners: (objective.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: objective.krs.map(fromAPIKr) })) },
    demands: (value.demands ?? []).map(fromAPIRegionalDemand),
    decisions: (value.decisions ?? []).map(fromAPIRegionalDecision),
    recapOverlays: (value.recap_overlays ?? []).map(fromAPIRegionalOverlay),
    translations: value.translations ?? {},
  }
}

export async function getRegionalAlignmentBoard(quarter: string, region: RegionalCode): Promise<RegionalAlignmentBoard> {
  return fromAPIRegionalBoard(await request<APIRegionalBoard>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/board?quarter=${encodeURIComponent(quarter)}`))
}

export async function refreshRegionalAlignmentBoard(quarter: string, region: RegionalCode): Promise<RegionalAlignmentBoard> {
  const base = `/api/biz-okr/regional-alignments/${encodeURIComponent(region)}`
  const query = `quarter=${encodeURIComponent(quarter)}`
  let result = await request<APIRegionalRefreshResult>(`${base}/refresh?${query}`, { method: 'POST' })
  for (let attempt = 0; result.pending; attempt += 1) {
    if (attempt >= 600) throw new Error('区域 OKR 刷新超时 / Regional OKR refresh timed out')
    await new Promise<void>((resolve) => setTimeout(resolve, 2000))
    result = await request<APIRegionalRefreshResult>(`${base}/refresh-status?${query}`)
  }
  return fromAPIRegionalBoard(result.board)
}

export async function createRegionalDemand(quarter: string, region: RegionalCode, value: Omit<RegionalDemand, 'id'>): Promise<RegionalDemand> {
  return fromAPIRegionalDemand(await request<APIRegionalDemand>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/demands?quarter=${encodeURIComponent(quarter)}`, { method: 'POST', body: JSON.stringify(regionalDemandBody({ ...value, version: 0 })) }))
}

export async function updateRegionalDemand(quarter: string, region: RegionalCode, value: RegionalDemand): Promise<RegionalDemand> {
  return fromAPIRegionalDemand(await request<APIRegionalDemand>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/demands/${encodeURIComponent(value.id)}?quarter=${encodeURIComponent(quarter)}`, { method: 'PUT', body: JSON.stringify(regionalDemandBody(value)) }))
}

export async function deleteRegionalDemand(quarter: string, region: RegionalCode, value: RegionalDemand): Promise<void> {
  await request(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/demands/${encodeURIComponent(value.id)}?quarter=${encodeURIComponent(quarter)}`, { method: 'DELETE', body: JSON.stringify({ expected_version: value.version }) })
}

export async function putRegionalDecision(quarter: string, region: RegionalCode, value: RegionalPlanDecisionItem): Promise<RegionalPlanDecisionItem> {
  const result = await request<APIRegionalDecision>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/decisions/${encodeURIComponent(value.planKrId)}?quarter=${encodeURIComponent(quarter)}`, { method: 'PUT', body: JSON.stringify({ expected_version: value.version, onboard: value.onboard, launch_regions: value.launchRegions, regional_pocs: value.regionalPocs.map((item) => ({ email: item.email, name: item.name })), regional_okr: value.regionalOkr, hidden: value.hidden }) })
  return fromAPIRegionalDecision(result)
}

export async function putRegionalCategoryOrder(quarter: string, region: RegionalCode, expectedVersion: number, categoryOrder: string[]) {
  const value = await request<{ region_code: RegionalCode; version: number; category_order: string[] }>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/settings?quarter=${encodeURIComponent(quarter)}`, { method: 'PUT', body: JSON.stringify({ expected_version: expectedVersion, category_order: categoryOrder }) })
  return { regionCode: value.region_code, version: value.version, categoryOrder: value.category_order }
}

export async function putRegionalRecapOrder(quarter: string, region: RegionalCode, bucketKey: string, objectiveIds: string[]): Promise<RegionalRecapOverlay[]> {
  const values = await request<APIRegionalRecapOverlay[]>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/recap-order?quarter=${encodeURIComponent(quarter)}`, { method: 'PUT', body: JSON.stringify({ bucket_key: bucketKey, objective_ids: objectiveIds }) })
  return values.map(fromAPIRegionalOverlay)
}

export async function patchRegionalRecap(quarter: string, region: RegionalCode, bucketKey: string, objectiveId: string, expectedVersion: number, hidden: boolean): Promise<RegionalRecapOverlay> {
  const value = await request<APIRegionalRecapOverlay>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/recap/${encodeURIComponent(objectiveId)}?quarter=${encodeURIComponent(quarter)}&bucket=${encodeURIComponent(bucketKey)}`, { method: 'PATCH', body: JSON.stringify({ expected_version: expectedVersion, hidden }) })
  return fromAPIRegionalOverlay(value)
}

export async function getOKRPlan(id: string): Promise<OKRPlan> {
  return fromAPIPlan(await request<APIPlan>(`/api/biz-okr/plans/${encodeURIComponent(id)}`))
}

export async function createOKRPlan(input: { quarter: string; title: string }): Promise<OKRPlan> {
  return fromAPIPlan(await request<APIPlan>('/api/biz-okr/plans', {
    method: 'POST',
    body: JSON.stringify({
      quarter: input.quarter,
      title: input.title,
    }),
  }))
}

export async function createOKRPlanObjective(planId: string, objective: Objective): Promise<OKRPlan> {
  return fromAPIPlan(await request<APIPlan>(`/api/biz-okr/plans/${encodeURIComponent(planId)}/objectives`, {
    method: 'POST',
    body: JSON.stringify(toAPIPlanObjective(objective)),
  }))
}

export async function updateOKRPlanObjective(planId: string, objective: Objective): Promise<OKRPlan> {
  try {
    return fromAPIPlan(await request<APIPlan>(`/api/biz-okr/plans/${encodeURIComponent(planId)}/objectives/${encodeURIComponent(objective.id)}`, {
      method: 'PATCH',
		body: JSON.stringify({
			expected_version: objective.version ?? 0,
			expected_structure_token: objective.structureToken ?? '',
			objective: toAPIPlanObjective(objective),
		}),
    }))
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIPlan(error.data as APIPlan), error.logid)
    }
    throw error
  }
}

export async function updateOKRPlanKR(planId: string, kr: Kr): Promise<OKRPlan> {
  try {
    return fromAPIPlan(await request<APIPlan>(`/api/biz-okr/plans/${encodeURIComponent(planId)}/krs/${encodeURIComponent(kr.id)}`, {
      method: 'PATCH',
		body: JSON.stringify({
			expected_version: kr.version ?? 0,
			expected_structure_token: kr.structureToken ?? '',
			kr: toAPIPlanKR(kr),
		}),
    }))
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIPlan(error.data as APIPlan), error.logid)
    }
    throw error
  }
}

export async function patchPointDefinition(input: { pointId: string; planId?: string; expectedVersion: number; title?: string; owners?: KrOwner[]; kind?: PointKind; meegoWorkItemId?: string; meegoUrl?: string; tags?: KrTag[] }): Promise<PointDefinitionPatchResult> {
  const path = input.planId
    ? `/api/biz-okr/plans/${encodeURIComponent(input.planId)}/points/${encodeURIComponent(input.pointId)}/definition`
    : `/api/okr/points/${encodeURIComponent(input.pointId)}/definition`
	  const body: { expected_version: number; title?: string; owners?: Array<{ email: string; name: string }>; kind?: PointKind; meego_work_item_id?: string; meego_url?: string; tags?: KrTag[] } = { expected_version: input.expectedVersion }
	  if (input.title !== undefined) body.title = input.title
	  if (input.owners !== undefined) body.owners = input.owners.map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId }))
	  if (input.kind !== undefined) body.kind = input.kind
	  if (input.meegoWorkItemId !== undefined) body.meego_work_item_id = input.meegoWorkItemId
	  if (input.meegoUrl !== undefined) body.meego_url = input.meegoUrl
	  if (input.tags !== undefined) body.tags = input.tags
	try {
		return fromAPIPointDefinitionPatchResult(await request<APIPointDefinitionPatchResult>(path, { method: 'PATCH', body: JSON.stringify(body) }))
	} catch (error) {
		if (error instanceof APIError && error.status === 409 && error.data) {
			throw new APIError(error.message, error.status, error.code, fromAPIPointDefinitionPatchResult(error.data as APIPointDefinitionPatchResult), error.logid)
		}
		throw error
	}
}

function fromAPIPointDefinitionPatchResult(value: APIPointDefinitionPatchResult): PointDefinitionPatchResult {
	return {
	    pointId: value.point_id,
	    version: value.version,
		structureToken: value.structure_token,
		krStructureToken: value.kr_structure_token,
		deleteToken: value.delete_token,
	    planDeleteToken: value.plan_delete_token,
	    title: value.title,
		kind: value.kind,
		meegoWorkItemId: value.meego_work_item_id,
		meegoUrl: value.meego_url,
	    owners: (value.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
		tags: value.tags ?? [],
	}
}

export async function deleteOKRPlanObjective(planId: string, objective: Objective): Promise<void> {
  try {
    await request(`/api/biz-okr/plans/${encodeURIComponent(planId)}/objectives/${encodeURIComponent(objective.id)}`, {
      method: 'DELETE',
		body: JSON.stringify({ expected_version: objective.version ?? 0, expected_structure_token: objective.structureToken ?? '' }),
    })
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIPlan(error.data as APIPlan), error.logid)
    }
    throw error
  }
}

export async function reorderOKRPlanObjectives(planId: string, ids: string[], expectedVersion: number): Promise<OKRPlan> {
  try {
    return fromAPIPlan(await request<APIPlan>(`/api/biz-okr/plans/${encodeURIComponent(planId)}/objectives/order`, {
      method: 'PUT',
      body: JSON.stringify({ ids, expected_version: expectedVersion }),
    }))
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIPlan(error.data as APIPlan), error.logid)
    }
    throw error
  }
}

export async function deleteOKRPlan(id: string, deleteToken: string): Promise<void> {
	await request<{ id: string }>(`/api/biz-okr/plans/${encodeURIComponent(id)}`, {
		method: 'DELETE',
		body: JSON.stringify({ delete_token: deleteToken }),
	})
}

export async function getOKRActivities(input: {
  surface: 'plan' | 'weekly'
  quarter?: string
  week?: string
  planId?: string
  limit?: number
}): Promise<OKRActivityEntry[]> {
  const params = new URLSearchParams({ surface: input.surface, limit: String(input.limit ?? 50) })
  if (input.quarter) params.set('quarter', input.quarter)
  if (input.week) params.set('week', input.week)
  if (input.planId) params.set('plan_id', input.planId)
  const value = await request<{ items: APIActivityEntry[] }>(`/api/biz-okr/activity?${params}`)
  return value.items.map((item) => ({
    at: item.at,
    actorId: item.actor_id,
    actorName: item.actor_name,
    surface: item.surface,
    quarter: item.quarter,
    week: item.week,
    planId: item.plan_id,
    action: item.action,
    targetId: item.target_id,
    summary: item.summary,
  }))
}

function fromAPIComment(value: APIPageComment): PageComment {
  return {
    id: value.id,
    version: value.version,
    deleteToken: value.delete_token,
    planId: value.plan_id,
    alignmentId: value.alignment_id,
    regionCode: value.region_code,
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
    mentions: (value.mentions ?? []).map((mention) => ({ email: mention.email, name: mention.name })),
    images: value.images ?? [],
    notificationErrors: value.notification_errors,
    notifications: value.notifications,
    todo: value.todo ?? false,
    resolved: value.resolved ?? false,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
    replies: (value.replies ?? []).map(fromAPIComment),
  }
}

// runPreviewReview asks the advisory review agent for one Markdown report. It
// is synchronous on purpose: the review creates no Task, so there is nothing to
// poll — the request stays open until the agent answers.
export async function runPreviewReview(input: {
  reviewType: 'plan' | 'progress'
  quarter: string
  week?: string
  planId?: string
  kind: 'all' | 'objective' | 'kr' | 'point'
  objectiveId?: string
  krId?: string
  pointId?: string
}, signal?: AbortSignal): Promise<string> {
  const value = await request<{ content: string }>('/api/biz-okr/preview-review', {
    method: 'POST',
    body: JSON.stringify({
      review_type: input.reviewType,
      quarter: input.quarter,
      week: input.week ?? '',
      plan_id: input.planId ?? '',
      kind: input.kind,
      objective_id: input.objectiveId ?? '',
      kr_id: input.krId ?? '',
      point_id: input.pointId ?? '',
    }),
    signal,
  })
  return value.content
}

export async function getComments(quarter: string, week: string): Promise<PageCommentList> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIPageCommentList>(`/api/biz-okr/comments?${params}`)
  return { quarter: value.quarter, week: value.week, count: value.count, comments: value.comments.map(fromAPIComment) }
}

export async function getPlanComments(planId: string): Promise<PageCommentList> {
  const value = await request<APIPageCommentList>(`/api/biz-okr/plans/${encodeURIComponent(planId)}/comments`)
  return { quarter: value.quarter, planId: value.plan_id, count: value.count, comments: value.comments.map(fromAPIComment) }
}

export async function getRegionalAlignmentComments(quarter: string, region: RegionalCode): Promise<PageCommentList> {
  const value = await request<APIPageCommentList>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/comments?quarter=${encodeURIComponent(quarter)}`)
  return { quarter: value.quarter, count: value.count, comments: value.comments.map(fromAPIComment) }
}

function fromAPIFollowUp(value: APIFollowUpItem): FollowUpItem {
  return {
    id: value.id,
    quarter: value.quarter,
    week: value.week,
    version: value.version,
    topic: value.topic,
    owners: value.owners.map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
    status: value.status,
    assignDate: value.assign_date,
    update: value.update,
    sourceKey: value.source_key,
    sourcePayload: value.source_payload,
    sortOrder: value.sort_order,
    createdBy: value.created_by,
    updatedBy: value.updated_by,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
  }
}

function followUpBody(item: FollowUpItem) {
  return {
    id: item.id,
    expected_version: item.version,
    quarter: item.quarter,
    week: item.week,
    topic: item.topic,
    owners: item.owners.map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
    status: item.status,
    assign_date: item.assignDate,
    update: item.update,
    source_key: item.sourceKey ?? '',
    source_payload: item.sourcePayload ?? {},
    sort_order: item.sortOrder,
  }
}

export async function getFollowUps(quarter: string, week: string): Promise<FollowUpList> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIFollowUpList>(`/api/biz-okr/follow-ups?${params}`)
  return { quarter: value.quarter, week: value.week, count: value.count, items: value.items.map(fromAPIFollowUp) }
}

export async function createFollowUp(item: FollowUpItem): Promise<FollowUpItem> {
  const value = await request<APIFollowUpItem>('/api/biz-okr/follow-ups', {
    method: 'POST',
    body: JSON.stringify(followUpBody({ ...item, version: 0 })),
  })
  return fromAPIFollowUp(value)
}

export async function updateFollowUp(item: FollowUpItem): Promise<FollowUpItem> {
  try {
    const value = await request<APIFollowUpItem>(`/api/biz-okr/follow-ups/${encodeURIComponent(item.id)}`, {
      method: 'PUT',
      body: JSON.stringify(followUpBody(item)),
    })
    return fromAPIFollowUp(value)
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIFollowUp(error.data as APIFollowUpItem), error.logid)
    }
    throw error
  }
}

export async function deleteFollowUp(item: FollowUpItem): Promise<void> {
  await request<{ id: string }>(`/api/biz-okr/follow-ups/${encodeURIComponent(item.id)}`, {
    method: 'DELETE',
    body: JSON.stringify({ expected_version: item.version }),
  })
}

export async function createComment(input: {
  quarter: string
  week: string
  sourceTab: string
  parentId?: string
  content: string
  mentions?: CommentMention[]
  images?: ImageRef[]
  targetType?: PageComment['targetType']
  targetId?: string
  targetTitle?: string
  selectedText?: string
  selectionStart?: number
  selectionEnd?: number
  selectionPrefix?: string
  selectionSuffix?: string
}): Promise<PageComment> {
  const value = await request<APIPageComment>('/api/biz-okr/comments', {
    method: 'POST',
    body: JSON.stringify({
      quarter: input.quarter,
      week: input.week,
      source_tab: input.sourceTab,
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
      mentions: (input.mentions ?? []).map((mention) => ({ email: mention.email, name: mention.name })),
      images: input.images ?? [],
    }),
  })
  return fromAPIComment(value)
}

export async function createPlanComment(planId: string, input: {
  parentId?: string
  content: string
  mentions?: CommentMention[]
  images?: ImageRef[]
  targetType?: PageComment['targetType']
  targetId?: string
  targetTitle?: string
  selectedText?: string
  selectionStart?: number
  selectionEnd?: number
  selectionPrefix?: string
  selectionSuffix?: string
}): Promise<PageComment> {
  const value = await request<APIPageComment>(`/api/biz-okr/plans/${encodeURIComponent(planId)}/comments`, {
    method: 'POST',
    body: JSON.stringify({
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
      mentions: (input.mentions ?? []).map((mention) => ({ email: mention.email, name: mention.name })),
      images: input.images ?? [],
    }),
  })
  return fromAPIComment(value)
}

export async function createRegionalAlignmentComment(quarter: string, region: RegionalCode, input: {
  parentId?: string
  content: string
  mentions?: CommentMention[]
  images?: ImageRef[]
  targetType?: PageComment['targetType']
  targetId?: string
  targetTitle?: string
}): Promise<PageComment> {
  const value = await request<APIPageComment>(`/api/biz-okr/regional-alignments/${encodeURIComponent(region)}/comments?quarter=${encodeURIComponent(quarter)}`, {
    method: 'POST',
    body: JSON.stringify({ parent_id: input.parentId ?? '', target_type: input.targetType ?? 'page', target_id: input.targetId ?? '', target_title: input.targetTitle ?? '', content: input.content, mentions: (input.mentions ?? []).map((mention) => ({ email: mention.email, name: mention.name })), images: input.images ?? [] }),
  })
  return fromAPIComment(value)
}

function fromAPIProductFeedbackPerson(value: APIProductFeedbackPerson) {
  return { name: value.name, email: value.email, avatarUrl: value.avatar_url }
}

function fromAPIProductFeedback(value: APIProductFeedback): ProductFeedback {
  return {
    id: value.id,
    version: value.version,
    title: value.title,
    content: value.content,
    images: value.images ?? [],
    sourceContext: value.source_context ?? {},
    author: fromAPIProductFeedbackPerson(value.author),
    resolved: value.resolved,
    resolvedBy: value.resolved_by ? fromAPIProductFeedbackPerson(value.resolved_by) : undefined,
    resolvedAt: value.resolved_at,
    createdAt: value.created_at,
    updatedAt: value.updated_at,
    replies: (value.replies ?? []).map((reply) => ({ id: reply.id, content: reply.content, author: fromAPIProductFeedbackPerson(reply.author), createdAt: reply.created_at })),
    plusOnes: (value.plus_ones ?? []).map(fromAPIProductFeedbackPerson),
    myPlusOne: value.my_plus_one,
    canResolve: value.can_resolve,
  }
}

export async function getProductFeedback(input: { resolved: boolean; sort: 'latest' | 'popular' }): Promise<ProductFeedbackList> {
  const params = new URLSearchParams({ resolved: String(input.resolved), sort: input.sort })
  const value = await request<APIProductFeedbackList>(`/api/biz-okr/feedback?${params}`)
  return { total: value.total, items: value.items.map(fromAPIProductFeedback) }
}

export async function createProductFeedback(input: { title: string; content: string; images: ImageRef[]; sourceContext: Record<string, unknown> }): Promise<ProductFeedback> {
  const value = await request<APIProductFeedback>('/api/biz-okr/feedback', {
    method: 'POST',
    body: JSON.stringify({ title: input.title, content: input.content, images: input.images, source_context: input.sourceContext }),
  })
  return fromAPIProductFeedback(value)
}

export async function replyProductFeedback(feedbackId: string, content: string): Promise<ProductFeedback> {
  const value = await request<APIProductFeedback>(`/api/biz-okr/feedback/${encodeURIComponent(feedbackId)}/replies`, {
    method: 'POST', body: JSON.stringify({ content }),
  })
  return fromAPIProductFeedback(value)
}

export async function setProductFeedbackPlusOne(feedbackId: string, enabled: boolean): Promise<ProductFeedback> {
  const value = await request<APIProductFeedback>(`/api/biz-okr/feedback/${encodeURIComponent(feedbackId)}/plus-one`, {
    method: enabled ? 'PUT' : 'DELETE',
  })
  return fromAPIProductFeedback(value)
}

export async function setProductFeedbackResolved(feedback: Pick<ProductFeedback, 'id' | 'version'>, resolved: boolean): Promise<ProductFeedback> {
  const value = await request<APIProductFeedback>(`/api/biz-okr/feedback/${encodeURIComponent(feedback.id)}/status`, {
    method: 'PATCH', body: JSON.stringify({ expected_version: feedback.version, resolved }),
  })
  return fromAPIProductFeedback(value)
}

export async function getAuthStatus(): Promise<AuthStatus> {
  const value = await request<APIAuthStatus>('/api/biz-okr/me')
  return {
    authenticated: value.authenticated,
    configured: value.configured,
		managementAccess: value.management_access,
		regionalAutoMatchAccess: value.regional_auto_match_access,
    expiresAt: value.expires_at,
    user: value.user ? {
      openId: value.user.open_id,
      unionId: value.user.union_id,
      name: value.user.name,
      avatarUrl: value.user.avatar_url,
      email: value.user.email,
    } : undefined,
  }
}

export async function beginFeishuLogin(): Promise<FeishuDeviceLogin> {
  const value = await request<APIFeishuDeviceLogin>('/api/biz-okr/auth/feishu/device', { method: 'POST' })
  return {
    loginId: value.login_id,
    verificationUrl: value.verification_url,
    userCode: value.user_code,
    expiresAt: value.expires_at,
    pollIntervalSeconds: value.poll_interval_seconds,
  }
}

export async function pollFeishuLogin(loginId: string): Promise<FeishuDeviceLoginPoll> {
  const value = await request<APIFeishuDeviceLoginPoll>(`/api/biz-okr/auth/feishu/device/${encodeURIComponent(loginId)}/poll`, { method: 'POST' })
  return {
    status: value.status,
    retryAfterSeconds: value.retry_after_seconds,
    user: value.user ? {
      openId: value.user.open_id,
      unionId: value.user.union_id,
      name: value.user.name,
      avatarUrl: value.user.avatar_url,
      email: value.user.email,
    } : undefined,
  }
}

export async function logout(): Promise<void> {
  await request<{ logged_out: boolean }>('/api/biz-okr/auth/logout', { method: 'POST' })
}

export async function updateComment(comment: Pick<PageComment, 'id' | 'version'>, patch: { content?: string; mentions?: CommentMention[]; images?: ImageRef[]; todo?: boolean; resolved?: boolean }): Promise<PageComment> {
  const value = await request<APIPageComment>(`/api/biz-okr/comments/${encodeURIComponent(comment.id)}`, {
    method: 'PUT',
    body: JSON.stringify({
      ...patch,
      expected_version: comment.version,
      mentions: patch.mentions?.map((mention) => ({ email: mention.email, name: mention.name })),
    }),
  })
  return fromAPIComment(value)
}

export async function deleteComment(comment: Pick<PageComment, 'id' | 'version' | 'deleteToken'>): Promise<void> {
  await request<{ id: string }>(`/api/biz-okr/comments/${encodeURIComponent(comment.id)}`, {
    method: 'DELETE',
    body: JSON.stringify({ expected_version: comment.version, delete_token: comment.deleteToken }),
  })
}

export async function getEnums(): Promise<EnumValues> {
	const value = await request<APIEnums>('/api/okr/enums')
	return { statuses: value.statuses, pointKinds: value.point_kinds, lights: value.lights }
}

export async function getReminderPreview(quarter: string, week: string): Promise<ReminderPreview> {
  const params = new URLSearchParams({ quarter, week })
  const value = await request<APIReminderPreview>(`/api/biz-okr/reminder-preview?${params}`)
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
      ownerEmail: recipient.owner_email,
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
      ownerEmail: recipient.owner_email,
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
  const value = await request<APIReminderBatchList>(`/api/biz-okr/reminder-batches?${params}`)
  return { mode: value.mode, sendEnabled: value.send_enabled, batches: value.batches.map(fromAPIReminderBatch) }
}

export async function generateReminderBatch(quarter: string, week: string): Promise<ReminderBatch> {
  const value = await request<APIReminderBatch>('/api/biz-okr/reminder-batches/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ quarter, week }),
  })
  return fromAPIReminderBatch(value)
}

export async function createFeishuDocument(title: string, content: string): Promise<FeishuDocumentResult> {
	const value = await request<{ document_id: string; url: string; warnings?: string[] }>('/api/biz-okr/feishu-documents', {
		method: 'POST',
		body: JSON.stringify({ title, content }),
	})
	return { documentId: value.document_id, url: value.url, warnings: value.warnings ?? [] }
}

export async function getMeegoPreview(pointId: string, week: string): Promise<MeegoPreview> {
  const params = new URLSearchParams({ week })
  const value = await request<APIMeegoPreview>(`/api/biz-okr/points/${encodeURIComponent(pointId)}/meego-preview?${params}`)
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
  const value = await request<APIMeegoBatchPreview>(`/api/biz-okr/meego-preview?${params}`)
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
		progressVersion: item.progress_version,
      owners: item.owners.map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })),
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
    const value = await request<APIKr>(`/api/biz-okr/points/${encodeURIComponent(input.pointId)}/meego-confirm`, {
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

function progressPayload(entry: Entry, week: string) {
	return {
		id: entry.id,
		expected_version: entry.version ?? 0,
		week,
		status: entry.status,
		text: entry.text,
		docs: entry.docs,
		images: entry.images,
		source: entry.source ?? 'manual',
		needs_review: entry.needsReview ?? false,
	}
}

export async function createProgress(pointId: string, entry: Entry, week: string): Promise<Kr> {
	return progressRequest(`/api/okr/points/${encodeURIComponent(pointId)}/progress`, {
		method: 'POST',
		body: JSON.stringify(progressPayload(entry, week)),
	})
}

export async function updateProgress(entry: Entry, week: string): Promise<Kr> {
	return progressRequest(`/api/okr/progress/${encodeURIComponent(entry.id)}`, {
		method: 'PUT',
		body: JSON.stringify(progressPayload(entry, week)),
	})
}

export async function deleteProgress(entry: Entry): Promise<Kr> {
	return progressRequest(`/api/okr/progress/${encodeURIComponent(entry.id)}`, {
		method: 'DELETE',
		body: JSON.stringify({ expected_version: entry.version ?? 0 }),
	})
}

export async function replaceWeeklyKRCore(kr: Kr, week: string): Promise<Kr> {
	try {
		const value = await request<APIKr>(`/api/okr/krs/${encodeURIComponent(kr.id)}/weekly-core`, {
			method: 'PUT',
			body: JSON.stringify({
				expected_version: kr.weeklyCoreVersion ?? 0,
				week,
				metric_note: kr.metricNote,
				metrics: kr.metrics,
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

export async function replaceWeeklyScore(input: { quarter: string; week: string; targetKind: 'kr' | 'point'; targetId: string; score: number; expectedVersion: number }): Promise<Kr> {
	return progressRequest(`/api/biz-okr/scores/${encodeURIComponent(input.targetKind)}/${encodeURIComponent(input.targetId)}`, {
		method: 'PUT',
		body: JSON.stringify({ quarter: input.quarter, week: input.week, score: input.score, expected_version: input.expectedVersion }),
	})
}

export async function deleteWeeklyScore(input: { quarter: string; week: string; targetKind: 'kr' | 'point'; targetId: string; expectedVersion: number }): Promise<Kr> {
	return progressRequest(`/api/biz-okr/scores/${encodeURIComponent(input.targetKind)}/${encodeURIComponent(input.targetId)}`, {
		method: 'DELETE',
		body: JSON.stringify({ quarter: input.quarter, week: input.week, expected_version: input.expectedVersion }),
	})
}

async function progressRequest(path: string, init: RequestInit): Promise<Kr> {
	try {
		return fromAPIKr(await request<APIKr>(path, init))
	} catch (error) {
		if (error instanceof APIError && error.status === 409 && error.data) {
			throw new APIError(error.message, error.status, error.code, fromAPIKr(error.data as APIKr), error.logid)
		}
		throw error
	}
}

// Writes only fields owned by the KR. Concrete KR rows are always saved via
// patchPointDefinition and never ride along in this parent request.
export async function replaceKRDefinition(kr: Kr): Promise<Kr> {
	const body = {
		expected_version: kr.version ?? 0,
		title: kr.title,
		owners: (kr.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
	}
	return fromAPIKr(await request<APIKr>(`/api/okr/krs/${encodeURIComponent(kr.id)}/definition`, {
		method: 'PUT',
		body: JSON.stringify(body),
	}))
}

export async function getWeeklyKR(krId: string, week: string): Promise<Kr> {
	return fromAPIKr(await request<APIKr>(`/api/biz-okr/krs/${encodeURIComponent(krId)}/weekly?week=${encodeURIComponent(week)}`))
}

export async function replaceKR(kr: Kr): Promise<Kr> {
  try {
		const body = {
			expected_version: kr.version ?? 0,
			delete_token: kr.deleteToken ?? '',
			title: kr.title,
			owners: (kr.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
			metric_note: kr.metricNote,
      metrics: kr.metrics,
      points: kr.points.map((point) => ({
        id: point.id,
				version: point.version,
				...(point.version === undefined ? {
					kind: point.kind,
					title: point.title,
					meego_work_item_id: point.meegoWorkItemId ?? '',
					meego_url: point.meegoUrl ?? '',
					owners: (point.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
					tags: point.tags ?? [],
				} : {}),
      })),
      tags: kr.tags ?? [],
    }
	const value = await request<APIKr>(`/api/biz-okr/krs/${encodeURIComponent(kr.id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    })
    return fromAPIKr(value)
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
      throw new APIError(error.message, error.status, error.code, fromAPIKr(error.data as APIKr), error.logid)
    }
    throw error
  }
}

export async function getPeopleAvatars(emails: string[], signal?: AbortSignal): Promise<{ people: PersonAvatarItem[]; failedEmails: string[] }> {
  const value = await request<{ people: Array<{ email: string; name: string; avatar_url: string }>; failed_emails: string[] }>(`/api/biz-okr/people/avatars?emails=${encodeURIComponent(emails.join(','))}`, { signal })
  return {
    people: value.people.map((item) => ({ email: item.email, name: item.name, avatarUrl: item.avatar_url })),
    failedEmails: value.failed_emails ?? [],
  }
}

export async function createObjective(input: { quarter: string; title: string; owners?: KrOwner[] }): Promise<Objective> {
	const value = await request<{ id: string; title: string; version: number; owners?: Array<{ email: string; name: string; union_id?: string }>; krs: APIKr[] }>('/api/okr/objectives', {
    method: 'POST',
		body: JSON.stringify({ ...input, owners: (input.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })) }),
  })
	return { id: value.id, title: value.title, version: value.version, owners: (value.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: value.krs.map(fromAPIKr) }
}

export async function updateObjective(objective: Pick<Objective, 'id' | 'version'>, title: string, owners?: KrOwner[]): Promise<Objective> {
  try {
		const value = await request<{ id: string; title: string; version: number; owners?: Array<{ email: string; name: string; union_id?: string }>; krs: APIKr[] }>(`/api/okr/objectives/${encodeURIComponent(objective.id)}`, {
      method: 'PUT',
			body: JSON.stringify({ expected_version: objective.version ?? 0, title, ...(owners ? { owners: owners.map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })) } : {}) }),
    })
		return { id: value.id, title: value.title, version: value.version, owners: (value.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: value.krs.map(fromAPIKr) }
  } catch (error) {
    if (error instanceof APIError && error.status === 409 && error.data) {
			const current = error.data as { id: string; title: string; version: number; owners?: Array<{ email: string; name: string; union_id?: string }>; krs: APIKr[] }
			throw new APIError(error.message, error.status, error.code, { id: current.id, title: current.title, version: current.version, owners: (current.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, unionId: owner.union_id })), krs: current.krs.map(fromAPIKr) }, error.logid)
    }
    throw error
  }
}

export async function reorderObjectives(quarter: string, objectiveIds: string[], expectedOrder: string[]): Promise<string[]> {
  const value = await request<{ order: string[] }>('/api/okr/objectives/order', {
    method: 'PUT',
    body: JSON.stringify({ quarter, objective_ids: objectiveIds, expected_order: expectedOrder }),
  })
  return value.order
}

export async function reorderKRs(objectiveId: string, krIds: string[], expectedOrder: string[]): Promise<string[]> {
  const value = await request<{ order: string[] }>(`/api/okr/objectives/${encodeURIComponent(objectiveId)}/kr-order`, {
    method: 'PUT',
    body: JSON.stringify({ kr_ids: krIds, expected_order: expectedOrder }),
  })
  return value.order
}

export async function deleteObjective(objective: Pick<Objective, 'id' | 'version'>): Promise<void> {
  await request<{ id: string }>(`/api/okr/objectives/${encodeURIComponent(objective.id)}`, {
    method: 'DELETE',
    body: JSON.stringify({ expected_version: objective.version ?? 0 }),
  })
}

export async function createKR(objectiveId: string, input: { title: string; owners?: KrOwner[]; businessCategory: string; priority: KrPriority }): Promise<Kr> {
		const value = await request<APIKr>(`/api/biz-okr/objectives/${encodeURIComponent(objectiveId)}/krs`, {
			method: 'POST',
			body: JSON.stringify({
				title: normalizeKRTitle(input.title),
				owners: (input.owners ?? []).map((owner) => ({ email: owner.email, name: owner.name, union_id: owner.unionId })),
			tags: [
				{ type: 'business_category', value: input.businessCategory },
				{ type: 'priority', value: input.priority },
			],
		}),
  })
  return fromAPIKr(value)
}

export async function deleteKR(kr: Kr): Promise<void> {
	await request<{ id: string }>(`/api/okr/krs/${encodeURIComponent(kr.id)}`, {
		method: 'DELETE',
		body: JSON.stringify({ expected_version: kr.version ?? 0, delete_token: kr.deleteToken ?? '' }),
  })
}

export interface OKRDirectoryCandidate {
 email: string
 name: string
 union_id?: string
 avatar_url?: string
 department: string
 is_external: boolean
 has_chatted: boolean
}

const cachedOKRPeopleSearch = createPeopleSearchCache<OKRDirectoryCandidate>(async (query) => {
 const result = await request<{candidates: OKRDirectoryCandidate[];has_more:boolean}>(`/api/biz-okr/people/search?q=${encodeURIComponent(query)}`)
 return {...result, candidates: result.candidates.map(person => ({...person, department: person.department ?? '', is_external:false, has_chatted:false}))}
})

export async function searchOKRPeople(query: string, signal?: AbortSignal): Promise<{ candidates: OKRDirectoryCandidate[]; has_more: boolean }> {
 return cachedOKRPeopleSearch(query, signal)
}
export async function retryCommentNotifications(id: string, email: string, resendUnknown = false): Promise<CommentDelivery[]> {
	return request<CommentDelivery[]>(`/api/biz-okr/comments/${encodeURIComponent(id)}/notifications/retry`, { method:'POST', body:JSON.stringify({email, resend_unknown: resendUnknown}) })
}
