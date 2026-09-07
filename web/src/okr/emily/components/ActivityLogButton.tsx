import { HistoryOutlined, ReloadOutlined } from '@ant-design/icons'
import { Drawer } from 'antd'
import { useCallback, useEffect, useState } from 'react'
import { getOKRActivities } from '../api'
import type { OKRActivityEntry } from '../types'

const timeFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: 'numeric',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

function displayTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : timeFormatter.format(date)
}

export function ActivityLogButton({
  surface,
  quarter,
  week,
  planId,
  disabled = false,
}: {
  surface: 'plan' | 'weekly'
  quarter?: string
  week?: string
  planId?: string
  disabled?: boolean
}) {
  const [open, setOpen] = useState(false)
  const [items, setItems] = useState<OKRActivityEntry[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = useCallback(async () => {
    if (disabled) return
    setLoading(true)
    setError('')
    try {
      setItems(await getOKRActivities({ surface, quarter, week, planId }))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : '操作记录读取失败。')
    } finally {
      setLoading(false)
    }
  }, [disabled, planId, quarter, surface, week])

  useEffect(() => {
    if (!open) return
    void load()
  }, [load, open])

  return <>
    <button
      type="button"
      disabled={disabled}
      title="操作记录"
      aria-label="查看操作记录"
      onClick={() => setOpen(true)}
      className="inline-flex h-8 items-center gap-1 rounded-md px-1.5 text-[10px] font-medium text-slate-400 transition-colors hover:bg-slate-50 hover:text-slate-600 disabled:cursor-not-allowed disabled:opacity-30"
    >
      <HistoryOutlined className="text-[11px]" />
      <span>记录</span>
    </button>
    <Drawer
      title="操作记录"
      open={open}
      width={360}
      onClose={() => setOpen(false)}
      extra={<button type="button" title="刷新" aria-label="刷新操作记录" disabled={loading} onClick={() => void load()} className="rounded p-1.5 text-slate-400 hover:bg-slate-100 hover:text-slate-600 disabled:opacity-40"><ReloadOutlined spin={loading} /></button>}
    >
      {error && <div className="mb-3 rounded-lg border border-red-100 bg-red-50 px-3 py-2 text-xs text-red-700">{error}</div>}
      {!error && loading && items.length === 0 && <div className="py-10 text-center text-xs text-slate-400">正在读取…</div>}
      {!error && !loading && items.length === 0 && <div className="py-10 text-center text-xs text-slate-400">暂无操作记录</div>}
      {items.length > 0 && <div className="space-y-0">
        {items.map((item, index) => <div key={`${item.at}-${item.action}-${item.targetId ?? ''}-${index}`} className="relative border-l border-slate-200 pb-5 pl-4 last:pb-0">
          <span className="absolute -left-[4.5px] top-1.5 size-2 rounded-full border border-slate-300 bg-white" />
          <div className="text-xs leading-5 text-slate-700">{item.summary}</div>
          <div className="mt-1 flex items-center gap-1.5 text-[10px] text-slate-400">
            <span>{item.actorName || item.actorId || 'Jarvis'}</span>
            <span aria-hidden>·</span>
            <time>{displayTime(item.at)}</time>
          </div>
        </div>)}
      </div>}
    </Drawer>
  </>
}
