import { useEffect, useState } from 'react'
import { APIError, confirmMeegoProgress, getMeegoBatchPreview } from '../api'
import { useBoard } from '../board'
import type { Kr, MeegoBatchPreview as MeegoBatchPreviewData, MeegoBatchPreviewItem, Status } from '../types'

type PreviewState =
  | { kind: 'loading'; requestKey: string }
  | { kind: 'ready'; requestKey: string; data: MeegoBatchPreviewData }
  | { kind: 'error'; requestKey: string; message: string }

function itemTone(item: MeegoBatchPreviewItem) {
  if (!item.sync) return { label: '未同步', className: 'bg-slate-100 text-slate-500' }
  if (item.sync.status === 'error' && !item.preview) return { label: '同步失败', className: 'bg-red-50 text-red-600' }
  if (item.risk) return { label: '风险', className: 'bg-red-50 text-red-600' }
  if (item.sync.status === 'error') return { label: '更新失败', className: 'bg-amber-50 text-amber-600' }
  if (item.preview?.needsReview) return { label: '有差异', className: 'bg-amber-50 text-amber-600' }
  return { label: '一致', className: 'bg-emerald-50 text-emerald-600' }
}

function syncLabel(item: MeegoBatchPreviewItem) {
  if (!item.sync) return { text: '尚未后台同步', className: 'text-slate-400' }
  const value = item.sync.lastSuccessAt || item.sync.lastAttemptAt
  const timestamp = value ? new Date(value).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : ''
  if (item.sync.status === 'error') return { text: `后台同步失败${timestamp ? ` · 上次成功 ${timestamp}` : ''}`, className: 'text-red-500' }
  if (item.sync.status === 'stale') return { text: `后台数据来自 ${item.sync.week || '其他周次'}${timestamp ? ` · ${timestamp}` : ''}`, className: 'text-amber-500' }
  return { text: `后台已同步${timestamp ? ` · ${timestamp}` : ''}`, className: 'text-emerald-600' }
}

function draftStatus(remoteStatus: string): Status {
  const value = remoteStatus.trim().toLowerCase()
  if (value === 'done' || value === 'completed' || value.includes('完成')) return 'done'
  if (value === 'blocked' || value.includes('阻塞')) return 'blocked'
  if (value === 'delayed' || value.includes('延期')) return 'delayed'
  if (value === 'at_risk' || value === 'risk' || value.includes('风险')) return 'at_risk'
  if (value === 'not_started' || value.includes('未开始')) return 'not_started'
  return 'in_progress'
}

const STATUS_OPTIONS: Array<{ value: Status; label: string }> = [
  { value: 'in_progress', label: '进行中' },
  { value: 'done', label: '已完成' },
  { value: 'not_started', label: '未开始' },
  { value: 'at_risk', label: '有风险' },
  { value: 'delayed', label: '延期' },
  { value: 'blocked', label: '阻塞' },
]

export function MeegoBatchPreview({ quarter, week, onClose, onOpenPoint }: { quarter: string; week: string; onClose: () => void; onOpenPoint: (pointId: string) => void }) {
  const { applySavedKr } = useBoard()
  const [reloadKey, setReloadKey] = useState(0)
  const requestKey = `${quarter}:${week}:${reloadKey}`
  const [state, setState] = useState<PreviewState>({ kind: 'loading', requestKey })
  const [draft, setDraft] = useState<{ pointId: string; status: Status; text: string }>()
  const [confirming, setConfirming] = useState(false)
  const [confirmError, setConfirmError] = useState('')
  const [confirmedPoint, setConfirmedPoint] = useState('')

  useEffect(() => {
    let active = true
    void getMeegoBatchPreview(quarter, week)
      .then((data) => { if (active) setState({ kind: 'ready', requestKey, data }) })
      .catch((error: unknown) => { if (active) setState({ kind: 'error', requestKey, message: error instanceof Error ? error.message : 'Meego 差异加载失败。' }) })
    return () => { active = false }
  }, [quarter, requestKey, week])

  const visibleState: PreviewState = state.requestKey === requestKey ? state : { kind: 'loading', requestKey }

  const beginConfirm = (item: MeegoBatchPreviewItem) => {
    if (!item.preview) return
    setConfirmError('')
    setDraft({
      pointId: item.pointId,
      status: draftStatus(item.preview.remote.status),
      text: item.preview.remote.progress || item.preview.remote.title || '',
    })
  }

  const submitConfirm = async (item: MeegoBatchPreviewItem) => {
    if (!item.preview || !draft || !draft.text.trim()) return
    setConfirming(true)
    setConfirmError('')
    try {
      const saved = await confirmMeegoProgress({
        pointId: item.pointId,
        expectedVersion: item.krVersion,
        week,
        meegoWorkItemId: item.preview.workItemId,
        status: draft.status,
        text: draft.text.trim(),
      })
      applySavedKr(saved)
      setDraft(undefined)
      setConfirmedPoint(item.pointId)
      setReloadKey((value) => value + 1)
    } catch (error) {
      if (error instanceof APIError && error.status === 409 && error.data) {
        applySavedKr(error.data as Kr)
        setConfirmError('这条 KR 已被其他人更新，已载入最新版本。请重新核对后确认。')
      } else {
        setConfirmError(error instanceof Error ? error.message : '确认失败，请稍后重试。')
      }
    } finally {
      setConfirming(false)
    }
  }

  return (
    <section className="mb-3 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm" aria-label="Meego 进展差异">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-xs font-semibold text-slate-800">Meego 进展差异</h2>
            <span className="rounded-full bg-blue-50 px-2 py-0.5 text-[10px] font-medium text-blue-600">Agent 快照</span>
          </div>
          <p className="mt-0.5 text-[11px] text-slate-400">页面只读 Agent 通过工具采集的快照；确认草稿只写周报，不会修改 Meego。</p>
        </div>
        <div className="flex items-center gap-1">
          <button type="button" onClick={() => setReloadKey((value) => value + 1)} className="rounded-md px-2 py-1 text-[11px] text-blue-600 hover:bg-blue-50">重新读取</button>
          <button type="button" onClick={onClose} className="rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">收起</button>
        </div>
      </div>

      {visibleState.kind === 'loading' && <div className="px-4 py-6 text-center text-xs text-slate-400">正在读取本地差异快照…</div>}
      {visibleState.kind === 'error' && (
        <div className="flex items-center gap-3 px-4 py-4 text-xs text-red-700">
          <span className="flex-1">{visibleState.message}</span>
          <button type="button" onClick={() => setReloadKey((value) => value + 1)} className="rounded-md border border-red-200 px-2.5 py-1 hover:bg-red-50">重新读取</button>
        </div>
      )}
      {visibleState.kind === 'ready' && (
        <div className="p-4">
          {confirmedPoint && <div className="mb-3 rounded-md bg-emerald-50 px-3 py-2 text-[11px] text-emerald-700">进展已写入本周页面，并保留 Meego 来源。</div>}
          <div className="mb-3 flex flex-wrap gap-x-5 gap-y-1 text-[11px] text-slate-500">
            <span><b className="mr-1 text-base font-semibold text-slate-800">{visibleState.data.summary.linkedCount}</b>项已关联</span>
            <span><b className="mr-1 text-base font-semibold text-red-600">{visibleState.data.summary.riskCount}</b>项风险</span>
            <span><b className="mr-1 text-base font-semibold text-amber-600">{visibleState.data.summary.needsReviewCount}</b>项有差异</span>
            {visibleState.data.summary.errorCount > 0 && <span><b className="mr-1 text-base font-semibold text-slate-600">{visibleState.data.summary.errorCount}</b>项读取失败</span>}
            {visibleState.data.summary.unsyncedCount > 0 && <span><b className="mr-1 text-base font-semibold text-slate-500">{visibleState.data.summary.unsyncedCount}</b>项未同步</span>}
            {visibleState.data.summary.staleCount > 0 && <span><b className="mr-1 text-base font-semibold text-amber-600">{visibleState.data.summary.staleCount}</b>项快照过期</span>}
            <span><b className="mr-1 text-base font-semibold text-blue-600">{visibleState.data.items.filter((item) => item.sync?.status === 'healthy').length}</b>项后台同步新鲜</span>
          </div>
          {visibleState.data.items.length === 0 ? (
            <div className="rounded-md bg-slate-50 px-3 py-5 text-center text-xs text-slate-500">暂无关联项。在具体 KR 点下填写 Meego 工作项 ID 后再刷新。</div>
          ) : (
            <div className="space-y-2">
              {visibleState.data.items.map((item) => {
                const tone = itemTone(item)
                const sync = syncLabel(item)
                return (
                  <article key={item.pointId} className="rounded-md border border-slate-200 bg-slate-50/50 p-3">
                    <div className="flex flex-wrap items-start gap-2">
                      <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${tone.className}`}>{tone.label}</span>
                      <div className="min-w-0 flex-1">
                        <div className="text-xs font-medium text-slate-700">{item.pointTitle}</div>
                        <div className="mt-0.5 truncate text-[10px] text-slate-400">{item.ownerName || '未分配'} · {item.krTitle}</div>
                        <div className={`mt-0.5 text-[10px] ${sync.className}`} title={item.sync?.lastError}>{sync.text}</div>
                      </div>
                      {item.preview?.needsReview && <button type="button" onClick={() => beginConfirm(item)} className="rounded-md border border-blue-200 bg-white px-2 py-1 text-[10px] text-blue-600 hover:bg-blue-50">确认进展</button>}
                      <button type="button" onClick={() => onOpenPoint(item.pointId)} className="rounded-md border border-slate-200 bg-white px-2 py-1 text-[10px] text-slate-500 hover:border-blue-200 hover:text-blue-600">查看事项</button>
                    </div>
                    {item.error ? (
                      <p className="mt-2 text-[11px] text-slate-500">{item.error}</p>
                    ) : item.preview && (
                      <div className="mt-2 grid gap-1 text-[11px] leading-5 text-slate-600 sm:grid-cols-[3rem_1fr]">
                        <span className="text-slate-400">页面</span><span>{item.preview.local.status || '无状态'} · {item.preview.local.progress || '暂无本周进展'}</span>
                        <span className="text-slate-400">Meego</span><span>{item.preview.remote.status || '无状态'} · {item.preview.remote.progress || item.preview.remote.title || '暂无进展'}</span>
                      </div>
                    )}
                    {draft?.pointId === item.pointId && item.preview && (
                      <div className="mt-3 rounded-md border border-blue-100 bg-white p-2.5">
                        <div className="mb-2 flex items-center justify-between gap-2">
                          <span className="text-[11px] font-medium text-slate-700">确认写入本周进展</span>
                          <span className="text-[10px] text-slate-400">只写 Emily，不写 Meego</span>
                        </div>
                        <div className="flex items-start gap-2">
                          <select value={draft.status} onChange={(event) => setDraft({ ...draft, status: event.target.value as Status })} className="rounded border border-slate-200 bg-white px-2 py-1 text-[11px] text-slate-600 outline-none focus:border-blue-400">
                            {STATUS_OPTIONS.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                          </select>
                          <textarea value={draft.text} onChange={(event) => setDraft({ ...draft, text: event.target.value })} rows={3} className="min-w-0 flex-1 resize-y rounded border border-slate-200 px-2 py-1 text-[11px] leading-5 text-slate-700 outline-none focus:border-blue-400" />
                        </div>
                        {confirmError && <p className="mt-1 text-[10px] text-red-600">{confirmError}</p>}
                        <div className="mt-2 flex justify-end gap-1.5">
                          <button type="button" disabled={confirming} onClick={() => { setDraft(undefined); setConfirmError('') }} className="rounded-md px-2.5 py-1 text-[10px] text-slate-500 hover:bg-slate-100">取消</button>
                          <button type="button" disabled={confirming || !draft.text.trim()} onClick={() => void submitConfirm(item)} className="rounded-md bg-blue-600 px-2.5 py-1 text-[10px] text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">{confirming ? '写入中…' : '确认写入'}</button>
                        </div>
                      </div>
                    )}
                  </article>
                )
              })}
            </div>
          )}
        </div>
      )}
    </section>
  )
}
