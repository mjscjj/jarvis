import type { Entry, Kr, KrOwner, KrTag, MetricLine, Objective, Point } from './types'

export interface ConflictField {
  label: string
  local: string
  remote: string
}

function same(left: unknown, right: unknown): boolean {
  return JSON.stringify(left ?? null) === JSON.stringify(right ?? null)
}

function owners(values?: KrOwner[]): string {
  return values?.map((owner) => owner.name || owner.email).filter(Boolean).join('、') || '无'
}

function tags(values?: KrTag[]): string {
  return values?.map((tag) => `${tag.type}：${tag.value}`).join('\n') || '无'
}

function metrics(values: MetricLine[]): string {
  return values.map((metric, index) => `${index + 1}. ${metric.text || '（空）'}${metric.light ? ` · ${metric.light}` : ''}`).join('\n') || '无'
}

function entries(values: Entry[]): string {
  return values.map((entry, index) => `${index + 1}. ${entry.status} · ${entry.text || '（空）'}`).join('\n') || '无'
}

function plain(value: unknown): string {
  if (value === undefined || value === null || value === '') return '未填写'
  return String(value)
}

function add(fields: ConflictField[], label: string, local: unknown, remote: unknown, format: (value: unknown) => string = plain) {
  if (same(local, remote)) return
  fields.push({ label, local: format(local), remote: format(remote) })
}

function pointFields(fields: ConflictField[], local?: Point, remote?: Point) {
  const title = local?.title || remote?.title || local?.id || remote?.id || '未命名条目'
  if (!local || !remote) {
    fields.push({ label: `具体条目「${title}」`, local: local ? '保留' : '已删除', remote: remote ? '保留' : '已删除' })
    return
  }
  add(fields, `具体条目「${title}」/ 文案`, local.title, remote.title)
  add(fields, `具体条目「${title}」/ 负责人`, local.owners, remote.owners, (value) => owners(value as KrOwner[]))
  add(fields, `具体条目「${title}」/ 类型`, local.kind, remote.kind)
  add(fields, `具体条目「${title}」/ Meego 链接`, local.meegoUrl, remote.meegoUrl)
  add(fields, `具体条目「${title}」/ 标签`, local.tags, remote.tags, (value) => tags(value as KrTag[]))
  add(fields, `具体条目「${title}」/ 本周进展`, local.entries, remote.entries, (value) => entries(value as Entry[]))
}

export function buildKrConflictFields(local: Kr, remote: Kr): ConflictField[] {
  const fields: ConflictField[] = []
  add(fields, 'KR 文案', local.title, remote.title)
  add(fields, '负责人', local.owners, remote.owners, (value) => owners(value as KrOwner[]))
  add(fields, '指标口径', local.metricNote, remote.metricNote)
  add(fields, '核心指标', local.metrics, remote.metrics, (value) => metrics(value as MetricLine[]))
  add(fields, 'KR 标签', local.tags, remote.tags, (value) => tags(value as KrTag[]))
  const localPoints = new Map(local.points.map((point) => [point.id, point]))
  const remotePoints = new Map(remote.points.map((point) => [point.id, point]))
  for (const id of new Set([...localPoints.keys(), ...remotePoints.keys()])) pointFields(fields, localPoints.get(id), remotePoints.get(id))
  return fields
}

export function localConflictCopyText(location: string, fields: ConflictField[]): string {
  return [`冲突位置：${location}`, ...fields.flatMap((field) => ['', field.label, field.local])].join('\n')
}

export function krConflictLocation(objectives: Objective[], krId: string, pointId?: string): string {
  for (const [objectiveIndex, objective] of objectives.entries()) {
    const krIndex = objective.krs.findIndex((kr) => kr.id === krId)
    if (krIndex < 0) continue
    const kr = objective.krs[krIndex]
    const parts = [`O${objectiveIndex + 1}「${objective.title || '未命名'}」`, `KR${krIndex + 1}「${kr.title || '未命名'}」`]
    if (pointId) {
      const point = kr.points.find((item) => item.id === pointId)
      parts.push(`具体条目「${point?.title || pointId}」`)
    }
    return parts.join(' / ')
  }
  return pointId ? `KR ${krId} / 具体条目 ${pointId}` : `KR ${krId}`
}
