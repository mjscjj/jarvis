import { useEffect, useState } from 'react'
import { getPMODigest } from '../api'
import type { PMODigest as PMODigestData, PMODigestKind } from '../types'

type DigestState =
  | { kind: 'loading'; requestKey: string }
  | { kind: 'ready'; requestKey: string; data: PMODigestData }
  | { kind: 'error'; requestKey: string; message: string }

const sectionTone: Record<PMODigestKind, string> = {
  risk: 'bg-red-50 text-red-600',
  sync_issue: 'bg-amber-50 text-amber-600',
  missing: 'bg-slate-100 text-slate-600',
  changed: 'bg-blue-50 text-blue-600',
}

const sourceLabel = { page: '页面', page_history: '周历史', meego: 'Meego 快照' } as const

export function PMODigest({ quarter, week, onClose, onOpenPoint }: { quarter: string; week: string; onClose: () => void; onOpenPoint: (pointId: string) => void }) {
  const [reloadKey, setReloadKey] = useState(0)
  const requestKey = `${quarter}:${week}:${reloadKey}`
  const [state, setState] = useState<DigestState>({ kind: 'loading', requestKey })

  useEffect(() => {
    let active = true
    void getPMODigest(quarter, week)
      .then((data) => { if (active) setState({ kind: 'ready', requestKey, data }) })
      .catch((error: unknown) => { if (active) setState({ kind: 'error', requestKey, message: error instanceof Error ? error.message : '风险简报加载失败。' }) })
    return () => { active = false }
  }, [quarter, requestKey, week])

  const visibleState: DigestState = state.requestKey === requestKey ? state : { kind: 'loading', requestKey }

  return (
    <section className="mb-3 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm" aria-label="PMO 风险行动简报">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-xs font-semibold text-slate-800">PMO 风险行动简报</h2>
            <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium text-slate-500">只读 · 不外发</span>
          </div>
          <p className="mt-0.5 text-[11px] text-slate-400">把页面、周历史与本地 Meego 快照收拢成可回查的行动清单。</p>
        </div>
        <button type="button" onClick={onClose} className="rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">收起</button>
      </div>

      {visibleState.kind === 'loading' && <div className="px-4 py-6 text-center text-xs text-slate-400">正在整理本周风险与变化…</div>}
      {visibleState.kind === 'error' && (
        <div className="flex items-center gap-3 px-4 py-4 text-xs text-red-700">
          <span className="flex-1">{visibleState.message}</span>
          <button type="button" onClick={() => setReloadKey((value) => value + 1)} className="rounded-md border border-red-200 px-2.5 py-1 hover:bg-red-50">重试</button>
        </div>
      )}
      {visibleState.kind === 'ready' && (
        <div className="p-4">
          <div className="mb-3 flex flex-wrap gap-x-5 gap-y-1 text-[11px] text-slate-500">
            <span><b className="mr-1 text-base font-semibold text-red-600">{visibleState.data.summary.riskCount}</b>项延期或阻塞</span>
            <span><b className="mr-1 text-base font-semibold text-amber-600">{visibleState.data.summary.syncIssueCount}</b>项同步异常</span>
            <span><b className="mr-1 text-base font-semibold text-slate-700">{visibleState.data.summary.missingCount}</b>项未填写</span>
            <span><b className="mr-1 text-base font-semibold text-blue-600">{visibleState.data.summary.changedCount}</b>项较上周变化</span>
          </div>
          <div className="grid gap-3 xl:grid-cols-2">
            {visibleState.data.sections.map((section) => (
              <div key={section.kind} className="rounded-md border border-slate-200 bg-slate-50/40 p-3">
                <div className="mb-2 flex items-center justify-between gap-2">
                  <h3 className="text-[11px] font-semibold text-slate-700">{section.title}</h3>
                  <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${sectionTone[section.kind]}`}>{section.items.length}</span>
                </div>
                {section.items.length === 0 ? (
                  <div className="py-3 text-center text-[11px] text-slate-400">暂无</div>
                ) : (
                  <div className="space-y-1.5">
                    {section.items.map((item) => (
                      <article key={`${section.kind}:${item.pointId}:${item.source}`} className="rounded-md bg-white px-3 py-2 ring-1 ring-slate-100">
                        <div className="flex items-start gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="text-[11px] font-medium text-slate-700">{item.pointTitle}</div>
                            <div className="mt-0.5 text-[10px] text-slate-400">{item.ownerName} · {item.krTitle}</div>
                          </div>
                          <button type="button" onClick={() => onOpenPoint(item.pointId)} className="shrink-0 rounded-md border border-slate-200 px-2 py-1 text-[10px] text-slate-500 hover:border-blue-200 hover:text-blue-600">查看事项</button>
                        </div>
                        <p className="mt-1.5 whitespace-pre-wrap text-[11px] leading-5 text-slate-600">{item.detail}</p>
                        <div className="mt-1 text-[10px] text-slate-400">来源：{sourceLabel[item.source]}</div>
                      </article>
                    ))}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  )
}
