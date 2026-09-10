import { useEffect, useMemo, useState } from 'react'
import type { TextFile } from '../../../types'

export function AgentPromptCenter({ prompts, loading, initialPromptKey, onSave, onOpen }: {
  prompts: TextFile[]
  loading: boolean
  initialPromptKey?: string
  onSave: (key: string, content: string) => Promise<TextFile>
  onOpen: (prompt: TextFile) => void
}) {
  const [selectedKey, setSelectedKey] = useState('')
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; text: string }>()

  useEffect(() => {
    setDrafts((current) => Object.fromEntries(prompts.map((item) => [item.key, current[item.key] ?? item.content])))
    setSelectedKey((current) => {
      if (initialPromptKey && prompts.some((item) => item.key === initialPromptKey)) return initialPromptKey
      return current && prompts.some((item) => item.key === current) ? current : prompts[0]?.key ?? ''
    })
  }, [initialPromptKey, prompts])

  const selected = useMemo(() => prompts.find((item) => item.key === selectedKey), [prompts, selectedKey])
  const draft = selected ? drafts[selected.key] ?? selected.content : ''
  const dirty = Boolean(selected && draft.trim() !== selected.content)

  const select = (prompt: TextFile) => {
    setSelectedKey(prompt.key)
    setDrafts((current) => ({ ...current, [prompt.key]: current[prompt.key] ?? prompt.content }))
    setNotice(undefined)
    onOpen(prompt)
  }

  const save = async () => {
    if (!selected || !draft.trim()) return
    setSaving(true)
    try {
      const updated = await onSave(selected.key, draft.trim())
      setDrafts((current) => ({ ...current, [updated.key]: updated.content }))
      setNotice({ kind: 'success', text: updated.name + '已保存，后续评审或 Agent Task 会实时读取。' })
    } catch (cause) {
      setNotice({ kind: 'error', text: '保存失败：' + (cause instanceof Error ? cause.message : String(cause)) })
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <div className="py-16 text-center text-xs text-slate-400">正在读取 Prompt…</div>
  if (!selected) return <div className="py-16 text-center text-xs text-slate-400">没有未绑定定时行动的 Prompt</div>

  return <div>
    <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
      <div><h2 className="text-xs font-semibold text-slate-800">功能 Prompt</h2><p className="mt-0.5 text-[10px] text-slate-400">这里集中管理不属于定时行动任务详情的 Markdown Prompt。</p></div>
    </div>
    {notice && <div role="status" className={'border-b px-4 py-2 text-[10px] ' + (notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700')}>{notice.text}</div>}
    <div className="grid min-h-[620px] lg:grid-cols-[280px_minmax(0,1fr)]">
      <aside className="border-b border-slate-100 bg-slate-50/70 p-3 lg:border-r lg:border-b-0">
        <div className="space-y-1.5">{prompts.map((item) => <button key={item.key} type="button" onClick={() => select(item)} className={'w-full rounded-lg px-3 py-2.5 text-left ' + (selected.key === item.key ? 'bg-white text-cyan-700 shadow-sm ring-1 ring-cyan-100' : 'text-slate-600 hover:bg-white')}><div className="flex items-center gap-2"><span className="truncate text-[11px] font-medium">{item.name}</span><span className="ml-auto rounded bg-slate-100 px-1.5 py-0.5 text-[8px] text-slate-400">{item.kind === 'agent_policy' ? '共用原则' : 'Prompt'}</span></div><div className="mt-1 line-clamp-2 text-[9px] leading-4 text-slate-400">{item.description}</div></button>)}</div>
      </aside>
      <div className="flex min-w-0 flex-col">
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
          <div className="min-w-0"><h3 className="truncate text-xs font-semibold text-slate-800">{selected.name}</h3><code className="text-[9px] text-slate-400">{selected.key}</code></div>
          <button type="button" disabled={!dirty || saving} onClick={() => setDrafts((current) => ({ ...current, [selected.key]: selected.content }))} className="ml-auto rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 disabled:opacity-40">撤销</button>
          <button type="button" disabled={!dirty || saving || !draft.trim()} onClick={() => void save()} className="rounded-md bg-cyan-600 px-3 py-1.5 text-[10px] font-medium text-white disabled:opacity-40">{saving ? '保存中…' : '保存并生效'}</button>
        </div>
        <div className="flex min-h-0 flex-1 flex-col p-4">
          <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1"><span className="text-[9px] font-semibold uppercase tracking-wider text-slate-400">Prompt 内容（Markdown）</span><span className="text-[9px] text-slate-400">保存后的文件就是运行时真源</span></div>
          <textarea value={draft} onChange={(event) => setDrafts((current) => ({ ...current, [selected.key]: event.target.value }))} spellCheck={false} aria-label={selected.name + ' Prompt'} className="min-h-[520px] flex-1 resize-y rounded-xl border border-slate-200 bg-slate-50/40 p-4 font-mono text-[11px] leading-5 text-slate-700 outline-none focus:border-cyan-300 focus:bg-white" />
        </div>
      </div>
    </div>
  </div>
}
