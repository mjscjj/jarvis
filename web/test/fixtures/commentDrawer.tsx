import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { CommentDrawer, type CommentReviewMode } from '../../src/okr/emily/components/CommentDrawer'
import type { CommentTarget, Objective } from '../../src/okr/emily/types'
import '../../src/styles.css'
import '../../src/okr/emily/index.css'

const objectives: Objective[] = []
const ignore = () => undefined
const focusedTarget: CommentTarget = { type: 'page', id: '2026-Q3:2026-W36', title: '2026-W36 OKR 页面', commentId: 'old-only' }

function Fixture() {
  const [open, setOpen] = useState(true)
  const [reviewMode, setReviewMode] = useState<CommentReviewMode>()
  const [target, setTarget] = useState<CommentTarget | undefined>(focusedTarget)
  return <div id="okr-workspace-root">
    <button onClick={() => { setTarget(undefined); setOpen(true) }}>打开评论</button>
    <button onClick={() => { setTarget(focusedTarget); setOpen(true) }}>打开具体评论</button>
    <button onClick={() => { setTarget({ ...focusedTarget, commentId: undefined }); setOpen(true) }}>打开评论位置</button>
    <CommentDrawer open={open} reviewEnabled reviewMode={reviewMode}
      quarter="2026-Q3" week="2026-W36" sourceTab="review-fill" objectives={objectives} target={target}
      onStartReview={(mode) => { setTarget(undefined); setReviewMode(mode) }}
      onShowAll={() => { setTarget(undefined); setReviewMode(undefined) }}
      onClose={() => { setOpen(false); setTarget(undefined); setReviewMode(undefined) }}
      onFocusCommentChange={ignore} onCountChange={ignore}
      onCountsChange={ignore} onCommentsChange={ignore} />
  </div>
}

createRoot(document.getElementById('root')!).render(<Fixture />)
