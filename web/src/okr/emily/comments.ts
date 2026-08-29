import type { CommentTarget, PageComment } from './types'

export function commentTargetKey(target: Pick<CommentTarget, 'type' | 'id'> | Pick<PageComment, 'targetType' | 'targetId'>) {
  if ('type' in target) return `${target.type}:${target.id}`
  return `${target.targetType}:${target.targetId ?? ''}`
}

export function commentMessageCount(comments: PageComment[]) {
  return comments.reduce((sum, comment) => sum + 1 + comment.replies.length, 0)
}

export function commentCountsByTarget(comments: PageComment[]) {
  const counts: Record<string, number> = {}
  for (const comment of comments) {
    const key = commentTargetKey(comment)
    counts[key] = (counts[key] ?? 0) + 1 + comment.replies.length
  }
  return counts
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
    selection: comment.selectedText ? {
      text: comment.selectedText,
      start: comment.selectionStart ?? 0,
      end: comment.selectionEnd ?? 0,
      prefix: comment.selectionPrefix,
      suffix: comment.selectionSuffix,
    } : undefined,
  }
}
