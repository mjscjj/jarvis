import { useCallback, useEffect, useMemo, useState } from 'react'
import { listTextFiles, updateTextFile } from '../../../api'
import { usePageContext } from '../../../pageContext'
import type { TextFile } from '../../../types'
import type { OKRActionDefinition } from '../actionConfig'
import { AgentActionCenter } from './AgentActionCenter'

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

export function AgentFlowCenter({ weeklyEnabled, quarter }: { weeklyEnabled: boolean; quarter: string }) {
  const { context, navigate, setViewState } = usePageContext()
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

  useEffect(() => {
    if (!quarter || context.view_state.quarter === quarter) return
    setViewState({ ...context.view_state, tab: 'agent-flows', quarter }, true)
  }, [context.view_state, quarter, setViewState])

  const selected = useMemo(() => items.find((item) => item.key === selectedKey), [items, selectedKey])
  const draft = drafts[selectedKey] ?? ''
  const dirty = Boolean(selected && draft !== selected.content)
  const prompts = useMemo(() => items.filter((item) => item.kind === 'agent_prompt'), [items])

  const selectPrompt = (key: string, action?: OKRActionDefinition) => {
    setSelectedKey(key)
    setViewState({
      ...context.view_state,
      tab: 'agent-flows',
      prompt_key: key,
      action_key: action?.key,
      action_label: action?.title,
    }, true)
  }

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
      {notice && <div className={`rounded-lg border px-3 py-2 text-xs ${notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700'}`}>{notice.text}</div>}

		{weeklyEnabled ? <AgentActionCenter prompts={prompts} selectedPromptKey={selected?.kind === 'agent_prompt' ? selected.key : ''} onSelectPrompt={selectPrompt} /> : <section className="rounded-xl border border-slate-200 bg-white px-4 py-5 text-xs text-slate-500">周报模块未启用，相关固定行动已隐藏；OKR Prompt 仍可查看和编辑。</section>}

      <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
          <div><h2 className="text-xs font-semibold text-slate-800">行动使用的 Prompt</h2><p className="mt-0.5 text-[10px] text-slate-400">修改后，所有绑定该 Prompt 的行动会在下一次触发时读取新版本。</p></div>
          <button type="button" onClick={() => navigate('scheduled-tasks')} className="ml-auto rounded-md border border-slate-200 px-3 py-1.5 text-[10px] text-slate-600 hover:border-cyan-200 hover:text-cyan-700">通用自动化</button>
          <button type="button" disabled={loading} onClick={() => void load()} className="rounded-md border border-slate-200 px-3 py-1.5 text-[10px] text-slate-600 disabled:opacity-40">刷新</button>
        </div>

        {loading ? <div className="py-16 text-center text-xs text-slate-400">正在读取 Prompt…</div> : <div className="grid min-h-[620px] lg:grid-cols-[280px_minmax(0,1fr)]">
          <aside className="border-b border-slate-100 bg-slate-50/70 p-3 lg:border-r lg:border-b-0">
            <div className="space-y-1.5">{items.map((item) => <button key={item.key} type="button" onClick={() => selectPrompt(item.key)} className={`w-full rounded-lg px-3 py-2.5 text-left ${selectedKey === item.key ? 'bg-white text-cyan-700 shadow-sm ring-1 ring-cyan-100' : 'text-slate-600 hover:bg-white'}`}><div className="flex items-center gap-2"><span className="truncate text-[11px] font-medium">{item.name}</span><span className="ml-auto rounded bg-slate-100 px-1.5 py-0.5 text-[8px] text-slate-400">{item.kind === 'agent_policy' ? '原则' : 'Prompt'}</span></div><div className="mt-1 line-clamp-2 text-[9px] leading-4 text-slate-400">{item.description}</div></button>)}</div>
          </aside>
          {selected ? <div className="flex min-w-0 flex-col">
            <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3"><div className="min-w-0"><h3 className="truncate text-xs font-semibold text-slate-800">{selected.name}</h3><code className="text-[9px] text-slate-400">{selected.key}</code></div><button type="button" disabled={!dirty} onClick={() => setDrafts((current) => ({ ...current, [selected.key]: selected.content }))} className="ml-auto rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 disabled:opacity-40">撤销</button><button type="button" disabled={!dirty || saving || !draft.trim()} onClick={() => void save()} className="rounded-md bg-cyan-600 px-3 py-1.5 text-[10px] font-medium text-white disabled:opacity-40">{saving ? '保存中…' : '保存并生效'}</button></div>
            <div className="flex min-h-0 flex-1 flex-col p-4">
              <div className="mb-2 flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="text-[9px] font-semibold uppercase tracking-wider text-slate-400">Prompt 内容（Markdown）</span>
                <span className="text-[9px] text-slate-400">保存后的内容就是 Agent 实际读取的内容</span>
              </div>
              <textarea value={draft} onChange={(event) => setDrafts((current) => ({ ...current, [selected.key]: event.target.value }))} spellCheck={false} className="min-h-[520px] flex-1 resize-y rounded-xl border border-slate-200 bg-slate-50/40 p-4 font-mono text-[11px] leading-5 text-slate-700 outline-none focus:border-cyan-300 focus:bg-white" />
              <div className="mt-3 rounded-lg border border-dashed border-slate-200 bg-slate-50 px-3 py-2 text-[10px] leading-5 text-slate-500">固定行动只保存行动标识、Prompt key 和执行时间；范围、对象、产出与验收由这份 Prompt 完整定义。审批、状态和回执继续走 Jarvis 通用能力。</div>
            </div>
          </div> : <div className="py-16 text-center text-xs text-slate-400">没有注册的 Biz OKR Agent Prompt</div>}
        </div>}
      </section>
    </div>
  )
}
