import { useState } from 'react'
import { createRoot } from 'react-dom/client'
import { CommentDrawer, type CommentReviewMode } from '../../src/okr/emily/components/CommentDrawer'
import type { Objective } from '../../src/okr/emily/types'
import '../../src/styles.css'
import '../../src/okr/emily/index.css'

const objectives: Objective[] = []
const ignore = () => undefined

function Fixture() {
  const [open, setOpen] = useState(true)
  const [reviewMode, setReviewMode] = useState<CommentReviewMode>()
  return <div id="okr-workspace-root">
    <button onClick={() => setOpen(true)}>打开评论</button>
    <CommentDrawer open={open} reviewEnabled reviewMode={reviewMode}
      quarter="2026-Q3" week="2026-W36" sourceTab="review-fill" objectives={objectives}
      onStartReview={setReviewMode} onShowAll={() => setReviewMode(undefined)}
      onClose={() => { setOpen(false); setReviewMode(undefined) }}
      onFocusCommentChange={ignore} onCountChange={ignore}
      onCountsChange={ignore} onCommentsChange={ignore} />
  </div>
}

createRoot(document.getElementById('root')!).render(<Fixture />)
