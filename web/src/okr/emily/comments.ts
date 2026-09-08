import type { CommentTarget, Kr, Objective, PageComment, Point } from './types'
import { KINDS } from './rows.ts'

export interface CommentOKRContext {
  objective?: { id: string; title: string }
  kr?: { id: string; title: string }
}

export type CommentOKRContextIndex = Record<string, CommentOKRContext>

export function commentTargetKey(target: Pick<CommentTarget, 'type' | 'id'> | Pick<PageComment, 'targetType' | 'targetId'>) {
  if ('type' in target) return `${target.type}:${target.id}`
  return `${target.targetType}:${target.targetId ?? ''}`
}

export function commentMessageCount(comments: PageComment[]) {
  return comments.reduce((sum, comment) => sum + 1 + comment.replies.length, 0)
}

export interface CommentTargetGroup {
  key: string
  comments: PageComment[]
  messageCount: number
}

export function groupCommentsByTarget(comments: readonly PageComment[]): CommentTargetGroup[] {
  const groups = new Map<string, CommentTargetGroup>()
  for (const comment of comments) {
    const key = `${commentTargetKey(comment)}:${comment.selectedText ?? ''}:${comment.selectionStart ?? ''}:${comment.selectionEnd ?? ''}`
    const group = groups.get(key)
    if (group) {
      group.comments.push(comment)
      group.messageCount += 1 + comment.replies.length
      continue
    }
    groups.set(key, { key, comments: [comment], messageCount: 1 + comment.replies.length })
  }
  return [...groups.values()]
}

export function commentCountsByTarget(comments: PageComment[]) {
  const counts: Record<string, number> = {}
  for (const comment of comments) {
    const key = commentTargetKey(comment)
    counts[key] = (counts[key] ?? 0) + 1 + comment.replies.length
  }
  return counts
}

export type CommentDocumentOrder = ReadonlyMap<string, number>

export interface CommentTargetLocation {
  objective: Objective
  kr?: Kr
  point?: Point
}

export function findCommentTargetLocation(objectives: Objective[], target: Pick<CommentTarget, 'type' | 'id'> | Pick<PageComment, 'targetType' | 'targetId'>): CommentTargetLocation | undefined {
  const type = 'type' in target ? target.type : target.targetType
  const id = ('type' in target ? target.id : target.targetId) ?? ''
  for (const objective of objectives) {
    if (type === 'objective' && objective.id === id) return { objective }
    for (const kr of objective.krs) {
      if (type === 'kr' && kr.id === id) return { objective, kr }
      if (type === 'metric' && kr.metrics.some((metric) => metric.id === id)) return { objective, kr }
      for (const point of kr.points) {
        if (type === 'point' && point.id === id) return { objective, kr, point }
        if (type === 'entry' && [...point.entries, ...(point.previousEntries ?? [])].some((entry) => entry.id === id)) return { objective, kr, point }
      }
    }
  }
  return undefined
}

// The comment drawer is a document projection: its stable order follows the
// complete, unfiltered page model instead of comment creation time or the
// currently selected navigation/filter. Point kinds use the same KINDS order
// as the page renderers, so this index cannot drift into a second layout rule.
export function buildCommentDocumentOrder(objectives: Objective[], followUpIds: readonly string[] = []): CommentDocumentOrder {
  const order = new Map<string, number>()
  let position = 1 // Page-level comments occupy the implicit position 0.
  const add = (type: PageComment['targetType'], id: string) => {
    const key = `${type}:${id}`
    if (!order.has(key)) order.set(key, position++)
  }

  for (const id of followUpIds) add('follow_up', id)
  for (const objective of objectives) {
    add('objective', objective.id)
    for (const kr of objective.krs) {
      add('kr', kr.id)
      for (const metric of kr.metrics) add('metric', metric.id)
      for (const kind of KINDS) {
        for (const point of kr.points.filter((candidate) => candidate.kind === kind)) {
          add('point', point.id)
          for (const entry of point.entries) add('entry', entry.id)
          for (const entry of point.previousEntries ?? []) add('entry', entry.id)
        }
      }
    }
  }
  return order
}

function commentCreatedOrder(left: PageComment, right: PageComment) {
  const created = left.createdAt.localeCompare(right.createdAt)
  return created !== 0 ? created : left.id.localeCompare(right.id)
}

// Only root threads are passed here. Replies remain attached to their root and
// preserve the chronological order assembled by the API.
export function sortCommentsByDocumentOrder(comments: readonly PageComment[], order: CommentDocumentOrder): PageComment[] {
  return [...comments].sort((left, right) => {
    const leftPosition = left.targetType === 'page' ? 0 : order.get(commentTargetKey(left))
    const rightPosition = right.targetType === 'page' ? 0 : order.get(commentTargetKey(right))
    const leftAnchored = leftPosition !== undefined
    const rightAnchored = rightPosition !== undefined

    if (leftAnchored !== rightAnchored) return leftAnchored ? -1 : 1
    // Deleted or otherwise unknown targets have no honest document position.
    // Keep those historical threads together at the end in creation order.
    if (!leftAnchored || !rightAnchored) return commentCreatedOrder(left, right)
    if (leftPosition !== rightPosition) return leftPosition! - rightPosition!

    const leftSelection = Boolean(left.selectedText)
    const rightSelection = Boolean(right.selectedText)
    if (leftSelection !== rightSelection) return leftSelection ? 1 : -1
    if (leftSelection && rightSelection) {
      const start = (left.selectionStart ?? 0) - (right.selectionStart ?? 0)
      if (start !== 0) return start
      const end = (left.selectionEnd ?? 0) - (right.selectionEnd ?? 0)
      if (end !== 0) return end
    }
    return commentCreatedOrder(left, right)
  })
}

export function commentMatchesTarget(comment: PageComment, target: CommentTarget) {
  if (commentTargetKey(comment) !== commentTargetKey(target)) return false
  if (!target.selection) return true
  return comment.selectedText === target.selection.text
    && comment.selectionStart === target.selection.start
    && comment.selectionEnd === target.selection.end
}

export function commentTargetFromThread(comment: PageComment): CommentTarget {
  return {
    type: comment.targetType,
    id: comment.targetId ?? '',
    title: comment.targetTitle ?? comment.selectedText ?? '',
    commentId: comment.id,
    selection: comment.selectedText ? {
      text: comment.selectedText,
      start: comment.selectionStart ?? 0,
      end: comment.selectionEnd ?? 0,
      prefix: comment.selectionPrefix,
      suffix: comment.selectionSuffix,
    } : undefined,
  }
}

// Comments keep one exact content target. The O/KR path is a read projection
// from the already loaded OKR tree, so the hierarchy keeps a single source of
// truth and opening the drawer never causes one request per comment.
export function buildCommentOKRContextIndex(objectives: Objective[]): CommentOKRContextIndex {
  const index: CommentOKRContextIndex = {}
  for (const objective of objectives) {
    const objectiveRef = { id: objective.id, title: objective.title }
    index[`objective:${objective.id}`] = { objective: objectiveRef }
    for (const kr of objective.krs) {
      const krRef = { id: kr.id, title: kr.title }
      const context = { objective: objectiveRef, kr: krRef }
      index[`kr:${kr.id}`] = context
      for (const metric of kr.metrics) index[`metric:${metric.id}`] = context
      for (const point of kr.points) {
        index[`point:${point.id}`] = context
        for (const entry of [...point.entries, ...(point.previousEntries ?? [])]) {
          index[`entry:${entry.id}`] = context
        }
      }
    }
  }
  return index
}

export function commentOKRContext(comment: PageComment, index: CommentOKRContextIndex): CommentOKRContext | undefined {
  const key = commentTargetKey(comment)
  const current = index[key]
  if (current) return current

  // Direct O/KR comments retain their own title snapshot after a definition is
  // deleted. Descendant comments cannot recover a missing ancestor without
  // inventing history, so they deliberately have no fallback hierarchy.
  const title = comment.targetTitle?.trim()
  if (!title || !comment.targetId) return undefined
  if (comment.targetType === 'objective') return { objective: { id: comment.targetId, title } }
  if (comment.targetType === 'kr') return { kr: { id: comment.targetId, title } }
  return undefined
}
