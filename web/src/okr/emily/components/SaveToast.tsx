import { useEffect, useMemo, useState } from 'react'
import { useBoard } from '../board'
import { buildKrConflictFields, localConflictCopyText } from '../saveNotice'

async function copyText(value: string) {
  if (navigator.clipboard) {
    try {
      await navigator.clipboard.writeText(value)
      return
    } catch {
      // HTTP deployments can expose Clipboard without allowing writes.
    }
  }
  const input = document.createElement('textarea')
  input.value = value
  input.style.position = 'fixed'
  input.style.opacity = '0'
  document.body.appendChild(input)
  input.select()
  document.execCommand('copy')
  input.remove()
}

export function SaveToast({ scopeLabel }: { scopeLabel: string }) {
  const { syncState, hasPendingChanges, retry, resolveConflict } = useBoard()
  const [successVisible, setSuccessVisible] = useState(false)
  const [detailsOpen, setDetailsOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (syncState.kind !== 'saved' || hasPendingChanges) {
      setSuccessVisible(false)
      return
    }
    setSuccessVisible(true)
    const timer = window.setTimeout(() => setSuccessVisible(false), 1800)
    return () => window.clearTimeout(timer)
  }, [hasPendingChanges, syncState])

  useEffect(() => {
    setDetailsOpen(false)
    setCopied(false)
  }, [syncState.kind === 'conflict' ? `${syncState.krId}:${syncState.pointId ?? ''}:${syncState.message}` : syncState.kind])

  const fields = useMemo(() => syncState.kind === 'conflict' ? buildKrConflictFields(syncState.local, syncState.remote) : [], [syncState])
  if (syncState.kind === 'saved' && !successVisible) return null
  if (syncState.kind !== 'saved' && syncState.kind !== 'error' && syncState.kind !== 'conflict') return null

  const conflict = syncState.kind === 'conflict'
  const error = syncState.kind === 'error'
  const tone = conflict ? 'border-amber-300 bg-amber-50 text-amber-950' : error ? 'border-red-300 bg-red-50 text-red-900' : 'border-emerald-300 bg-emerald-50 text-emerald-900'
  const iconTone = conflict ? 'bg-amber-600' : error ? 'bg-red-600' : 'bg-emerald-600'
  const icon = conflict ? '!' : error ? '×' : '✓'
  const title = conflict ? `保存冲突 · ${scopeLabel}` : error ? syncState.title ?? '保存失败' : syncState.message
  const copyLocal = async () => {
    if (!conflict) return
    try {
      await copyText(localConflictCopyText(syncState.location, fields))
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setCopied(false)
    }
  }

  return (
    <div className="pointer-events-none fixed inset-x-0 top-3 z-[90] flex justify-center px-3" role={error || conflict ? 'alert' : 'status'} aria-live={error || conflict ? 'assertive' : 'polite'}>
      <div className={`okr-save-toast pointer-events-auto w-full max-w-[760px] overflow-hidden rounded-lg border shadow-[0_8px_28px_rgba(15,23,42,0.16)] ${tone}`}>
        <div className="flex min-h-9 items-center gap-2 px-3 py-1.5 text-xs">
          <span className={`flex size-4 shrink-0 items-center justify-center rounded-full text-[11px] font-bold text-white ${iconTone}`}>{icon}</span>
          <strong className="shrink-0 font-semibold">{title}</strong>
          {conflict && <span className="min-w-0 flex-1 truncate text-[11px] opacity-80">{syncState.location}</span>}
          {error && <span className="min-w-0 flex-1 truncate text-[11px] opacity-85">{syncState.message}</span>}
          {!conflict && !error && scopeLabel && <span className="min-w-0 flex-1 truncate text-[11px] opacity-75">{scopeLabel}</span>}
          {conflict && <button type="button" onClick={() => setDetailsOpen((value) => !value)} className="shrink-0 rounded-md border border-amber-300 bg-white px-2 py-0.5 text-[11px] hover:bg-amber-100">{detailsOpen ? '收起' : '查看差异'}</button>}
          {error && <button type="button" onClick={retry} className="shrink-0 rounded-md border border-red-300 bg-white px-2 py-0.5 text-[11px] hover:bg-red-100">重试</button>}
        </div>
        {error && syncState.logid && <div className="border-t border-red-200 px-3 py-1 text-[10px] text-red-700">错误编号：{syncState.logid}</div>}
        {conflict && detailsOpen && (
          <div className="max-h-[min(520px,70vh)] overflow-y-auto border-t border-amber-200 bg-white px-3 py-3 text-xs text-slate-700">
            <div className="mb-2 font-medium text-slate-900">冲突位置：{syncState.location}</div>
            {fields.length === 0 ? <div className="rounded-md bg-slate-50 px-2.5 py-2 text-slate-500">内容相同，版本号已变化。</div> : fields.map((field) => (
              <section key={field.label} className="mb-2 overflow-hidden rounded-md border border-slate-200 last:mb-0">
                <div className="bg-slate-50 px-2.5 py-1 font-medium text-slate-700">{field.label}</div>
                <div className="grid sm:grid-cols-2">
                  <div className="border-b border-slate-200 p-2.5 sm:border-b-0 sm:border-r"><div className="mb-1 text-[10px] font-semibold text-indigo-600">我的修改</div><div className="max-h-32 overflow-auto whitespace-pre-wrap break-words leading-5">{field.local}</div></div>
                  <div className="p-2.5"><div className="mb-1 text-[10px] font-semibold text-amber-700">服务器当前内容</div><div className="max-h-32 overflow-auto whitespace-pre-wrap break-words leading-5">{field.remote}</div></div>
                </div>
              </section>
            ))}
            <div className="sticky bottom-0 mt-3 flex justify-end gap-2 border-t border-slate-100 bg-white pt-3">
              <button type="button" onClick={() => void copyLocal()} className="mr-auto rounded-md border border-indigo-200 bg-indigo-50 px-3 py-1.5 text-indigo-700 hover:bg-indigo-100">{copied ? '已复制' : '复制我的内容'}</button>
              <button type="button" onClick={() => resolveConflict('remote')} className="rounded-md border border-slate-300 bg-white px-3 py-1.5 hover:bg-slate-50">使用服务器版本</button>
              <button type="button" onClick={() => resolveConflict('local')} className="rounded-md bg-amber-700 px-3 py-1.5 font-medium text-white hover:bg-amber-800">保留我的修改</button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
