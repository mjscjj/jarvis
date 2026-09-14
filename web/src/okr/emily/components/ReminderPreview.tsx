import { useEffect, useState } from 'react'
import { generateReminderBatch, getReminderBatches, getReminderPreview } from '../api'
import type { ReminderBatch, ReminderPreview as ReminderPreviewData } from '../types'

type PreviewState =
  | { kind: 'loading'; requestKey: string }
  | { kind: 'ready'; requestKey: string; data: ReminderPreviewData }
  | { kind: 'error'; requestKey: string; message: string }

export function ReminderPreview({ quarter, week, onClose, readOnly = false }: { quarter: string; week: string; onClose: () => void; readOnly?: boolean }) {
  const [reloadKey, setReloadKey] = useState(0)
  const requestKey = `${quarter}:${week}:${reloadKey}`
  const [state, setState] = useState<PreviewState>({ kind: 'loading', requestKey })
  const [copiedOwner, setCopiedOwner] = useState<string>()
  const [batches, setBatches] = useState<ReminderBatch[]>([])
  const [generating, setGenerating] = useState(false)
  const [batchError, setBatchError] = useState<string>()

  useEffect(() => {
    let active = true
    void Promise.all([getReminderPreview(quarter, week), getReminderBatches(quarter, week)])
      .then(([data, history]) => {
        if (active) {
          setState({ kind: 'ready', requestKey, data })
          setBatches(history.batches)
        }
      })
      .catch((error: unknown) => {
        if (active) setState({ kind: 'error', requestKey, message: error instanceof Error ? error.message : '进展填写检查加载失败。' })
      })
    return () => { active = false }
  }, [quarter, requestKey, week])

	const visibleState: PreviewState = state.requestKey === requestKey ? state : { kind: 'loading', requestKey }
	const remindableCount = visibleState.kind === 'ready' ? visibleState.data.recipients.filter((recipient) => recipient.needsReminder && recipient.canRemind).length : 0
	const unresolvedCount = visibleState.kind === 'ready' ? visibleState.data.recipients.filter((recipient) => recipient.needsReminder && !recipient.canRemind).length : 0

  const copyMessage = async (owner: string, message: string) => {
    await navigator.clipboard.writeText(message)
    setCopiedOwner(owner)
    window.setTimeout(() => setCopiedOwner(undefined), 1400)
  }

  const createBatch = async () => {
    setGenerating(true)
    setBatchError(undefined)
    try {
      const batch = await generateReminderBatch(quarter, week)
      setBatches((current) => [batch, ...current.filter((item) => item.id !== batch.id)])
    } catch (error) {
      setBatchError(error instanceof Error ? error.message : '生成检查快照失败。')
    } finally {
      setGenerating(false)
    }
  }

  return (
		<section className="mb-3 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm" aria-label="进展填写检查">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
        <div>
          <div className="flex items-center gap-2">
					<h2 className="text-xs font-semibold text-slate-800">进展填写检查</h2>
            <span className="rounded-full bg-blue-50 px-2 py-0.5 text-[10px] font-medium text-blue-600">仅预览 · 不会发送</span>
          </div>
          <p className="mt-0.5 text-[11px] text-slate-400">这里只检查 {week} 的 KR 进展完整性，不代表自动催填的四类 Review 判断。</p>
        </div>
        <div className="flex items-center gap-2">
			{!readOnly && <button type="button" disabled={generating} onClick={() => void createBatch()} className="rounded-md border border-blue-200 bg-blue-50 px-2.5 py-1 text-[11px] font-medium text-blue-700 hover:bg-blue-100 disabled:cursor-wait disabled:opacity-50">{generating ? '生成中…' : '保存检查快照'}</button>}
          <button type="button" onClick={onClose} className="rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">收起</button>
        </div>
      </div>

      {visibleState.kind === 'loading' && <div className="px-4 py-6 text-center text-xs text-slate-400">正在核对填写情况…</div>}
      {visibleState.kind === 'error' && (
        <div className="flex items-center gap-3 px-4 py-4 text-xs text-red-700">
          <span className="flex-1">{visibleState.message}</span>
          <button type="button" onClick={() => setReloadKey((value) => value + 1)} className="rounded-md border border-red-200 px-2.5 py-1 hover:bg-red-50">重试</button>
        </div>
      )}
      {visibleState.kind === 'ready' && (
        <div className="p-4">
          {batchError && <div className="mb-3 rounded-md bg-red-50 px-3 py-2 text-[11px] text-red-700">{batchError}</div>}
          <div className="mb-3 flex flex-wrap gap-x-5 gap-y-1 text-[11px] text-slate-500">
			<span><b className="mr-1 text-base font-semibold text-slate-800">{remindableCount}</b>人可提醒</span>
			{unresolvedCount > 0 && <span><b className="mr-1 text-base font-semibold text-amber-600">{unresolvedCount}</b>项需补负责人</span>}
            <span><b className="mr-1 text-base font-semibold text-slate-800">{visibleState.data.summary.missingCount}</b>条 KR 待填写</span>
            <span><b className="mr-1 text-base font-semibold text-emerald-600">{visibleState.data.summary.filledCount}</b>条已完成</span>
          </div>
          {visibleState.data.summary.missingCount === 0 ? (
            <div className="rounded-md bg-emerald-50 px-3 py-4 text-center text-xs text-emerald-700">本周所有 KR 都已填写，无需催办。</div>
          ) : (
            <div className="grid gap-2 lg:grid-cols-2">
              {visibleState.data.recipients.filter((recipient) => recipient.needsReminder).map((recipient) => (
                <article key={recipient.ownerEmail || recipient.ownerName} className="rounded-md border border-slate-200 bg-slate-50/60 p-3">
                  <div className="mb-2 flex items-center justify-between gap-2">
                    <div className="text-xs font-medium text-slate-700">
                      {recipient.ownerName}
                      <span className="ml-1.5 font-normal text-amber-600">缺 {recipient.missingCount}/{recipient.dueCount}</span>
                    </div>
                    <button
                      type="button"
                      disabled={!recipient.canRemind}
                      onClick={() => void copyMessage(recipient.ownerName, recipient.message)}
                      className="rounded-md border border-slate-200 bg-white px-2 py-1 text-[10px] text-slate-500 hover:border-blue-200 hover:text-blue-600 disabled:cursor-not-allowed disabled:opacity-40"
                    >
                      {copiedOwner === recipient.ownerName ? '已复制' : '复制消息'}
                    </button>
                  </div>
                  <p className="whitespace-pre-wrap text-[11px] leading-5 text-slate-600">{recipient.message}</p>
                </article>
              ))}
            </div>
          )}
          <div className="mt-4 border-t border-slate-100 pt-3">
            <div className="mb-2 flex items-center justify-between">
              <h3 className="text-[11px] font-medium text-slate-600">检查快照</h3>
              <span className="text-[10px] text-slate-400">仅保存预览，不会发送</span>
            </div>
            {batches.length === 0 ? (
				<p className="rounded-md bg-slate-50 px-3 py-3 text-center text-[11px] text-slate-400">尚无检查快照，手动保存后会出现在这里。</p>
            ) : (
              <div className="space-y-1.5">
                {batches.slice(0, 6).map((batch) => (
                  <div key={batch.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-md border border-slate-100 px-3 py-2 text-[10px] text-slate-500">
                    <span className={`h-1.5 w-1.5 rounded-full ${batch.status === 'succeeded' ? 'bg-emerald-500' : batch.status === 'failed' ? 'bg-red-500' : 'bg-amber-400'}`} />
					<span className="font-medium text-slate-600">{batch.trigger === 'scheduler' ? '定时生成' : '手动生成'}</span>
                    <span>{new Date(batch.startedAt).toLocaleString('zh-CN', { hour12: false })}</span>
                    <span>{batch.recipientCount} 人 · 缺 {batch.missingCount} 条</span>
                    {batch.lastError && <span className="basis-full pl-4 text-red-600">{batch.lastError}</span>}
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
      )}
    </section>
  )
}
