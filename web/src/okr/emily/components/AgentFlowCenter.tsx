import { useCallback, useEffect, useMemo, useState } from 'react'
import MarkdownReport from '../../../components/MarkdownReport'
import { listTextFiles, updateTextFile } from '../../../api'
import { usePageContext } from '../../../pageContext'
import type { TextFile } from '../../../types'

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

export function AgentFlowCenter() {
  const { navigate } = usePageContext()
  const [items, setItems] = useState<TextFile[]>([])
  const [drafts, setDrafts] = useState<Record<string, string>>({})
  const [selectedKey, setSelectedKey] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; text: string }>()

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const response = await listTextFiles(signal)
      const next = response.items.filter((item) => item.stage === 'okr_agent')
      setItems(next)
      setDrafts(Object.fromEntries(next.map((item) => [item.key, item.content])))
      setSelectedKey((current) => current && next.some((item) => item.key === current) ? current : next[0]?.key || '')
    } catch (cause) {
      if (!signal?.aborted) setNotice({ kind: 'error', text: `Agent Prompt 读取失败：${errorText(cause)}` })
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const selected = useMemo(() => items.find((item) => item.key === selectedKey), [items, selectedKey])
  const draft = drafts[selectedKey] ?? ''
  const dirty = Boolean(selected && draft !== selected.content)
  const flowCount = items.filter((item) => item.kind === 'agent_prompt').length

  const save = async () => {
    if (!selected || !draft.trim()) return
    setSaving(true)
    try {
      const updated = await updateTextFile(selected.key, { content: draft.trim() })
      setItems((current) => current.map((item) => item.key === updated.key ? updated : item))
      setDrafts((current) => ({ ...current, [updated.key]: updated.content }))
      setNotice({ kind: 'success', text: `${updated.name}已保存；下一次 Agent Task 会实时读取。` })
    } catch (cause) {
      setNotice({ kind: 'error', text: `保存失败：${errorText(cause)}` })
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-3">
      <section className="grid gap-2 sm:grid-cols-3">
        <article className="rounded-xl border border-slate-200 bg-white p-3.5"><div className="text-[10px] text-slate-400">业务 Prompt</div><div className="mt-1 text-xl font-semibold text-slate-800">{flowCount}</div><p className="mt-1 text-[10px] text-slate-400">季度规划、对齐与 Report A/B/C</p></article>
        <article className="rounded-xl border border-slate-200 bg-white p-3.5"><div className="text-[10px] text-slate-400">统一执行 Skill</div><div className="mt-1 text-sm font-semibold text-cyan-700">okr-agent-orchestrator</div><p className="mt-1 text-[10px] text-slate-400">每次按实时事实动态规划，不走固定 workflow</p></article>
        <article className="rounded-xl border border-slate-200 bg-white p-3.5"><div className="text-[10px] text-slate-400">工具边界</div><div className="mt-1 text-sm font-semibold text-slate-800">原子读写 + 通用调度</div><p className="mt-1 text-[10px] text-slate-400">业务判断由 Agent 完成；代码只守机器边界</p></article>
      </section>

      {notice && <div className={`rounded-lg border px-3 py-2 text-xs ${notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700'}`}>{notice.text}</div>}

      <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
          <div><h2 className="text-xs font-semibold text-slate-800">Agent Prompt 模块</h2><p className="mt-0.5 text-[10px] text-slate-400">Prompt 只定义目标、输入、判断口径和验收；Agent 自己组合工具。</p></div>
          <button type="button" onClick={() => navigate('scheduled-tasks')} className="ml-auto rounded-md border border-slate-200 px-3 py-1.5 text-[10px] text-slate-600 hover:border-cyan-200 hover:text-cyan-700">通用自动化</button>
          <button type="button" disabled={loading} onClick={() => void load()} className="rounded-md border border-slate-200 px-3 py-1.5 text-[10px] text-slate-600 disabled:opacity-40">刷新</button>
        </div>

        {loading ? <div className="py-16 text-center text-xs text-slate-400">正在读取 Prompt…</div> : <div className="grid min-h-[620px] lg:grid-cols-[280px_minmax(0,1fr)]">
          <aside className="border-b border-slate-100 bg-slate-50/70 p-3 lg:border-r lg:border-b-0">
            <div className="space-y-1.5">{items.map((item) => <button key={item.key} type="button" onClick={() => setSelectedKey(item.key)} className={`w-full rounded-lg px-3 py-2.5 text-left ${selectedKey === item.key ? 'bg-white text-cyan-700 shadow-sm ring-1 ring-cyan-100' : 'text-slate-600 hover:bg-white'}`}><div className="flex items-center gap-2"><span className="truncate text-[11px] font-medium">{item.name}</span><span className="ml-auto rounded bg-slate-100 px-1.5 py-0.5 text-[8px] text-slate-400">{item.kind === 'agent_policy' ? '原则' : 'Prompt'}</span></div><div className="mt-1 line-clamp-2 text-[9px] leading-4 text-slate-400">{item.description}</div></button>)}</div>
          </aside>
          {selected ? <div className="flex min-w-0 flex-col">
            <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3"><div className="min-w-0"><h3 className="truncate text-xs font-semibold text-slate-800">{selected.name}</h3><code className="text-[9px] text-slate-400">{selected.key}</code></div><button type="button" disabled={!dirty} onClick={() => setDrafts((current) => ({ ...current, [selected.key]: selected.content }))} className="ml-auto rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 disabled:opacity-40">撤销</button><button type="button" disabled={!dirty || saving || !draft.trim()} onClick={() => void save()} className="rounded-md bg-cyan-600 px-3 py-1.5 text-[10px] font-medium text-white disabled:opacity-40">{saving ? '保存中…' : '保存并生效'}</button></div>
            <div className="grid min-h-0 flex-1 xl:grid-cols-2"><div className="flex min-h-0 flex-col border-b border-slate-100 p-4 xl:border-r xl:border-b-0"><div className="mb-2 text-[9px] font-semibold uppercase tracking-wider text-slate-400">Markdown 编辑</div><textarea value={draft} onChange={(event) => setDrafts((current) => ({ ...current, [selected.key]: event.target.value }))} spellCheck={false} className="min-h-96 flex-1 resize-none rounded-xl border border-slate-200 bg-slate-50/40 p-3 font-mono text-[11px] leading-5 text-slate-700 outline-none focus:border-cyan-300 focus:bg-white" /></div><div className="min-h-0 overflow-auto p-4"><div className="mb-2 text-[9px] font-semibold uppercase tracking-wider text-slate-400">Agent 看到的内容</div><MarkdownReport className="daily-digest-markdown" content={draft} /><div className="mt-5 rounded-lg border border-dashed border-slate-200 bg-slate-50 px-3 py-2 text-[10px] leading-5 text-slate-500">ScheduledTask 的指令写本次自然语言目标；冻结上下文只放 <code>{`{"module":"okr","skill":"okr-agent-orchestrator","prompt_key":"${selected.key}"}`}</code>。审批、状态和回执继续走 Jarvis 通用能力。</div></div></div>
          </div> : <div className="py-16 text-center text-xs text-slate-400">没有注册的 OKR Agent Prompt</div>}
        </div>}
      </section>
    </div>
  )
}
