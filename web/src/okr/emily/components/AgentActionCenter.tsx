import { useCallback, useEffect, useMemo, useState } from 'react'
import { createScheduledTask, createTask, deleteScheduledTask, executeTask, getScheduledTask, getTask, listScheduledTasks, updateScheduledTask } from '../../../api'
import type { ScheduledTask, Task, TextFile } from '../../../types'
import {
  OKR_ACTIONS,
  WEEKDAYS,
  actionKeyForSchedule,
  actionScheduleText,
  formValueForAction,
  manualTaskInput,
  scheduledTaskInput,
  type OKRActionCategory,
  type OKRActionDefinition,
  type OKRActionFormValue,
  type OKRActionKey,
} from '../actionConfig'
import { AgentPromptCenter } from './AgentPromptCenter'

const taskStatusMeta: Record<Task['status'], { label: string; tone: string }> = {
  pending: { label: '等待执行', tone: 'bg-slate-100 text-slate-600' },
  executing: { label: '执行中', tone: 'bg-blue-50 text-blue-700' },
  waiting: { label: '等待继续', tone: 'bg-amber-50 text-amber-700' },
  needs_human: { label: '需要处理', tone: 'bg-red-50 text-red-700' },
  done: { label: '已完成', tone: 'bg-emerald-50 text-emerald-700' },
  failed: { label: '失败', tone: 'bg-red-50 text-red-700' },
  observing: { label: '已检查', tone: 'bg-slate-100 text-slate-600' },
}

const actionTones: Record<OKRActionDefinition['tone'], string> = {
  amber: 'bg-amber-50 text-amber-700 ring-amber-100',
  blue: 'bg-blue-50 text-blue-700 ring-blue-100',
  violet: 'bg-violet-50 text-violet-700 ring-violet-100',
  emerald: 'bg-emerald-50 text-emerald-700 ring-emerald-100',
}

type AgentTab = OKRActionCategory | 'prompt'

const categoryTabs: Array<{ key: AgentTab; label: string; description: string }> = [
  { key: 'notification', label: '通知与跟进', description: '催填、巡检和进展跟进' },
  { key: 'material', label: '材料生成', description: '会议材料与对外周报' },
  { key: 'prompt', label: 'Prompt', description: '评审、规划与对齐提示词' },
]

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

function formatDateTime(value?: string | null): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))
}

function taskSummary(task?: Task): string {
  if (!task) return '尚未执行'
  const result = task.execution_result || {}
  const summary = typeof result.summary === 'string' ? result.summary.trim() : ''
  const followup = typeof result.needs_followup === 'string' ? result.needs_followup.trim() : ''
  return summary || task.summary || followup || taskStatusMeta[task.status].label
}

export function AgentActionCenter({ prompts, promptsLoading, actionsEnabled, initialPromptKey, onReloadPrompts, onSavePrompt, onOpenAction, onOpenPrompt }: {
  prompts: TextFile[]
  promptsLoading: boolean
  actionsEnabled: boolean
  initialPromptKey?: string
  onReloadPrompts: () => void
  onSavePrompt: (key: string, content: string) => Promise<TextFile>
  onOpenAction: (action: OKRActionDefinition) => void
  onOpenPrompt: (prompt: TextFile) => void
}) {
  const scheduledPromptKeys = useMemo(() => new Set(OKR_ACTIONS.map((item) => item.promptKey)), [])
  const standalonePrompts = useMemo(() => prompts.filter((item) => !scheduledPromptKeys.has(item.key)), [prompts, scheduledPromptKeys])
  const [activeCategory, setActiveCategory] = useState<AgentTab>(() => initialPromptKey && !scheduledPromptKeys.has(initialPromptKey) ? 'prompt' : 'notification')
  const [schedules, setSchedules] = useState<ScheduledTask[]>([])
  const [lastTasks, setLastTasks] = useState<Record<number, Task>>({})
  const [manualTaskIds, setManualTaskIds] = useState<Partial<Record<OKRActionKey, number>>>({})
  const [manualTasks, setManualTasks] = useState<Partial<Record<OKRActionKey, Task>>>({})
  const [loading, setLoading] = useState(true)
  const [busyKey, setBusyKey] = useState<OKRActionKey>()
  const [editing, setEditing] = useState<OKRActionDefinition>()
  const [form, setForm] = useState<OKRActionFormValue>()
  const [promptDraft, setPromptDraft] = useState('')
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; text: string }>()
  const promptByKey = useMemo(() => new Map(prompts.map((item) => [item.key, item])), [prompts])

  const scheduleByKey = useMemo(() => {
    const result = new Map<OKRActionKey, ScheduledTask>()
    for (const task of schedules) {
      const key = actionKeyForSchedule(task)
      if (key && !result.has(key)) result.set(key, task)
    }
    return result
  }, [schedules])

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const response = await listScheduledTasks('', signal)
      const fixedTitles = new Set(OKR_ACTIONS.map((item) => `OKR · ${item.title}`))
      const candidates = response.items.filter((item) => item.action_type === 'agent_task' && fixedTitles.has(item.title))
      const details = await Promise.all(candidates.map((item) => getScheduledTask(item.id, signal)))
      const fixedSchedules = details.filter((item) => actionKeyForSchedule(item))
      setSchedules(fixedSchedules)
      const taskPairs = await Promise.all(fixedSchedules
        .filter((item) => item.last_task_id)
        .map(async (item) => [item.id, await getTask(item.last_task_id!, signal)] as const))
      setLastTasks(Object.fromEntries(taskPairs))
    } catch (cause) {
      if (!signal?.aborted) setNotice({ kind: 'error', text: `定时任务读取失败：${errorText(cause)}` })
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
    const active = schedules.some((item) => item.status === 'running') || Object.values(lastTasks).some((task) => ['pending', 'executing', 'waiting'].includes(task.status))
    if (!active) return
    const timer = window.setInterval(() => void load(), 3500)
    return () => window.clearInterval(timer)
  }, [lastTasks, load, schedules])

  useEffect(() => {
    const entries = Object.entries(manualTaskIds) as Array<[OKRActionKey, number]>
    if (entries.length === 0) return
    const controller = new AbortController()
    let timer: number | undefined
    const refresh = async () => {
      try {
        const pairs = await Promise.all(entries.map(async ([key, id]) => [key, await getTask(id, controller.signal)] as const))
        setManualTasks(Object.fromEntries(pairs))
        if (pairs.some(([, task]) => ['pending', 'executing', 'waiting'].includes(task.status))) {
          timer = window.setTimeout(() => void refresh(), 3500)
        }
      } catch (cause) {
        if (!controller.signal.aborted) setNotice({ kind: 'error', text: `手动任务状态读取失败：${errorText(cause)}` })
      }
    }
    void refresh()
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [manualTaskIds])

  useEffect(() => {
    if (!editing) return
    setPromptDraft(promptByKey.get(editing.promptKey)?.content ?? '')
  }, [editing, promptByKey])

  const openEditor = (definition: OKRActionDefinition) => {
    setEditing(definition)
    setForm(formValueForAction(definition, scheduleByKey.get(definition.key)))
    setPromptDraft(promptByKey.get(definition.promptKey)?.content ?? '')
    setNotice(undefined)
    onOpenAction(definition)
  }

  const save = async () => {
    if (!editing || !form) return
    const prompt = promptByKey.get(editing.promptKey)
    if (!prompt) {
      setNotice({ kind: 'error', text: '这个任务的 Prompt 尚未加载，无法保存。' })
      return
    }
    if (!promptDraft.trim()) {
      setNotice({ kind: 'error', text: '任务 Prompt 不能为空。' })
      return
    }
    if (editing.cadence === 'interval' && form.intervalMinutes < 1) {
      setNotice({ kind: 'error', text: '巡检间隔必须大于 0 分钟。' })
      return
    }
    setBusyKey(editing.key)
    try {
      const input = scheduledTaskInput(editing, form)
      const existing = scheduleByKey.get(editing.key)
      if (existing) await updateScheduledTask(existing.id, input)
      else await createScheduledTask(input)
      if (promptDraft.trim() !== prompt.content) await onSavePrompt(prompt.key, promptDraft.trim())
      setEditing(undefined)
      setForm(undefined)
      setNotice({ kind: 'success', text: `${editing.title}已保存，时间配置与任务 Prompt 将在下次执行时生效。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `保存失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const toggle = async (definition: OKRActionDefinition, schedule?: ScheduledTask) => {
    setBusyKey(definition.key)
    try {
      const value = { ...formValueForAction(definition, schedule), enabled: !schedule?.enabled }
      if (schedule) await updateScheduledTask(schedule.id, scheduledTaskInput(definition, value))
      else await createScheduledTask(scheduledTaskInput(definition, value))
      setNotice({ kind: 'success', text: `${definition.title}已${value.enabled ? '启用' : '停用'}。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `更新失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const trigger = async (definition: OKRActionDefinition) => {
    setBusyKey(definition.key)
    onOpenAction(definition)
    try {
      const created = await createTask(manualTaskInput(definition))
      await executeTask(created.id)
      const task = await getTask(created.id)
      setManualTaskIds((current) => ({ ...current, [definition.key]: created.id }))
      setManualTasks((current) => ({ ...current, [definition.key]: task }))
      setNotice({ kind: 'success', text: `${definition.title}已开始执行。` })
    } catch (cause) {
      setNotice({ kind: 'error', text: `手动执行失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const remove = async (definition: OKRActionDefinition, schedule: ScheduledTask) => {
    if (!window.confirm(`删除“${definition.title}”的定时配置？任务 Prompt 会保留。`)) return
    setBusyKey(definition.key)
    try {
      await deleteScheduledTask(schedule.id)
      setEditing(undefined)
      setForm(undefined)
      setNotice({ kind: 'success', text: `${definition.title}的定时配置已删除。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `删除失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const configured = OKR_ACTIONS.filter((item) => scheduleByKey.has(item.key)).length
  const enabled = OKR_ACTIONS.filter((item) => scheduleByKey.get(item.key)?.enabled).length
  const attention = OKR_ACTIONS.filter((item) => {
    const schedule = scheduleByKey.get(item.key)
    const task = schedule ? lastTasks[schedule.id] : undefined
    return Boolean(schedule?.last_error_detail || task && ['failed', 'needs_human'].includes(task.status))
  }).length
  const nextAction = OKR_ACTIONS
    .map((definition) => ({ definition, schedule: scheduleByKey.get(definition.key) }))
    .filter((item): item is { definition: OKRActionDefinition; schedule: ScheduledTask } => Boolean(item.schedule?.enabled && item.schedule.status !== 'completed'))
    .sort((left, right) => left.schedule.next_run_at.localeCompare(right.schedule.next_run_at))[0]
  const visibleActions = activeCategory === 'prompt' ? [] : OKR_ACTIONS.filter((item) => item.category === activeCategory)
  const editingSchedule = editing ? scheduleByKey.get(editing.key) : undefined
  const editingTask = editing ? manualTasks[editing.key] ?? (editingSchedule ? lastTasks[editingSchedule.id] : undefined) : undefined
  const editingPrompt = editing ? promptByKey.get(editing.promptKey) : undefined
  const promptDirty = Boolean(editingPrompt && promptDraft.trim() !== editingPrompt.content)

  useEffect(() => {
    if (initialPromptKey && standalonePrompts.some((item) => item.key === initialPromptKey)) setActiveCategory('prompt')
  }, [initialPromptKey, standalonePrompts])

  return (
    <div className="space-y-3">
      <section aria-label="OKR Agent 概览" className="grid overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm sm:grid-cols-3 lg:grid-cols-5">
        {[
          { label: 'Agent 行动', value: String(OKR_ACTIONS.length), note: String(configured) + ' 已配置' },
          { label: '运行中', value: String(enabled), note: '已启用' },
          { label: '下次执行', value: nextAction ? formatDateTime(nextAction.schedule.next_run_at) : '暂无', note: nextAction?.definition.title },
          { label: '需要处理', value: String(attention), note: attention ? '请检查最近执行' : '运行正常' },
          { label: '功能 Prompt', value: String(standalonePrompts.length), note: 'Markdown' },
        ].map((item) => <article key={item.label} className="min-w-0 border-b border-r border-slate-100 px-4 py-3 last:border-r-0 sm:border-b-0"><span className="block text-[9px] text-slate-400">{item.label}</span><div className="mt-1 flex min-w-0 items-baseline gap-2"><b className="truncate text-[15px] font-semibold text-slate-800">{item.value}</b>{item.note && <span className="truncate text-[9px] text-slate-400">{item.note}</span>}</div></article>)}
      </section>

      <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-wrap items-end gap-2 border-b border-slate-100 px-4 pt-3" role="tablist" aria-label="OKR Agent 分类">
          <div className="flex gap-1 rounded-lg bg-slate-100 p-1 sm:w-fit">
            {categoryTabs.map((tab) => {
              const count = tab.key === 'prompt' ? standalonePrompts.length : OKR_ACTIONS.filter((item) => item.category === tab.key).length
              return <button key={tab.key} type="button" role="tab" aria-selected={activeCategory === tab.key} onClick={() => setActiveCategory(tab.key)} className={`min-w-36 rounded-md px-3 py-2 text-left transition-colors ${activeCategory === tab.key ? 'bg-white text-slate-900 shadow-sm' : 'text-slate-500 hover:text-slate-700'}`}><span className="flex items-center gap-2 text-[11px] font-semibold"><span>{tab.label}</span><span className="rounded-full bg-slate-100 px-1.5 text-[9px] font-medium text-slate-500">{count}</span></span><span className="mt-0.5 block text-[9px] font-normal text-slate-400">{tab.description}</span></button>
            })}
          </div>
          <button type="button" disabled={loading || promptsLoading} onClick={() => { void load(); onReloadPrompts() }} className="mb-3 ml-auto rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 disabled:opacity-40">刷新</button>
        </div>

        {notice && <div role="status" className={`border-b px-4 py-2 text-[10px] ${notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700'}`}>{notice.text}</div>}

        {activeCategory === 'prompt' ? <AgentPromptCenter prompts={standalonePrompts} loading={promptsLoading} initialPromptKey={initialPromptKey} onSave={onSavePrompt} onOpen={onOpenPrompt} /> : !actionsEnabled ? <div className="py-14 text-center text-xs text-slate-400">当前 OKR 周报模块未启用，Agent 行动暂不可用。</div> : loading ? <div className="py-14 text-center text-xs text-slate-400">正在读取 Agent 行动…</div> : <div className="divide-y divide-slate-100">{visibleActions.map((definition) => {
          const schedule = scheduleByKey.get(definition.key)
          const lastTask = manualTasks[definition.key] ?? (schedule ? lastTasks[schedule.id] : undefined)
          const taskMeta = lastTask ? taskStatusMeta[lastTask.status] : undefined
          const busy = busyKey === definition.key
          return <article key={definition.key} className="grid items-center gap-3 px-4 py-3 transition-colors hover:bg-slate-50/60 lg:grid-cols-[minmax(260px,1.2fr)_minmax(170px,.7fr)_minmax(220px,1fr)_auto]">
            <div className="flex min-w-0 items-start gap-3">
              <span className={`mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold ring-1 ${actionTones[definition.tone]}`}>{definition.shortLabel}</span>
              <div className="min-w-0"><div className="flex flex-wrap items-center gap-1.5"><h3 className="text-[12px] font-semibold text-slate-800">{definition.title}</h3><span className={`rounded-full px-2 py-0.5 text-[9px] ${schedule?.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{schedule ? schedule.enabled ? '已启用' : '已停用' : '未配置'}</span></div><p className="mt-1 line-clamp-2 text-[10px] leading-4 text-slate-500">{definition.description}</p></div>
            </div>
            <div className="text-[10px] leading-5 text-slate-500"><span className="block text-[9px] text-slate-400">执行计划</span><b className="font-medium text-slate-700">{actionScheduleText(definition, schedule)}</b><span className="block text-slate-400">下次 {schedule?.enabled ? formatDateTime(schedule.next_run_at) : '—'}</span></div>
            <div className="min-w-0 text-[10px] leading-5 text-slate-500"><div className="flex items-center gap-1.5"><span className="text-slate-400">最近执行</span>{taskMeta ? <span className={`rounded-full px-2 py-0.5 text-[9px] ${taskMeta.tone}`}>{taskMeta.label}</span> : <span>尚未执行</span>}</div><p className="truncate" title={schedule?.last_error_detail || taskSummary(lastTask)}>{schedule?.last_error_detail || taskSummary(lastTask)}</p></div>
            <div className="flex flex-wrap justify-end gap-1.5"><button type="button" onClick={() => openEditor(definition)} className="rounded-md border border-cyan-200 bg-cyan-50 px-2.5 py-1.5 text-[10px] font-medium text-cyan-700 hover:bg-cyan-100">任务详情</button><button type="button" disabled={busy || schedule?.status === 'running'} onClick={() => void toggle(definition, schedule)} className="rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-600 disabled:opacity-40">{schedule?.enabled ? '停用' : '启用'}</button><button type="button" disabled={busy || ['pending', 'executing', 'waiting'].includes(manualTasks[definition.key]?.status ?? '')} onClick={() => void trigger(definition)} className="rounded-md bg-emerald-600 px-2.5 py-1.5 text-[10px] font-medium text-white hover:bg-emerald-700 disabled:opacity-40">立即运行</button></div>
          </article>
        })}</div>}
      </section>

      {editing && form && <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/35 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget && !busyKey) setEditing(undefined) }}>
        <section role="dialog" aria-modal="true" aria-label={`${editing.title}任务详情`} className="flex max-h-[92vh] w-full max-w-4xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
          <header className="flex items-start gap-3 border-b border-slate-100 px-5 py-4">
            <span className={`flex size-9 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold ring-1 ${actionTones[editing.tone]}`}>{editing.shortLabel}</span>
            <div className="min-w-0"><div className="text-[10px] font-medium text-cyan-700">任务详情</div><h2 className="text-[15px] font-semibold text-slate-900">{editing.title}</h2><p className="mt-1 text-[10px] text-slate-500">{editing.description}</p></div>
            <button type="button" aria-label="关闭任务详情" disabled={Boolean(busyKey)} onClick={() => setEditing(undefined)} className="ml-auto rounded-md px-2 py-1 text-lg leading-none text-slate-400 hover:bg-slate-100 hover:text-slate-600 disabled:opacity-40">×</button>
          </header>

          <div className="min-h-0 flex-1 overflow-y-auto p-5">
            <div className="grid gap-4 lg:grid-cols-[minmax(0,1.15fr)_minmax(250px,.85fr)]">
              <section className="rounded-xl border border-slate-200 p-4">
                <div className="mb-3"><h3 className="text-[12px] font-semibold text-slate-800">执行计划</h3><p className="mt-0.5 text-[9px] text-slate-400">设置任务何时自动运行。</p></div>
                <div className="grid gap-3 sm:grid-cols-2">
                  {editing.cadence === 'weekly' ? <><label className="text-[10px] font-medium text-slate-500">每周执行日<select value={form.weekday} onChange={(event) => setForm({ ...form, weekday: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-[11px] text-slate-700">{WEEKDAYS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label><label className="text-[10px] font-medium text-slate-500">执行时间<input type="time" value={form.time} onChange={(event) => setForm({ ...form, time: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px] text-slate-700" /></label></> : <label className="text-[10px] font-medium text-slate-500 sm:col-span-2">巡检间隔（分钟）<input type="number" min={1} value={form.intervalMinutes} onChange={(event) => setForm({ ...form, intervalMinutes: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px] text-slate-700" /></label>}
                  <label className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 bg-slate-50/60 px-3 py-2.5 text-[10px] text-slate-600 sm:col-span-2"><span><b className="block font-medium text-slate-700">启用自动执行</b><span className="text-[9px] text-slate-400">关闭后保留时间和 Prompt</span></span><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} className="size-4 accent-cyan-600" /></label>
                </div>
              </section>

              <section className="rounded-xl border border-slate-200 p-4">
                <h3 className="text-[12px] font-semibold text-slate-800">运行状态</h3>
                <dl className="mt-3 grid grid-cols-[4.5rem_1fr] gap-y-2 text-[10px]"><dt className="text-slate-400">当前状态</dt><dd className="font-medium text-slate-700">{editingSchedule ? editingSchedule.enabled ? '已启用' : '已停用' : '未配置'}</dd><dt className="text-slate-400">下次执行</dt><dd className="text-slate-700">{editingSchedule?.enabled ? formatDateTime(editingSchedule.next_run_at) : '—'}</dd><dt className="text-slate-400">最近结果</dt><dd className="min-w-0 text-slate-700">{editingTask ? taskStatusMeta[editingTask.status].label : '尚未执行'}</dd></dl>
                <p className="mt-3 line-clamp-3 rounded-lg bg-slate-50 px-3 py-2 text-[10px] leading-5 text-slate-500" title={editingSchedule?.last_error_detail || taskSummary(editingTask)}>{editingSchedule?.last_error_detail || taskSummary(editingTask)}</p>
              </section>
            </div>

            <section className="mt-4 overflow-hidden rounded-xl border border-slate-200">
              <div className="flex flex-wrap items-start gap-2 border-b border-slate-100 bg-slate-50/60 px-4 py-3"><div><h3 className="text-[12px] font-semibold text-slate-800">任务 Prompt</h3><p className="mt-0.5 text-[9px] text-slate-400">定义执行范围、对象、产出与验收标准；保存后下次任务实时读取。</p></div>{editingPrompt && <span className="ml-auto max-w-full truncate rounded bg-white px-2 py-1 font-mono text-[9px] text-slate-400">{editingPrompt.name} · {editingPrompt.key}</span>}</div>
              {promptsLoading ? <div className="py-16 text-center text-xs text-slate-400">正在读取 Prompt…</div> : editingPrompt ? <textarea value={promptDraft} onChange={(event) => setPromptDraft(event.target.value)} spellCheck={false} aria-label={`${editing.title} Prompt`} className="min-h-[300px] w-full resize-y border-0 bg-white p-4 font-mono text-[11px] leading-5 text-slate-700 outline-none focus:bg-cyan-50/20" /> : <div className="m-4 rounded-lg border border-red-100 bg-red-50 px-3 py-3 text-[10px] text-red-700">未找到这个任务绑定的 Prompt，请刷新后重试。</div>}
            </section>
          </div>

          <footer className="flex flex-wrap items-center gap-2 border-t border-slate-100 bg-white px-5 py-3">
            {editingSchedule && <button type="button" disabled={busyKey === editing.key || editingSchedule.status === 'running'} onClick={() => void remove(editing, editingSchedule)} className="rounded-lg px-3 py-2 text-[10px] text-red-500 hover:bg-red-50 disabled:opacity-40">删除定时配置</button>}
            {promptDirty && <span className="text-[9px] text-amber-600">Prompt 有未保存修改</span>}
            <button type="button" disabled={Boolean(busyKey)} onClick={() => setEditing(undefined)} className="ml-auto rounded-lg px-3 py-2 text-[11px] text-slate-500 disabled:opacity-40">取消</button>
            <button type="button" disabled={busyKey === editing.key || promptsLoading || !editingPrompt || !promptDraft.trim() || editingSchedule?.status === 'running'} onClick={() => void save()} className="rounded-lg bg-cyan-600 px-4 py-2 text-[11px] font-medium text-white hover:bg-cyan-700 disabled:opacity-40">{busyKey === editing.key ? '保存中…' : '保存任务'}</button>
          </footer>
        </section>
      </div>}
    </div>
  )
}
