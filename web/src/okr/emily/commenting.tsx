import { createContext, useContext, type MouseEvent, type ReactNode } from 'react'
import { commentTargetKey } from './comments'
import type { CommentTarget, PageComment, TextSelection } from './types'

export interface PendingCommentSelection {
  targetKey: string
  selection: TextSelection
}

export interface CommentInteraction {
  enabled?: boolean
  triggerMode?: 'button' | 'surface'
  selected?: CommentTarget
  focused?: PageComment
  comments: PageComment[]
  counts: Record<string, number>
  pendingSelection?: PendingCommentSelection
  setPendingSelection: (value?: PendingCommentSelection) => void
  select: (target: CommentTarget) => void
}

const CommentInteractionContext = createContext<CommentInteraction>({ comments: [], counts: {}, setPendingSelection: () => undefined, select: () => undefined })

export function CommentInteractionProvider({ value, children }: { value: CommentInteraction; children: ReactNode }) {
  return <CommentInteractionContext.Provider value={value}>{children}</CommentInteractionContext.Provider>
}

export function useCommentInteraction() {
  return useContext(CommentInteractionContext)
}

export function useCommentSurface(target: CommentTarget) {
  const interaction = useCommentInteraction()
  const key = commentTargetKey(target)
  const enabled = Boolean(interaction.enabled && interaction.triggerMode === 'surface')
  const selected = !interaction.focused && interaction.selected ? commentTargetKey(interaction.selected) === key : false
  const focused = interaction.focused ? commentTargetKey(interaction.focused) === key : false
  return {
    enabled,
    selected,
    focused,
    count: interaction.counts[key] ?? 0,
    onClick: (event: MouseEvent<HTMLElement>) => {
      if (!enabled) return
      const source = event.target
      if (source instanceof Element && source.closest('button,input,textarea,select,a,[contenteditable="true"]')) return
      if (window.getSelection()?.toString().trim()) return
      event.stopPropagation()
      interaction.select(target)
    },
  }
}

export function CommentSurfaceHint({ target }: { target: CommentTarget }) {
  const surface = useCommentSurface(target)
  if (!surface.enabled) return null
  return surface.count > 0
    ? <span className="ml-auto shrink-0 rounded-full bg-indigo-50 px-1.5 py-0.5 text-[9px] font-medium text-indigo-600">{surface.count} 条评论</span>
    : <span className="ml-auto shrink-0 text-[9px] font-medium text-indigo-400 opacity-0 transition-opacity group-hover/commentable:opacity-100">点击评论</span>
}

export function commentTargetElementId(target: Pick<CommentTarget, 'type' | 'id'> | Pick<PageComment, 'targetType' | 'targetId'>) {
  return `comment-target-${encodeURIComponent(commentTargetKey(target))}`
}

export function commentSelectionElementId(commentId: string) {
  return `comment-selection-${encodeURIComponent(commentId)}`
}

export function scrollToCommentSource(comment: PageComment) {
  const selection = document.getElementById(commentSelectionElementId(comment.id))
  const target = document.getElementById(commentTargetElementId(comment))
  const element = selection ?? target
  element?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' })
  return selection ? 'selection' : target ? 'target' : 'missing'
}

export function scrollToCommentTarget(target: CommentTarget) {
  const selection = target.commentId
    ? document.getElementById(commentSelectionElementId(target.commentId))
    : null
  const element = selection ?? document.getElementById(commentTargetElementId(target))
  element?.scrollIntoView({ behavior: 'smooth', block: 'center', inline: 'nearest' })
  return selection ? 'selection' : element ? 'target' : 'missing'
}

export function CommentTargetButton({ target, className = '' }: { target: CommentTarget; className?: string }) {
  const interaction = useCommentInteraction()
  if (!interaction.enabled || interaction.triggerMode === 'surface') return null
  const key = commentTargetKey(target)
  const count = interaction.counts[key] ?? 0
  const selected = !interaction.focused && interaction.selected ? commentTargetKey(interaction.selected) === key : false
  const focused = interaction.focused ? commentTargetKey(interaction.focused) === key : false
  return (
    <button
      type="button"
      title="查看或添加评论"
      aria-label={`评论 ${target.title}`}
      onClick={(event) => { event.stopPropagation(); interaction.select(target) }}
      className={`inline-flex h-6 shrink-0 items-center justify-center gap-1 rounded-md border px-1.5 text-[9px] font-medium transition-colors ${selected || focused ? 'border-indigo-300 bg-indigo-50 text-indigo-700' : 'border-slate-200 bg-white text-slate-400 hover:border-indigo-200 hover:text-indigo-600'} ${className}`}
    >
      <span aria-hidden>💬</span>{count > 0 && <span>{count}</span>}
    </button>
  )
}
