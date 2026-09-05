import { useCallback, useEffect, useMemo, useState } from 'react'
import { createScheduledTask, createTask, deleteScheduledTask, executeTask, getTask, listScheduledTasks, updateScheduledTask } from '../../../api'
import type { ScheduledTask, Task, TextFile } from '../../../types'
import {
  OKR_ACTIONS,
  WEEKDAYS,
  actionKeyForSchedule,
  actionScheduleText,
  formValueForAction,
  manualTaskInput,
  scheduledTaskInput,
  type OKRActionDefinition,
  type OKRActionFormValue,
  type OKRActionKey,
} from '../actionConfig'

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

export function AgentActionCenter({ prompts, selectedPromptKey, onSelectPrompt }: {
  prompts: TextFile[]
  selectedPromptKey: string
  onSelectPrompt: (key: string, action?: OKRActionDefinition) => void
}) {
  const [schedules, setSchedules] = useState<ScheduledTask[]>([])
  const [lastTasks, setLastTasks] = useState<Record<number, Task>>({})
  const [manualTaskIds, setManualTaskIds] = useState<Partial<Record<OKRActionKey, number>>>({})
  const [manualTasks, setManualTasks] = useState<Partial<Record<OKRActionKey, Task>>>({})
  const [loading, setLoading] = useState(true)
  const [busyKey, setBusyKey] = useState<OKRActionKey>()
  const [editing, setEditing] = useState<OKRActionDefinition>()
  const [form, setForm] = useState<OKRActionFormValue>()
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
      const fixedSchedules = response.items.filter((item) => actionKeyForSchedule(item))
      setSchedules(fixedSchedules)
      const taskPairs = await Promise.all(fixedSchedules
        .filter((item) => item.last_task_id)
        .map(async (item) => [item.id, await getTask(item.last_task_id!, signal)] as const))
      setLastTasks(Object.fromEntries(taskPairs))
    } catch (cause) {
      if (!signal?.aborted) setNotice({ kind: 'error', text: `行动读取失败：${errorText(cause)}` })
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
        if (!controller.signal.aborted) setNotice({ kind: 'error', text: `手动行动状态读取失败：${errorText(cause)}` })
      }
    }
    void refresh()
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [manualTaskIds])

  const openEditor = (definition: OKRActionDefinition) => {
    setEditing(definition)
    setForm(formValueForAction(definition, scheduleByKey.get(definition.key)))
    setNotice(undefined)
  }

  const save = async () => {
    if (!editing || !form) return
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
      setEditing(undefined)
      setForm(undefined)
      setNotice({ kind: 'success', text: `${editing.title}已保存；执行内容以绑定 Prompt 为准。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `保存失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const toggle = async (definition: OKRActionDefinition, schedule: ScheduledTask) => {
    setBusyKey(definition.key)
    try {
      const value = { ...formValueForAction(definition, schedule), enabled: !schedule.enabled }
      await updateScheduledTask(schedule.id, scheduledTaskInput(definition, value))
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `更新失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const trigger = async (definition: OKRActionDefinition) => {
    setBusyKey(definition.key)
    onSelectPrompt(definition.promptKey, definition)
    try {
      const created = await createTask(manualTaskInput(definition))
      await executeTask(created.id)
      const task = await getTask(created.id)
      setManualTaskIds((current) => ({ ...current, [definition.key]: created.id }))
      setManualTasks((current) => ({ ...current, [definition.key]: task }))
      setNotice({ kind: 'success', text: `${definition.title}已启动手动 Agent Task #${created.id}。` })
    } catch (cause) {
      setNotice({ kind: 'error', text: `手动执行失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const remove = async (definition: OKRActionDefinition, schedule: ScheduledTask) => {
    if (!window.confirm(`删除“${definition.title}”的时间配置？固定行动和 Prompt 不会删除。`)) return
    setBusyKey(definition.key)
    try {
      await deleteScheduledTask(schedule.id)
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

  return (
    <div className="space-y-3">
      <section aria-label="自动化概览" className="flex min-h-14 items-center overflow-x-auto rounded-xl border border-slate-200 bg-white px-2 shadow-sm">
        {[
          { label: '业务 Prompt', value: String(prompts.length) },
          { label: '固定行动', value: '4' },
          { label: '已配置', value: `${configured}/4`, note: `${enabled} 启用` },
          { label: '下次行动', value: nextAction ? formatDateTime(nextAction.schedule.next_run_at) : '暂无' },
          { label: '需处理', value: String(attention) },
          { label: '执行方式', value: 'Prompt + 原子工具' },
        ].map((item) => <article key={item.label} className="flex shrink-0 items-baseline gap-2 border-r border-slate-100 px-3.5 last:border-r-0"><span className="text-[9px] text-slate-400">{item.label}</span><b className="text-[12px] font-semibold text-slate-800">{item.value}</b>{item.note && <span className="text-[9px] text-slate-400">{item.note}</span>}</article>)}
      </section>

      <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
        <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
          <div><h2 className="text-xs font-semibold text-slate-800">Agent 行动</h2><p className="mt-0.5 text-[10px] text-slate-400">行动固定；这里只管理何时运行。范围、对象、产出和验收全部写在绑定 Prompt 中。</p></div>
          <button type="button" disabled={loading} onClick={() => void load()} className="ml-auto rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 disabled:opacity-40">刷新</button>
        </div>

        {notice && <div className={`border-b px-4 py-2 text-[10px] ${notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700'}`}>{notice.text}</div>}

        {loading ? <div className="py-12 text-center text-xs text-slate-400">正在读取行动…</div> : <div className="divide-y divide-slate-100">{OKR_ACTIONS.map((definition) => {
          const schedule = scheduleByKey.get(definition.key)
          const lastTask = manualTasks[definition.key] ?? (schedule ? lastTasks[schedule.id] : undefined)
          const taskMeta = lastTask ? taskStatusMeta[lastTask.status] : undefined
          const prompt = promptByKey.get(definition.promptKey)
          const busy = busyKey === definition.key
          return <article key={definition.key} className={`grid items-center gap-3 px-4 py-3 lg:grid-cols-[minmax(250px,1.1fr)_minmax(190px,.8fr)_minmax(220px,1fr)_auto] ${selectedPromptKey === definition.promptKey ? 'bg-cyan-50/20' : ''}`}>
            <div className="flex min-w-0 items-start gap-3"><span className={`mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold ring-1 ${actionTones[definition.tone]}`}>{definition.shortLabel}</span><div className="min-w-0"><div className="flex flex-wrap items-center gap-1.5"><h3 className="text-[12px] font-semibold text-slate-800">{definition.title}</h3><span className={`rounded-full px-2 py-0.5 text-[9px] ${schedule?.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{schedule ? schedule.enabled ? '已启用' : '已停用' : '未配置'}</span></div><p className="mt-1 text-[10px] leading-4 text-slate-500">{definition.description}</p></div></div>
            <div className="text-[10px] leading-5 text-slate-500"><div><span className="text-slate-400">计划：</span><b className="font-medium text-slate-700">{actionScheduleText(definition, schedule)}</b></div><button type="button" onClick={() => onSelectPrompt(definition.promptKey, definition)} className="max-w-full truncate text-left text-cyan-700 hover:underline">Prompt · {prompt?.name ?? definition.promptKey}</button><div><span className="text-slate-400">下次：</span>{schedule?.enabled ? formatDateTime(schedule.next_run_at) : '—'}</div></div>
            <div className="min-w-0 text-[10px] leading-5 text-slate-500"><div className="flex items-center gap-1.5"><span className="text-slate-400">最近：</span>{taskMeta ? <span className={`rounded-full px-2 py-0.5 text-[9px] ${taskMeta.tone}`}>{taskMeta.label}</span> : <span>尚未执行</span>}</div><p className="line-clamp-2" title={schedule?.last_error_detail || taskSummary(lastTask)}>{schedule?.last_error_detail || taskSummary(lastTask)}</p></div>
            <div className="flex flex-wrap justify-end gap-1"><button type="button" onClick={() => onSelectPrompt(definition.promptKey, definition)} className="rounded-md border border-cyan-100 px-2 py-1 text-[9px] text-cyan-700">Prompt</button><button type="button" disabled={busy || schedule?.status === 'running'} onClick={() => openEditor(definition)} className="rounded-md border border-slate-200 px-2 py-1 text-[9px] text-slate-600 disabled:opacity-40">配置</button>{schedule && <><button type="button" disabled={busy || schedule.status === 'running'} onClick={() => void toggle(definition, schedule)} className="rounded-md border border-slate-200 px-2 py-1 text-[9px] text-slate-600 disabled:opacity-40">{schedule.enabled ? '停用' : '启用'}</button><button type="button" disabled={busy || schedule.status === 'running'} onClick={() => void remove(definition, schedule)} className="rounded-md px-2 py-1 text-[9px] text-red-500 disabled:opacity-40">删除配置</button></>}<button type="button" disabled={busy || ['pending', 'executing', 'waiting'].includes(manualTasks[definition.key]?.status ?? '')} onClick={() => void trigger(definition)} className="rounded-md bg-emerald-600 px-2 py-1 text-[9px] text-white hover:bg-emerald-700 disabled:opacity-40">手动执行</button></div>
          </article>
        })}</div>}
      </section>

      {editing && form && <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/30 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setEditing(undefined) }}><section role="dialog" aria-modal="true" aria-label="配置固定 Agent 行动" className="w-full max-w-lg rounded-2xl border border-slate-200 bg-white shadow-2xl"><div className="border-b border-slate-100 px-5 py-4"><h2 className="text-sm font-semibold text-slate-800">配置 · {editing.title}</h2><p className="mt-1 text-[10px] text-slate-400">绑定 {promptByKey.get(editing.promptKey)?.name ?? editing.promptKey}；这里只配置执行时间。</p></div><div className="grid gap-4 px-5 py-4 sm:grid-cols-2">{editing.cadence === 'weekly' ? <><label className="text-[10px] font-medium text-slate-500">每周执行日<select value={form.weekday} onChange={(event) => setForm({ ...form, weekday: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-[11px]">{WEEKDAYS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label><label className="text-[10px] font-medium text-slate-500">执行时间<input type="time" value={form.time} onChange={(event) => setForm({ ...form, time: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px]" /></label></> : <label className="text-[10px] font-medium text-slate-500 sm:col-span-2">巡检间隔（分钟）<input type="number" min={1} value={form.intervalMinutes} onChange={(event) => setForm({ ...form, intervalMinutes: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px]" /></label>}<label className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 px-3 py-2 text-[10px] text-slate-600 sm:col-span-2"><span><b className="block font-medium text-slate-700">启用行动</b><span className="text-[9px] text-slate-400">关闭时保留配置，也可以手动立即运行</span></span><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} className="size-4 accent-cyan-600" /></label></div><div className="flex justify-end gap-2 border-t border-slate-100 px-5 py-3"><button type="button" onClick={() => setEditing(undefined)} className="rounded-lg px-3 py-2 text-[11px] text-slate-500">取消</button><button type="button" disabled={busyKey === editing.key} onClick={() => void save()} className="rounded-lg bg-cyan-600 px-4 py-2 text-[11px] font-medium text-white disabled:opacity-40">{busyKey === editing.key ? '保存中…' : '保存配置'}</button></div></section></div>}
    </div>
  )
}
