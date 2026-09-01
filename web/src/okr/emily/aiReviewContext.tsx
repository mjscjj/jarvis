import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { createTask, executeTask, getTask } from '../../api'
import MarkdownReport from '../../components/MarkdownReport'
import type { Task, TaskStatus } from '../../types'
import { useBoard } from './board'
import { previewReviewContent, previewReviewKey, previewReviewTaskInput, type PreviewReviewTarget } from './aiReview'

interface PreviewReviewState {
  taskId?: number
  status?: TaskStatus
  content?: string
  error?: string
}

interface PreviewReviewApi {
  reviews: Record<string, PreviewReviewState>
  ready: boolean
  run: (target: PreviewReviewTarget) => Promise<void>
}

const PreviewReviewContext = createContext<PreviewReviewApi | null>(null)
const ACTIVE_STATUSES = new Set<TaskStatus>(['pending', 'executing'])

function stateFromTask(task: Task): PreviewReviewState {
	const content = previewReviewContent(task)
	const finishedWithoutContent = (task.status === 'done' || task.status === 'observing') && !content
  return {
    taskId: task.id,
    status: task.status,
		content,
		error: task.status === 'failed'
			? content || 'AI 评审执行失败。'
			: finishedWithoutContent ? 'AI 评审已经结束，但没有返回评审内容。' : undefined,
  }
}

export function PreviewReviewProvider({ children }: { children: ReactNode }) {
  const { quarter, week, templateKey, syncState } = useBoard()
  const [reviews, setReviews] = useState<Record<string, PreviewReviewState>>({})
  const ready = templateKey === 'okr_weekly_preview_v1' && (syncState.kind === 'ready' || syncState.kind === 'saved')

  useEffect(() => setReviews({}), [quarter, week])

  useEffect(() => {
    const active = Object.entries(reviews).filter(([, review]) => review.taskId && review.status && ACTIVE_STATUSES.has(review.status))
    if (active.length === 0) return
    let cancelled = false
    const refresh = async () => {
      const settled = await Promise.allSettled(active.map(async ([key, review]) => [key, await getTask(review.taskId as number)] as const))
      if (cancelled) return
      setReviews((current) => {
        const next = { ...current }
        for (const result of settled) {
          if (result.status === 'fulfilled') {
            const [key, task] = result.value
            next[key] = stateFromTask(task)
          }
        }
        return next
      })
    }
    const timer = window.setInterval(() => void refresh(), 2500)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [reviews])

  const run = useCallback(async (target: PreviewReviewTarget) => {
    if (templateKey !== 'okr_weekly_preview_v1') throw new Error('AI 评分只适用于 OKR Preview 周次。')
    if (syncState.kind !== 'ready' && syncState.kind !== 'saved') throw new Error('请等待当前内容保存完成后再评审。')
    const key = previewReviewKey(target)
    setReviews((current) => ({ ...current, [key]: { status: 'pending' } }))
    try {
      const created = await createTask(previewReviewTaskInput(quarter, week, target))
      setReviews((current) => ({ ...current, [key]: { taskId: created.id, status: created.status } }))
      await executeTask(created.id)
      const task = await getTask(created.id)
      setReviews((current) => ({ ...current, [key]: stateFromTask(task) }))
    } catch (cause) {
			setReviews((current) => ({ ...current, [key]: { ...current[key], status: 'failed', error: cause instanceof Error ? cause.message : 'AI 评审启动失败。' } }))
      throw cause
    }
  }, [quarter, syncState.kind, templateKey, week])

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
  if (review.status === 'pending' || review.status === 'executing') return 'Emily 正在评审…'
  if (review.status === 'needs_human' || review.status === 'awaiting_approval') return '需要处理'
  if (review.status === 'waiting') return '等待中'
  return 'AI 评审结果'
}

export function PreviewReviewButton({ target, label, className = '' }: { target: PreviewReviewTarget; label: string; className?: string }) {
  const { reviews, ready, run } = usePreviewReview()
  const review = reviews[previewReviewKey(target)]
  const running = review?.status === 'pending' || review?.status === 'executing'
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
  const running = review.status === 'pending' || review.status === 'executing'

  return <details open className={`group rounded-lg border border-sky-200 bg-sky-50/45 px-2.5 py-2 [&>summary]:list-none ${className}`}>
    <summary className="flex cursor-pointer flex-wrap items-center gap-1.5 text-[10px] leading-4">
      <span aria-hidden className="text-xs leading-none text-sky-400 transition-transform group-open:rotate-90">›</span>
      <strong className={review.error ? 'text-red-700' : running ? 'text-sky-700' : 'text-slate-700'}>{reviewStatusText(review)}</strong>
      {review.taskId && <a href={`#/work/task/${review.taskId}`} className="text-slate-400 hover:text-sky-700">Task #{review.taskId}</a>}
      <span className="ml-auto text-[10px] text-slate-400">只读建议 · 不修改人工评分</span>
    </summary>
    <div className="mt-1 border-t border-sky-100 pt-1.5">
      {review.error && <div className="text-[11px] leading-4 text-red-700">{review.error}</div>}
      {!review.error && running && <div className="text-[11px] leading-4 text-sky-600">正在读取当前周的最新 OKR 和进展，完成后会自动显示结果。</div>}
      {!review.error && !running && review.content && <MarkdownReport className="daily-digest-markdown okr-preview-review-markdown" content={review.content} />}
    </div>
  </details>
}
