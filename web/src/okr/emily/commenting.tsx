import { createContext, useContext, type ReactNode } from 'react'
import type { CommentTarget, PageComment, TextSelection } from './types'

export interface PendingCommentSelection {
  targetKey: string
  selection: TextSelection
}

export interface CommentInteraction {
  selected?: CommentTarget
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
