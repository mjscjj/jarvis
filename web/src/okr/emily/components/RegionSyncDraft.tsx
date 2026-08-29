import { useEffect, useState } from 'react'
import { getRegionSyncDraft } from '../api'
import { buildRegionSyncMarkdown } from '../regionSyncMarkdown'
import type { RegionSyncDraft as RegionSyncDraftData } from '../types'

type State =
  | { kind: 'loading'; key: string }
  | { kind: 'ready'; key: string; data: RegionSyncDraftData }
  | { kind: 'error'; key: string; message: string }

const statusLabel: Record<string, string> = {
  not_started: '未开始', in_progress: '进行中', done: '已完成', at_risk: '有风险', delayed: 'Delay', blocked: '阻塞',
}

export function RegionSyncDraft({ quarter, week, onClose, onOpenPoint }: { quarter: string; week: string; onClose: () => void; onOpenPoint: (pointId: string) => void }) {
  const [region, setRegion] = useState('sea')
  const [selectedRegion, setSelectedRegion] = useState('sea')
  const key = `${quarter}:${week}:${selectedRegion}`
  const [state, setState] = useState<State>({ kind: 'loading', key })
  const [notice, setNotice] = useState('')

  useEffect(() => {
    let active = true
    void getRegionSyncDraft(quarter, week, selectedRegion)
      .then((data) => { if (active) setState({ kind: 'ready', key, data }) })
      .catch((error: unknown) => { if (active) setState({ kind: 'error', key, message: error instanceof Error ? error.message : '区域同步表加载失败。' }) })
    return () => { active = false }
  }, [key, quarter, selectedRegion, week])

  const visible = state.key === key ? state : { kind: 'loading' as const, key }
  const copy = async (data: RegionSyncDraftData) => {
    try {
      await navigator.clipboard.writeText(buildRegionSyncMarkdown(data).content)
      setNotice('同步表 Markdown 已复制；未发送到外部系统。')
    } catch {
      setNotice('浏览器未允许复制，可下载本地文件。')
    }
  }
  const download = (data: RegionSyncDraftData) => {
    const output = buildRegionSyncMarkdown(data)
    const url = URL.createObjectURL(new Blob([output.content], { type: 'text/markdown;charset=utf-8' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = output.filename
    anchor.click()
    URL.revokeObjectURL(url)
    setNotice(`已下载 ${output.filename}；未发布或外发。`)
  }

  return (
    <section className="mb-3 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm" aria-label="区域需求进度同步表草稿">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
        <div>
          <div className="flex items-center gap-2"><h2 className="text-xs font-semibold text-slate-800">区域需求进度同步表</h2><span className="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-medium text-amber-600">未发布 · 不外发</span></div>
          <p className="mt-0.5 text-[11px] text-slate-400">按 region 标签聚合，风险置顶；导出前先在页面审核。</p>
        </div>
        <button type="button" onClick={onClose} className="rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100">收起</button>
      </div>
      <div className="flex items-end gap-2 border-b border-slate-100 bg-slate-50/50 px-4 py-3">
        <label className="min-w-48 flex-1 text-[10px] text-slate-400">区域标签值<input value={region} onChange={(event) => setRegion(event.target.value)} placeholder="例如 sea" className="mt-1 block w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[11px] text-slate-600 outline-none focus:border-blue-400" /></label>
        <button type="button" disabled={!region.trim()} onClick={() => { setNotice(''); setSelectedRegion(region.trim()) }} className="rounded-md border border-blue-200 bg-white px-2.5 py-1.5 text-[11px] text-blue-600 hover:bg-blue-50 disabled:opacity-40">生成预览</button>
      </div>
      {visible.kind === 'loading' && <div className="px-4 py-7 text-center text-xs text-slate-400">正在整理区域需求…</div>}
      {visible.kind === 'error' && <div className="px-4 py-5 text-xs text-red-600">{visible.message}</div>}
      {visible.kind === 'ready' && <div className="p-4">
        <div className="mb-3 flex flex-wrap items-center gap-x-5 gap-y-2 text-[11px] text-slate-500">
          <span><b className="mr-1 text-base text-slate-800">{visible.data.summary.krCount}</b>条 KR</span><span><b className="mr-1 text-base text-slate-800">{visible.data.summary.pointCount}</b>个事项</span><span><b className="mr-1 text-base text-red-600">{visible.data.summary.riskCount}</b>项风险</span><span><b className="mr-1 text-base text-amber-600">{visible.data.summary.missingCount}</b>项未填</span>
          <span className="ml-auto text-[10px] text-slate-400">{buildRegionSyncMarkdown(visible.data).filename}</span>
          <button type="button" onClick={() => void copy(visible.data)} className="rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-blue-600 hover:bg-blue-50">复制 Markdown</button>
          <button type="button" onClick={() => download(visible.data)} className="rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-blue-600 hover:bg-blue-50">下载 .md</button>
        </div>
        {notice && <div className="mb-3 rounded-md bg-slate-50 px-3 py-2 text-[11px] text-slate-600">{notice}</div>}
        <div className="overflow-x-auto rounded-md border border-slate-200"><table className="w-full min-w-[900px] text-left text-[11px]"><thead className="bg-slate-50 text-slate-500"><tr>{['KR', '负责人', '事项', '状态', '本周进展', '来源'].map((label) => <th key={label} className="px-3 py-2 font-medium">{label}</th>)}</tr></thead><tbody className="divide-y divide-slate-100">
          {visible.data.rows.length === 0 ? <tr><td colSpan={6} className="px-3 py-8 text-center text-slate-400">当前区域暂无内容</td></tr> : visible.data.rows.map((row) => <tr key={row.pointId} className={row.risk ? 'bg-red-50/40' : row.missing ? 'bg-amber-50/30' : 'bg-white'}><td className="max-w-56 px-3 py-2 text-slate-600">{row.krTitle}</td><td className="whitespace-nowrap px-3 py-2 text-slate-500">{row.ownerName}</td><td className="max-w-64 px-3 py-2"><button type="button" onClick={() => onOpenPoint(row.pointId)} className="text-left text-blue-600 hover:underline">{row.pointTitle}</button></td><td className="whitespace-nowrap px-3 py-2 text-slate-500">{statusLabel[row.status] ?? row.status}</td><td className="max-w-80 whitespace-pre-wrap px-3 py-2 leading-5 text-slate-600">{row.detail}</td><td className="whitespace-nowrap px-3 py-2 text-[10px] text-slate-400">{row.source}</td></tr>)}
        </tbody></table></div>
      </div>}
    </section>
  )
}
