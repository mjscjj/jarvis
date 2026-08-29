import { createContext, useContext, type ReactNode } from 'react'
import type { CommentTarget, PageComment } from './types'

export interface CommentInteraction {
  selected?: CommentTarget
  comments: PageComment[]
  counts: Record<string, number>
  select: (target: CommentTarget) => void
}

const CommentInteractionContext = createContext<CommentInteraction>({ comments: [], counts: {}, select: () => undefined })

export function CommentInteractionProvider({ value, children }: { value: CommentInteraction; children: ReactNode }) {
  return <CommentInteractionContext.Provider value={value}>{children}</CommentInteractionContext.Provider>
}

export function useCommentInteraction() {
  return useContext(CommentInteractionContext)
}
