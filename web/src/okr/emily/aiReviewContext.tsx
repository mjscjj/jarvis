import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import MarkdownReport from '../../components/MarkdownReport'
import { runPreviewReview } from './api'
import { useBoard } from './board'
import { previewReviewKey, previewReviewRequest, type OKRReviewSource, type PreviewReviewTarget } from './aiReview'

interface PreviewReviewState {
  running: boolean
  content?: string
  error?: string
}

interface PreviewReviewApi {
  reviews: Record<string, PreviewReviewState>
  ready: boolean
  run: (target: PreviewReviewTarget) => Promise<void>
}

const PreviewReviewContext = createContext<PreviewReviewApi | null>(null)

export function PreviewReviewProvider({ children, reviewType, planId = '' }: { children: ReactNode; reviewType: OKRReviewSource['reviewType']; planId?: string }) {
  const { quarter, week, templateKey, syncState, hasPendingChanges } = useBoard()
  const [reviews, setReviews] = useState<Record<string, PreviewReviewState>>({})
  const source: OKRReviewSource = reviewType === 'plan'
    ? { reviewType, quarter, planId }
    : { reviewType, quarter, week }
  const ready = (reviewType === 'plan' ? Boolean(planId) : templateKey === 'okr_weekly_preview_v1')
    && !hasPendingChanges
    && (syncState.kind === 'ready' || syncState.kind === 'saved')

  useEffect(() => setReviews({}), [planId, quarter, reviewType, week])

  const run = useCallback(async (target: PreviewReviewTarget) => {
    if (reviewType === 'plan' && !planId) throw new Error('请先选择一个 OKR Plan。')
    if (reviewType === 'progress' && templateKey !== 'okr_weekly_preview_v1') throw new Error('AI 评审只适用于 OKR Review 周次。')
    if (hasPendingChanges || (syncState.kind !== 'ready' && syncState.kind !== 'saved')) throw new Error('请等待当前内容保存完成后再评审。')
    const key = previewReviewKey(target)
    setReviews((current) => ({ ...current, [key]: { running: true } }))
    try {
      const content = await runPreviewReview(previewReviewRequest(source, target))
      setReviews((current) => ({ ...current, [key]: { running: false, content } }))
    } catch (cause) {
      setReviews((current) => ({
        ...current,
        [key]: { running: false, error: cause instanceof Error ? cause.message : 'AI 评审失败。' },
      }))
      throw cause
    }
  }, [hasPendingChanges, planId, quarter, reviewType, syncState.kind, templateKey, week])

  const value = useMemo(() => ({ reviews, ready, run }), [ready, reviews, run])
  return <PreviewReviewContext.Provider value={value}>{children}</PreviewReviewContext.Provider>
}

export function usePreviewReview() {
  const context = useContext(PreviewReviewContext)
  if (!context) throw new Error('usePreviewReview 必须在 PreviewReviewProvider 内使用')
  return context
}

function reviewStatusText(review: PreviewReviewState): string {
  if (review.error) return '评审失败'
  if (review.running) return 'Emily 正在评审…'
  return 'AI 评审结果'
}

export function PreviewReviewButton({ target, label, className = '' }: { target: PreviewReviewTarget; label: string; className?: string }) {
  const { reviews, ready, run } = usePreviewReview()
  const review = reviews[previewReviewKey(target)]
  const running = review?.running === true
  const [clickError, setClickError] = useState('')

  const start = async () => {
    setClickError('')
    try {
      await run(target)
    } catch (cause) {
      setClickError(cause instanceof Error ? cause.message : 'AI 评审启动失败。')
    }
  }

  return <span className="inline-flex flex-col items-end">
    <button type="button" disabled={!ready || running} onClick={() => void start()} className={`inline-flex h-6 items-center rounded-md border border-sky-200 bg-sky-50 px-1.5 text-[10px] font-semibold leading-none text-sky-700 hover:bg-sky-100 disabled:cursor-not-allowed disabled:opacity-45 ${className}`}>
      {running ? '评审中…' : review?.content ? `重新${label}` : label}
    </button>
    {clickError && <span className="mt-1 max-w-64 text-right text-[10px] text-red-600">{clickError}</span>}
  </span>
}

export function PreviewReviewPanel({ target, className = '' }: { target: PreviewReviewTarget; className?: string }) {
  const { reviews } = usePreviewReview()
  const review = reviews[previewReviewKey(target)]
  if (!review) return null

  return <details open className={`group rounded-lg border border-sky-200 bg-sky-50/45 px-2.5 py-2 [&>summary]:list-none ${className}`}>
    <summary className="flex cursor-pointer flex-wrap items-center gap-1.5 text-[10px] leading-4">
      <span aria-hidden className="text-xs leading-none text-sky-400 transition-transform group-open:rotate-90">›</span>
      <strong className={review.error ? 'text-red-700' : review.running ? 'text-sky-700' : 'text-slate-700'}>{reviewStatusText(review)}</strong>
      <span className="ml-auto text-[10px] text-slate-400">只读建议 · 不修改当前内容</span>
    </summary>
    <div className="mt-1 border-t border-sky-100 pt-1.5">
      {review.error && <div className="text-[11px] leading-4 text-red-700">{review.error}</div>}
      {!review.error && review.running && <div className="text-[11px] leading-4 text-sky-600">正在读取当前内容并评审，完成后会自动显示结果。</div>}
      {!review.error && !review.running && review.content && <MarkdownReport className="daily-digest-markdown okr-preview-review-markdown" content={review.content} />}
    </div>
  </details>
}
