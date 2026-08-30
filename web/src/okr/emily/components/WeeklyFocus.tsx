import { useMemo } from 'react'
import { useBoard } from '../board'
import { splitOwnerNames } from '../people'
import type { PageComment, Point } from '../types'

function krNeedsUpdate(points: Point[]) {
  return points.length === 0 || points.some((point) => !point.entries.some((entry) => entry.text.trim().length > 0))
}

function targetLabel(comment: PageComment) {
  if (comment.targetTitle?.trim()) return comment.targetTitle
  return ({ page: '整页评论', kr: 'KR 评论', metric: '核心数据评论', point: '具体 KR 评论', entry: '进展评论' } as const)[comment.targetType]
}

/** Incomplete progress and unresolved To do comments stay backed by their original records. */
export function WeeklyFocus({ comments, onOpenComment }: { comments: PageComment[]; onOpenComment: (comment: PageComment) => void }) {
  const { objectives } = useBoard()
  const incomplete = useMemo(() => {
    const missing = new Map<string, number>()
    for (const objective of objectives) {
      for (const kr of objective.krs) {
        if (!krNeedsUpdate(kr.points)) continue
        const owners = splitOwnerNames(kr.ownerName)
        for (const owner of owners.length > 0 ? owners : ['未分配']) {
          missing.set(owner, (missing.get(owner) ?? 0) + 1)
        }
      }
    }
    return [...missing]
      .map(([name, count]) => ({ name, count }))
      .sort((left, right) => right.count - left.count || left.name.localeCompare(right.name, 'zh-Hans-CN'))
  }, [objectives])
  const todos = useMemo(() => comments.filter((comment) => comment.todo && !comment.resolved), [comments])

  return (
    <section aria-label="本周重点关注" className="mb-4 rounded-xl border border-blue-100 bg-gradient-to-br from-white to-blue-50/35 p-3.5 shadow-[0_3px_12px_rgba(31,35,40,0.035)]">
      <header className="mb-2.5 border-b border-slate-100 pb-2">
        <div>
          <span className="block text-[9px] font-bold tracking-[0.12em] text-blue-600">WEEKLY FOCUS</span>
          <h2 className="text-[16px] font-semibold text-slate-900">本周重点关注</h2>
        </div>
      </header>
      <div className="space-y-2.5">
        <div className="min-w-0 rounded-lg border border-slate-200 bg-white/85 p-2.5">
          <h3 className="mb-2 flex items-center justify-between text-[12px] font-semibold text-slate-700"><span>未完成 OKR</span><b className="text-[10px] font-medium text-slate-400">{incomplete.length} 人</b></h3>
          {incomplete.length > 0 ? (
            <div className="flex flex-wrap gap-1.5">{incomplete.map((item) => <span key={item.name} title={`${item.count} 条 KR 尚未完整填写本周进展`} className="inline-flex items-center gap-1 rounded-full border border-slate-200 bg-slate-50 py-0.5 pr-2 pl-1 text-[10px] text-slate-600"><span className="flex size-4 items-center justify-center rounded-full bg-slate-200 text-[8px] font-semibold">{item.name.slice(0, 1)}</span>{item.name}{item.count > 1 && <b className="text-blue-600">{item.count}</b>}</span>)}</div>
          ) : <div className="text-[11px] text-slate-400">本周具体 KR 均已填写</div>}
        </div>
        <div className="min-w-0 rounded-lg border border-slate-200 bg-white/85 p-2.5">
          <h3 className="mb-2 flex items-center justify-between text-[12px] font-semibold text-slate-700"><span>待跟进事项</span><b className="text-[10px] font-medium text-slate-400">{todos.length} 项</b></h3>
          {todos.length > 0 ? (
            <div className="grid max-h-28 gap-1 overflow-y-auto">
              {todos.map((comment) => (
                <button key={comment.id} type="button" title="打开对应评论" onClick={() => onOpenComment(comment)} className="grid w-full grid-cols-[minmax(0,1fr)_auto] items-baseline gap-2 rounded-md bg-amber-50 px-2 py-1.5 text-left hover:bg-amber-100">
                  <span className="truncate text-[11px] font-medium text-slate-700">{comment.content}</span>
                  <span className="max-w-56 truncate text-[10px] text-slate-400">{targetLabel(comment)}</span>
                </button>
              ))}
            </div>
          ) : <div className="text-[11px] text-slate-400">会议评论中还没有标记 To do</div>}
        </div>
      </div>
    </section>
  )
}
