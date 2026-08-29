import { useEffect, useState } from 'react'
import { APIError, createFeishuDocument, generateReportDraft, getReportDraftHistory, restoreReportDraft, saveReportDraft } from '../api'
import { buildReportDraftMarkdown } from '../reportMarkdown'
import type { ReportDraft as ReportDraftData, ReportDraftHistory, ReportType } from '../types'

type DraftState =
  | { kind: 'loading'; requestKey: string }
  | { kind: 'ready'; requestKey: string; data: ReportDraftData }
  | { kind: 'error'; requestKey: string; message: string }

type HistoryState =
  | { kind: 'closed' }
  | { kind: 'loading' }
  | { kind: 'ready'; data: ReportDraftHistory }
  | { kind: 'error'; message: string }

const reportOptions: Array<{ value: ReportType; label: string }> = [
  { value: 'middle_platform_weekly', label: '中台周报' },
  { value: 'biweekly_review', label: '双周会材料' },
]

const tagOptions = [
  { value: '', label: '全部标签' },
  { value: 'management_focus', label: '管理重点' },
  { value: 'biweekly', label: '双周会' },
  { value: 'central_weekly', label: '中台周报' },
  { value: 'region', label: '区域' },
]

export function ReportDraft({ quarter, week, onClose, onOpenPoint }: { quarter: string; week: string; onClose: () => void; onOpenPoint: (pointId: string) => void }) {
  const [reportType, setReportType] = useState<ReportType>('middle_platform_weekly')
  const [tagType, setTagType] = useState('')
  const [tagValue, setTagValue] = useState('')
  const [query, setQuery] = useState<{ reportType: ReportType; tagType: string; tagValue: string; serial: number }>({ reportType: 'middle_platform_weekly', tagType: '', tagValue: '', serial: 0 })
  const requestKey = `${quarter}:${week}:${query.reportType}:${query.tagType}:${query.tagValue}:${query.serial}`
  const [state, setState] = useState<DraftState>({ kind: 'loading', requestKey })
  const [saving, setSaving] = useState(false)
  const [restoring, setRestoring] = useState(0)
  const [notice, setNotice] = useState('')
  const [history, setHistory] = useState<HistoryState>({ kind: 'closed' })
  const [exportOpen, setExportOpen] = useState(false)
  const [exportingFeishu, setExportingFeishu] = useState(false)
  const [feishuURL, setFeishuURL] = useState('')

  useEffect(() => {
    let active = true
    void generateReportDraft({ quarter, week, reportType: query.reportType, tagType: query.tagType, tagValue: query.tagValue })
      .then((data) => { if (active) setState({ kind: 'ready', requestKey, data }) })
      .catch((error: unknown) => { if (active) setState({ kind: 'error', requestKey, message: error instanceof Error ? error.message : '汇报草稿生成失败。' }) })
    return () => { active = false }
  }, [quarter, query.reportType, query.tagType, query.tagValue, requestKey, week])

  const visibleState: DraftState = state.requestKey === requestKey ? state : { kind: 'loading', requestKey }

  const regenerate = () => {
    setNotice('')
    setHistory({ kind: 'closed' })
    setExportOpen(false)
    setFeishuURL('')
    setQuery({ reportType, tagType, tagValue: tagValue.trim(), serial: query.serial + 1 })
  }

  const loadHistory = async (draftId: string) => {
    setHistory({ kind: 'loading' })
    try {
      setHistory({ kind: 'ready', data: await getReportDraftHistory(draftId) })
    } catch (error) {
      setHistory({ kind: 'error', message: error instanceof Error ? error.message : '版本历史加载失败。' })
    }
  }

  const updateTitle = (title: string) => {
    if (visibleState.kind !== 'ready') return
    setState({ ...visibleState, data: { ...visibleState.data, title } })
  }

  const updateItem = (sectionIndex: number, itemIndex: number, detail: string) => {
    if (visibleState.kind !== 'ready') return
    const sections = visibleState.data.sections.map((section, currentSection) => currentSection !== sectionIndex ? section : {
      ...section,
      items: section.items.map((item, currentItem) => currentItem === itemIndex ? { ...item, detail } : item),
    })
    setState({ ...visibleState, data: { ...visibleState.data, sections } })
  }

  const save = async () => {
    if (visibleState.kind !== 'ready' || !visibleState.data.title.trim()) return
    setSaving(true)
    setNotice('')
    try {
      const data = await saveReportDraft(visibleState.data)
      setState({ kind: 'ready', requestKey, data })
      setNotice('草稿已保存；KR 原始进展未改动。')
      if (history.kind !== 'closed') await loadHistory(data.id)
    } catch (error) {
      if (error instanceof APIError && error.status === 409) {
        setNotice('草稿已被其他人更新，请重新生成后再编辑。')
      } else {
        setNotice(error instanceof Error ? error.message : '草稿保存失败。')
      }
    } finally {
      setSaving(false)
    }
  }

  const restore = async (draft: ReportDraftData, revisionVersion: number) => {
    setRestoring(revisionVersion)
    setNotice('')
    try {
      const data = await restoreReportDraft(draft.id, draft.version, revisionVersion)
      setState({ kind: 'ready', requestKey, data })
      setNotice(`已将 v${revisionVersion} 恢复为新的 v${data.version}；历史版本均保留。`)
      await loadHistory(data.id)
    } catch (error) {
      setNotice(error instanceof APIError && error.status === 409 ? '草稿已被其他人更新，请重新生成后再恢复。' : error instanceof Error ? error.message : '恢复失败。')
    } finally {
      setRestoring(0)
    }
  }

  const copyMarkdown = async (draft: ReportDraftData) => {
    try {
      await navigator.clipboard.writeText(buildReportDraftMarkdown(draft).content)
      setNotice('Markdown 已复制到剪贴板；未发送到外部系统。')
    } catch {
      setNotice('浏览器未允许复制，请在预览框中手动复制。')
    }
  }

  const downloadMarkdown = (draft: ReportDraftData) => {
    const output = buildReportDraftMarkdown(draft)
    const url = URL.createObjectURL(new Blob([output.content], { type: 'text/markdown;charset=utf-8' }))
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = output.filename
    anchor.click()
    URL.revokeObjectURL(url)
    setNotice(`已下载 ${output.filename}；未发布或外发。`)
  }

  const exportToFeishu = async (draft: ReportDraftData) => {
    if (exportingFeishu || !draft.title.trim()) return
    setExportingFeishu(true)
    setFeishuURL('')
    setNotice('')
    try {
      const output = buildReportDraftMarkdown(draft)
      const result = await createFeishuDocument(draft.title, output.content)
      setFeishuURL(result.url)
      setNotice(result.warnings.length > 0 ? `飞书文档已生成；有 ${result.warnings.length} 条内容转换提示。` : '飞书文档已生成。')
    } catch (error) {
      setNotice(error instanceof Error ? error.message : '飞书文档生成失败。')
    } finally {
      setExportingFeishu(false)
    }
  }

  return (
    <section className="mb-3 overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm" aria-label="可追溯汇报材料草稿">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
        <div>
          <div className="flex items-center gap-2">
            <h2 className="text-xs font-semibold text-slate-800">汇报材料草稿</h2>
            <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-medium text-amber-600">草稿 · 导出前可编辑</span>
          </div>
          <p className="mt-0.5 text-[11px] text-slate-400">从 KR 事实生成，可人工调整表达；来源与原始进展保持独立。</p>
        </div>
        <button type="button" onClick={onClose} className="rounded-md px-2 py-1 text-[11px] text-slate-400 hover:bg-slate-100 hover:text-slate-600">收起</button>
      </div>

      <div className="flex flex-wrap items-end gap-2 border-b border-slate-100 bg-slate-50/50 px-4 py-3">
        <label className="text-[10px] text-slate-400">材料类型
          <select value={reportType} onChange={(event) => setReportType(event.target.value as ReportType)} className="mt-1 block rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[11px] text-slate-600 outline-none focus:border-blue-400">
            {reportOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </label>
        <label className="text-[10px] text-slate-400">标签范围
          <select value={tagType} onChange={(event) => { setTagType(event.target.value); if (!event.target.value) setTagValue('') }} className="mt-1 block rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[11px] text-slate-600 outline-none focus:border-blue-400">
            {tagOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </label>
        {tagType && <label className="min-w-40 flex-1 text-[10px] text-slate-400">标签值
          <input value={tagValue} onChange={(event) => setTagValue(event.target.value)} placeholder="例如 sea" className="mt-1 block w-full rounded-md border border-slate-200 bg-white px-2 py-1.5 text-[11px] text-slate-600 outline-none focus:border-blue-400" />
        </label>}
        <button type="button" disabled={Boolean(tagType && !tagValue.trim())} onClick={regenerate} className="rounded-md border border-blue-200 bg-white px-2.5 py-1.5 text-[11px] text-blue-600 hover:bg-blue-50 disabled:cursor-not-allowed disabled:opacity-40">重新生成</button>
      </div>

      {visibleState.kind === 'loading' && <div className="px-4 py-8 text-center text-xs text-slate-400">正在整理可追溯草稿…</div>}
      {visibleState.kind === 'error' && <div className="px-4 py-5 text-xs text-red-600">{visibleState.message}</div>}
      {visibleState.kind === 'ready' && (
        <div className="p-4">
          <div className="mb-3 flex flex-wrap items-center gap-2">
            <input value={visibleState.data.title} onChange={(event) => updateTitle(event.target.value)} className="min-w-64 flex-1 rounded-md border border-slate-200 px-3 py-2 text-sm font-medium text-slate-800 outline-none focus:border-blue-400" />
            <span className="text-[10px] text-slate-400">{visibleState.data.saved ? `已保存 v${visibleState.data.version}` : '尚未保存'}</span>
            <button type="button" disabled={exportingFeishu || !visibleState.data.title.trim()} onClick={() => void exportToFeishu(visibleState.data)} className="rounded-md bg-blue-600 px-2.5 py-2 text-[11px] font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">{exportingFeishu ? '导出中…' : '导出飞书文档'}</button>
            {feishuURL && <a href={feishuURL} target="_blank" rel="noreferrer" className="text-[11px] font-medium text-blue-600 hover:underline">打开文档</a>}
            <button type="button" onClick={() => setExportOpen((value) => !value)} className="rounded-md border border-slate-200 px-2.5 py-2 text-[11px] text-slate-500 hover:border-blue-200 hover:text-blue-600">{exportOpen ? '收起导出' : 'Markdown 预览'}</button>
            <button type="button" disabled={!visibleState.data.saved} onClick={() => history.kind === 'closed' ? void loadHistory(visibleState.data.id) : setHistory({ kind: 'closed' })} className="rounded-md border border-slate-200 px-2.5 py-2 text-[11px] text-slate-500 hover:border-blue-200 hover:text-blue-600 disabled:cursor-not-allowed disabled:opacity-40">{history.kind === 'closed' ? '版本历史' : '收起历史'}</button>
            <button type="button" disabled={saving || !visibleState.data.title.trim()} onClick={() => void save()} className="rounded-md bg-blue-600 px-3 py-2 text-[11px] text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40">{saving ? '保存中…' : '保存草稿'}</button>
          </div>
          {notice && <div className="mb-3 rounded-md bg-slate-50 px-3 py-2 text-[11px] text-slate-600">{notice}</div>}
          {exportOpen && (() => {
            const output = buildReportDraftMarkdown(visibleState.data)
            return (
              <div className="mb-3 rounded-md border border-slate-200 bg-slate-50/50 p-3" aria-label="Markdown 导出预览">
                <div className="mb-2 flex flex-wrap items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <h3 className="text-[11px] font-semibold text-slate-700">Markdown 导出预览</h3>
                    <div className="truncate text-[10px] text-slate-400">{output.filename} · 来源脚注随文保留 · 仅本地操作</div>
                  </div>
                  <button type="button" onClick={() => void copyMarkdown(visibleState.data)} className="rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[10px] text-blue-600 hover:bg-blue-50">复制</button>
                  <button type="button" onClick={() => downloadMarkdown(visibleState.data)} className="rounded-md border border-slate-200 bg-white px-2.5 py-1.5 text-[10px] text-blue-600 hover:bg-blue-50">下载 .md</button>
                </div>
                <textarea readOnly value={output.content} rows={12} className="w-full resize-y rounded-md border border-slate-200 bg-white px-3 py-2 font-mono text-[10px] leading-5 text-slate-600 outline-none" />
              </div>
            )
          })()}
          {history.kind !== 'closed' && (
            <div className="mb-3 rounded-md border border-slate-200 bg-slate-50/50 p-3">
              <div className="mb-2 flex items-center justify-between">
                <h3 className="text-[11px] font-semibold text-slate-700">版本历史</h3>
                <span className="text-[10px] text-slate-400">恢复会生成新版本，不会删除历史</span>
              </div>
              {history.kind === 'loading' && <div className="py-3 text-center text-[11px] text-slate-400">正在读取历史…</div>}
              {history.kind === 'error' && <div className="py-2 text-[11px] text-red-600">{history.message}</div>}
              {history.kind === 'ready' && (
                <div className="space-y-1.5">
                  {history.data.revisions.map((revision) => (
                    <details key={revision.version} className="rounded-md bg-white px-3 py-2 ring-1 ring-slate-100">
                      <summary className="flex cursor-pointer list-none items-center gap-2 text-[11px] text-slate-600">
                        <b className="text-slate-800">v{revision.version}</b>
                        <span>{revision.updatedBy || '未知编辑者'}</span>
                        <span className="text-slate-400">{new Date(revision.createdAt).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</span>
                        <span className="ml-auto text-[10px] text-slate-400">{revision.titleChanged ? '标题有变化 · ' : ''}{revision.changedItemCount} 项内容变化</span>
                        {revision.version !== history.data.currentVersion && <button type="button" disabled={restoring !== 0} onClick={(event) => { event.preventDefault(); void restore(visibleState.data, revision.version) }} className="rounded-md border border-slate-200 px-2 py-1 text-[10px] text-blue-600 hover:bg-blue-50 disabled:opacity-40">{restoring === revision.version ? '恢复中…' : '恢复此版'}</button>}
                      </summary>
                      <div className="mt-2 border-t border-slate-100 pt-2">
                        <div className="mb-1 text-[11px] font-medium text-slate-700">{revision.title}</div>
                        {revision.changedItems.length === 0 ? <div className="text-[10px] text-slate-400">首个版本，无上版差异。</div> : revision.changedItems.map((item) => (
                          <div key={item.id} className="mt-1.5 rounded bg-slate-50 px-2 py-1.5 text-[10px] leading-4 text-slate-500">
                            <div className="font-medium text-slate-600">{item.pointTitle}</div>
                            <div>上版：{item.before || '无'}</div>
                            <div>本版：{item.after || '无'}</div>
                          </div>
                        ))}
                      </div>
                    </details>
                  ))}
                </div>
              )}
            </div>
          )}
          <div className="space-y-3">
            {visibleState.data.sections.map((section, sectionIndex) => (
              <div key={section.kind} className="rounded-md border border-slate-200 p-3">
                <div className="mb-2 flex items-center justify-between">
                  <h3 className="text-xs font-semibold text-slate-700">{section.title}</h3>
                  <span className="text-[10px] text-slate-400">{section.items.length} 项</span>
                </div>
                {section.items.length === 0 ? <div className="py-3 text-center text-[11px] text-slate-400">当前范围暂无内容</div> : (
                  <div className="space-y-2">
                    {section.items.map((item, itemIndex) => (
                      <article key={item.id} className="rounded-md bg-slate-50/70 p-3">
                        <div className="mb-1.5 flex items-start gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="text-[11px] font-medium text-slate-700">{item.pointTitle}</div>
                            <div className="mt-0.5 text-[10px] text-slate-400">{item.ownerName} · {item.week} · {item.source}</div>
                          </div>
                          <button type="button" onClick={() => onOpenPoint(item.pointId)} className="rounded-md border border-slate-200 bg-white px-2 py-1 text-[10px] text-slate-500 hover:border-blue-200 hover:text-blue-600">查看事项</button>
                        </div>
                        <textarea value={item.detail} onChange={(event) => updateItem(sectionIndex, itemIndex, event.target.value)} rows={2} className="w-full resize-y rounded-md border border-slate-200 bg-white px-2.5 py-2 text-[11px] leading-5 text-slate-600 outline-none focus:border-blue-400" />
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
