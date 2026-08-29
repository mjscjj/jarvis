import { useEffect, useState } from 'react'
import { getQuarterlyOKRDraft } from '../api'
import type { QuarterlyOKRDraft as QuarterlyOKRDraftData } from '../types'

type State =
  | { kind: 'loading'; key: string }
  | { kind: 'ready'; key: string; data: QuarterlyOKRDraftData }
  | { kind: 'error'; key: string; message: string }

const statusLabel: Record<string, string> = { not_started: '未开始', in_progress: '进行中', done: '已完成', at_risk: '有风险', delayed: 'Delay', blocked: '阻塞' }
const statusTone: Record<string, string> = { not_started: 'bg-slate-100 text-slate-500', in_progress: 'bg-blue-50 text-blue-600', done: 'bg-emerald-50 text-emerald-600', at_risk: 'bg-amber-50 text-amber-700', delayed: 'bg-orange-50 text-orange-700', blocked: 'bg-red-50 text-red-700' }

export function QuarterlyOKRDraft({ quarter, week, onClose, onOpenPoint }: { quarter: string; week: string; onClose: () => void; onOpenPoint: (pointId: string) => void }) {
  const [density, setDensity] = useState<'team' | 'management'>('management')
  const key = `${quarter}:${week}`
  const [state, setState] = useState<State>({ kind: 'loading', key })
  const [reload, setReload] = useState(0)

  useEffect(() => {
    let active = true
    void getQuarterlyOKRDraft(quarter, week)
      .then((data) => { if (active) setState({ kind: 'ready', key, data }) })
      .catch((error: unknown) => { if (active) setState({ kind: 'error', key, message: error instanceof Error ? error.message : '季度材料加载失败。' }) })
    return () => { active = false }
  }, [key, quarter, reload, week])

  const visible = state.key === key ? state : { kind: 'loading' as const, key }
  return (
    <section className="mb-3 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm" aria-label="季度 OKR 材料草稿">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
        <div><div className="flex items-center gap-2"><h2 className="text-xs font-semibold text-slate-800">季度 OKR 材料草稿</h2><span className="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-medium text-amber-600">未发布 · 不外发</span></div><p className="mt-0.5 text-[11px] text-slate-400">同一份 KR 数据，按受众切换信息密度；风险始终优先。</p></div>
        <div className="flex items-center gap-2"><div className="inline-flex rounded-md bg-slate-100 p-0.5 text-[10px]"><button type="button" onClick={() => setDensity('management')} className={`rounded px-2.5 py-1 ${density === 'management' ? 'bg-white text-slate-700 shadow-sm' : 'text-slate-400'}`}>管理层版</button><button type="button" onClick={() => setDensity('team')} className={`rounded px-2.5 py-1 ${density === 'team' ? 'bg-white text-slate-700 shadow-sm' : 'text-slate-400'}`}>团队版</button></div><button type="button" onClick={onClose} className="rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100">收起</button></div>
      </div>
      {visible.kind === 'loading' && <div className="px-4 py-7 text-center text-xs text-slate-400">正在整理季度 OKR…</div>}
      {visible.kind === 'error' && <div className="flex items-center gap-2 px-4 py-5 text-xs text-red-600"><span className="flex-1">{visible.message}</span><button type="button" onClick={() => setReload((value) => value + 1)} className="rounded border border-red-200 px-2 py-1">重试</button></div>}
      {visible.kind === 'ready' && <div className="p-4">
        <div className="mb-3 flex flex-wrap gap-x-5 gap-y-1 text-[11px] text-slate-500"><span><b className="mr-1 text-base text-slate-800">{visible.data.summary.objectiveCount}</b>个 O</span><span><b className="mr-1 text-base text-slate-800">{visible.data.summary.krCount}</b>条 KR</span><span><b className="mr-1 text-base text-red-600">{visible.data.summary.riskCount}</b>条风险</span><span><b className="mr-1 text-base text-amber-600">{visible.data.summary.missingCount}</b>条有缺填</span></div>
        <div className="space-y-3">{visible.data.objectives.map((objective) => <section key={objective.id} className="rounded-md border border-slate-200"><h3 className="border-b border-slate-100 bg-slate-50/70 px-3 py-2 text-[11px] font-semibold text-slate-700">{objective.title}</h3><div className={density === 'management' ? 'grid gap-2 p-3 xl:grid-cols-2' : 'space-y-2 p-3'}>{objective.krs.map((kr) => <article key={kr.id} className={`rounded-md border p-3 ${kr.risk ? 'border-red-100 bg-red-50/30' : 'border-slate-100 bg-white'}`}>
          <div className="flex items-start gap-2"><div className="min-w-0 flex-1"><div className="text-[11px] font-medium text-slate-700">{kr.title}</div><div className="mt-0.5 text-[10px] text-slate-400">{kr.ownerName} · {kr.priority.toUpperCase()}</div></div><span className={`rounded-full px-2 py-0.5 text-[10px] ${statusTone[kr.status] ?? statusTone.not_started}`}>{statusLabel[kr.status] ?? kr.status}</span></div>
          {density === 'management' ? <p className="mt-2 text-[11px] leading-5 text-slate-600">{kr.keyProgress}</p> : <><div className="mt-2 rounded bg-slate-50 px-2.5 py-2 text-[10px] leading-4 text-slate-500"><b className="text-slate-600">核心数据：</b>{kr.metrics.length ? kr.metrics.join('；') : '未填写'}</div><div className="mt-2 space-y-1.5">{kr.points.map((point) => <div key={point.id} className="flex items-start gap-2 rounded bg-slate-50/60 px-2.5 py-2"><div className="min-w-0 flex-1"><button type="button" onClick={() => onOpenPoint(point.id)} className="text-left text-[10px] font-medium text-blue-600 hover:underline">{point.title}</button><p className="mt-0.5 whitespace-pre-wrap text-[10px] leading-4 text-slate-500">{point.detail}</p><span className="text-[9px] text-slate-400">来源：{point.source}</span></div><span className={`rounded-full px-1.5 py-0.5 text-[9px] ${statusTone[point.status] ?? statusTone.not_started}`}>{statusLabel[point.status] ?? point.status}</span></div>)}</div></>}
        </article>)}</div></section>)}</div>
      </div>}
    </section>
  )
}
