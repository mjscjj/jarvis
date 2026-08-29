import { useCallback, useEffect, useMemo, useState } from 'react'
import { createScheduledTask, deleteScheduledTask, listScheduledTasks, triggerScheduledTask, updateScheduledTask } from '../../../api'
import type { ScheduledTask, ScheduledTaskInput, ScheduledTaskScheduleType, TextFile } from '../../../types'

const WEEKDAYS = [
  { value: 1, label: '周一' },
  { value: 2, label: '周二' },
  { value: 3, label: '周三' },
  { value: 4, label: '周四' },
  { value: 5, label: '周五' },
  { value: 6, label: '周六' },
  { value: 7, label: '周日' },
]

interface ActionForm {
  title: string
  promptKey: string
  objective: string
  scope: string
  recipient: string
  scheduleType: ScheduledTaskScheduleType
  weekday: number
  time: string
  intervalMinutes: number
  runAt: string
  enabled: boolean
}

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

function contextString(task: ScheduledTask, key: string): string {
  const value = task.context_snapshot?.[key]
  return typeof value === 'string' ? value.trim() : ''
}

function isOKRAgentAction(task: ScheduledTask) {
  return task.dispatch_kind === 'create_task'
    && contextString(task, 'module') === 'okr'
    && contextString(task, 'skill') === 'okr-agent-orchestrator'
    && contextString(task, 'prompt_key').startsWith('okr_agent_')
}

function localDateTimeValue(value: Date): string {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}

function emptyForm(prompt?: TextFile): ActionForm {
  return {
    title: prompt ? prompt.name : '',
    promptKey: prompt?.key ?? '',
    objective: prompt ? `按“${prompt.name}”完成本次目标，并返回可核验结果。` : '',
    scope: '当前季度与当前有效周次；根据实时事实收敛范围',
    recipient: '结果保留在 Task；如需外部送达，以行动目标中明确的对象为准',
    scheduleType: 'weekly',
    weekday: 1,
    time: '09:00',
    intervalMinutes: 360,
    runAt: localDateTimeValue(new Date(Date.now() + 60 * 60 * 1000)),
    enabled: false,
  }
}

function formForTask(task: ScheduledTask): ActionForm {
  return {
    title: task.title,
    promptKey: contextString(task, 'prompt_key'),
    objective: task.instruction,
    scope: contextString(task, 'action_scope'),
    recipient: contextString(task, 'action_recipient'),
    scheduleType: task.schedule_type,
    weekday: task.weekday ?? 1,
    time: task.daily_time ?? '09:00',
    intervalMinutes: task.interval_minutes ?? 360,
    runAt: task.run_at ? localDateTimeValue(new Date(task.run_at)) : localDateTimeValue(new Date(Date.now() + 60 * 60 * 1000)),
    enabled: task.enabled,
  }
}

function inputForForm(form: ActionForm): ScheduledTaskInput {
  const title = form.title.trim()
  const objective = form.objective.trim()
  const scope = form.scope.trim()
  const recipient = form.recipient.trim()
  if (!title || !form.promptKey || !objective || !scope || !recipient) {
    throw new Error('名称、Prompt、行动目标、范围和对象都必须填写')
  }
  if (form.scheduleType === 'interval' && form.intervalMinutes <= 0) {
    throw new Error('执行间隔必须大于 0 分钟')
  }
  if (form.scheduleType === 'once' && !form.runAt) {
    throw new Error('请选择执行时间')
  }
  return {
    title,
    action_type: 'agent_task',
    instruction: objective,
    context_snapshot: {
      module: 'okr',
      skill: 'okr-agent-orchestrator',
      prompt_key: form.promptKey,
      action_scope: scope,
      action_recipient: recipient,
    },
    schedule_type: form.scheduleType,
    daily_time: form.scheduleType === 'daily' || form.scheduleType === 'weekly' ? form.time : null,
    weekday: form.scheduleType === 'weekly' ? form.weekday : null,
    interval_minutes: form.scheduleType === 'interval' ? form.intervalMinutes : null,
    run_at: form.scheduleType === 'once' ? new Date(form.runAt).toISOString() : null,
    enabled: form.enabled,
  }
}

function inputForTask(task: ScheduledTask, enabled: boolean): ScheduledTaskInput {
  return {
    title: task.title,
    action_type: 'agent_task',
    instruction: task.instruction,
    context_snapshot: task.context_snapshot ?? {},
    schedule_type: task.schedule_type,
    daily_time: task.daily_time,
    weekday: task.weekday,
    interval_minutes: task.interval_minutes,
    run_at: task.run_at,
    enabled,
  }
}

function scheduleText(task: ScheduledTask): string {
  if (task.schedule_type === 'once') return `执行一次 · ${formatDateTime(task.run_at)}`
  if (task.schedule_type === 'daily') return `每天 ${task.daily_time}`
  if (task.schedule_type === 'weekly') return `每${WEEKDAYS.find((item) => item.value === task.weekday)?.label ?? '周'} ${task.daily_time}`
  return `每 ${task.interval_minutes} 分钟`
}

function formatDateTime(value: string | null): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))
}

export function AgentActionCenter({ prompts, selectedPromptKey, onSelectPrompt }: {
  prompts: TextFile[]
  selectedPromptKey: string
  onSelectPrompt: (key: string) => void
}) {
  const [actions, setActions] = useState<ScheduledTask[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<number | 'new'>()
  const [editing, setEditing] = useState<ScheduledTask | 'new'>()
  const [form, setForm] = useState<ActionForm>(() => emptyForm())
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; text: string }>()
  const promptByKey = useMemo(() => new Map(prompts.map((item) => [item.key, item])), [prompts])

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const response = await listScheduledTasks('', signal)
      setActions(response.items.filter(isOKRAgentAction))
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

  const openCreate = () => {
    const prompt = promptByKey.get(selectedPromptKey) ?? prompts[0]
    setForm(emptyForm(prompt))
    setEditing('new')
  }

  const openEdit = (task: ScheduledTask) => {
    setForm(formForTask(task))
    setEditing(task)
  }

  const save = async () => {
    if (!editing) return
    const key = editing === 'new' ? 'new' : editing.id
    setBusy(key)
    try {
      const input = inputForForm(form)
      if (editing === 'new') await createScheduledTask(input)
      else await updateScheduledTask(editing.id, input)
      setEditing(undefined)
      setNotice({ kind: 'success', text: '行动已保存；Prompt 修改会在下一次触发时实时生效。' })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `行动保存失败：${errorText(cause)}` })
    } finally {
      setBusy(undefined)
    }
  }

  const toggle = async (task: ScheduledTask) => {
    setBusy(task.id)
    try {
      await updateScheduledTask(task.id, inputForTask(task, !task.enabled))
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `行动更新失败：${errorText(cause)}` })
    } finally {
      setBusy(undefined)
    }
  }

  const trigger = async (task: ScheduledTask) => {
    setBusy(task.id)
    try {
      await triggerScheduledTask(task.id)
      setNotice({ kind: 'success', text: `“${task.title}”已创建一次普通 Task。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `立即运行失败：${errorText(cause)}` })
    } finally {
      setBusy(undefined)
    }
  }

  const remove = async (task: ScheduledTask) => {
    if (!window.confirm(`删除行动“${task.title}”？`)) return
    setBusy(task.id)
    try {
      await deleteScheduledTask(task.id)
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `行动删除失败：${errorText(cause)}` })
    } finally {
      setBusy(undefined)
    }
  }

  return (
    <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
      <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-4 py-3">
        <div><h2 className="text-xs font-semibold text-slate-800">Agent 行动</h2><p className="mt-0.5 text-[10px] text-slate-400">Prompt 决定怎么做；行动绑定目标、范围、对象和触发时间。</p></div>
        <span className="ml-auto rounded-full bg-cyan-50 px-2 py-1 text-[9px] text-cyan-700">{actions.length} 个行动</span>
        <button type="button" disabled={!prompts.length} onClick={openCreate} className="rounded-md bg-cyan-600 px-3 py-1.5 text-[10px] font-medium text-white disabled:opacity-40">+ 新建行动</button>
        <button type="button" disabled={loading} onClick={() => void load()} className="rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 disabled:opacity-40">刷新</button>
      </div>

      {notice && <div className={`border-b px-4 py-2 text-[10px] ${notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700'}`}>{notice.text}</div>}

      {loading ? <div className="py-12 text-center text-xs text-slate-400">正在读取行动…</div> : actions.length === 0 ? (
        <div className="px-4 py-10 text-center"><div className="text-xs font-medium text-slate-600">还没有绑定 Prompt 的行动</div><p className="mt-1 text-[10px] text-slate-400">选择一个 Prompt 后创建行动，配置什么时候做、做什么范围、结果给谁。</p></div>
      ) : <div className="divide-y divide-slate-100">{actions.map((task) => {
        const promptKey = contextString(task, 'prompt_key')
        const prompt = promptByKey.get(promptKey)
        return <article key={task.id} className="grid gap-3 px-4 py-3 lg:grid-cols-[minmax(220px,1fr)_minmax(190px,.8fr)_minmax(220px,1fr)_auto] lg:items-center">
          <div className="min-w-0"><div className="flex flex-wrap items-center gap-1.5"><h3 className="truncate text-[11px] font-semibold text-slate-800">{task.title}</h3><span className={`rounded-full px-2 py-0.5 text-[8px] ${task.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{task.enabled ? '已启用' : '已停用'}</span></div><button type="button" onClick={() => onSelectPrompt(promptKey)} className="mt-1 truncate text-left text-[9px] text-cyan-700 hover:underline">Prompt · {prompt?.name ?? promptKey}</button><p className="mt-1 line-clamp-2 text-[10px] leading-4 text-slate-500">{task.instruction}</p></div>
          <div className="text-[10px] leading-5 text-slate-500"><div><span className="text-slate-400">何时：</span>{scheduleText(task)}</div><div><span className="text-slate-400">下次：</span>{task.enabled ? formatDateTime(task.next_run_at) : '已停用'}</div><div><span className="text-slate-400">最近：</span>{task.last_error_detail || task.last_result || '尚未执行'}</div></div>
          <div className="min-w-0 text-[10px] leading-5 text-slate-500"><div className="truncate" title={contextString(task, 'action_scope')}><span className="text-slate-400">范围：</span>{contextString(task, 'action_scope')}</div><div className="truncate" title={contextString(task, 'action_recipient')}><span className="text-slate-400">对象：</span>{contextString(task, 'action_recipient')}</div></div>
          <div className="flex flex-wrap justify-end gap-1"><button type="button" disabled={busy === task.id || task.status === 'running'} onClick={() => openEdit(task)} className="rounded-md border border-slate-200 px-2 py-1 text-[9px] text-slate-600 disabled:opacity-40">编辑</button><button type="button" disabled={busy === task.id || task.status === 'running'} onClick={() => void toggle(task)} className="rounded-md border border-slate-200 px-2 py-1 text-[9px] text-slate-600 disabled:opacity-40">{task.enabled ? '停用' : '启用'}</button><button type="button" disabled={busy === task.id || task.status === 'running'} onClick={() => void trigger(task)} className="rounded-md bg-slate-800 px-2 py-1 text-[9px] text-white disabled:opacity-40">立即运行</button><button type="button" disabled={busy === task.id || task.status === 'running'} onClick={() => void remove(task)} className="rounded-md px-2 py-1 text-[9px] text-red-500 disabled:opacity-40">删除</button></div>
        </article>
      })}</div>}

      {editing && <div className="fixed inset-0 z-[90] flex items-center justify-center bg-slate-950/30 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setEditing(undefined) }}>
        <section role="dialog" aria-modal="true" aria-label="配置 Agent 行动" className="max-h-[92vh] w-full max-w-2xl overflow-auto rounded-2xl border border-slate-200 bg-white shadow-2xl">
          <div className="border-b border-slate-100 px-5 py-4"><h2 className="text-sm font-semibold text-slate-800">{editing === 'new' ? '新建 Agent 行动' : '编辑 Agent 行动'}</h2><p className="mt-1 text-[10px] text-slate-400">行动不保存 workflow 或审批策略；Prompt 与 M5 在执行时做判断。</p></div>
          <div className="grid gap-4 px-5 py-4 sm:grid-cols-2">
            <label className="text-[10px] font-medium text-slate-500">行动名称<input value={form.title} onChange={(event) => setForm({ ...form, title: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px] outline-none focus:border-cyan-400" /></label>
            <label className="text-[10px] font-medium text-slate-500">绑定 Prompt<select value={form.promptKey} onChange={(event) => setForm({ ...form, promptKey: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-[11px] outline-none focus:border-cyan-400">{prompts.map((item) => <option key={item.key} value={item.key}>{item.name}</option>)}</select></label>
            <label className="text-[10px] font-medium text-slate-500 sm:col-span-2">本次行动目标<textarea value={form.objective} onChange={(event) => setForm({ ...form, objective: event.target.value })} rows={3} className="mt-1.5 w-full rounded-lg border border-slate-200 px-3 py-2 text-[11px] leading-5 outline-none focus:border-cyan-400" /></label>
            <label className="text-[10px] font-medium text-slate-500">行动范围<textarea value={form.scope} onChange={(event) => setForm({ ...form, scope: event.target.value })} rows={3} placeholder="例如：2026-Q4、SEA 区域、未闭环 KR" className="mt-1.5 w-full rounded-lg border border-slate-200 px-3 py-2 text-[11px] leading-5 outline-none focus:border-cyan-400" /></label>
            <label className="text-[10px] font-medium text-slate-500">结果对象 / 落点<textarea value={form.recipient} onChange={(event) => setForm({ ...form, recipient: event.target.value })} rows={3} placeholder="例如：仅 Task 结果；或指定负责人、群、文档" className="mt-1.5 w-full rounded-lg border border-slate-200 px-3 py-2 text-[11px] leading-5 outline-none focus:border-cyan-400" /></label>
            <label className="text-[10px] font-medium text-slate-500">触发方式<select value={form.scheduleType} onChange={(event) => setForm({ ...form, scheduleType: event.target.value as ScheduledTaskScheduleType })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-[11px] outline-none focus:border-cyan-400"><option value="once">指定时间执行一次</option><option value="weekly">每周指定时间</option><option value="daily">每天指定时间</option><option value="interval">每隔 N 分钟</option></select></label>
            {form.scheduleType === 'weekly' && <div className="grid grid-cols-2 gap-2"><label className="text-[10px] font-medium text-slate-500">执行日<select value={form.weekday} onChange={(event) => setForm({ ...form, weekday: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-2 text-[11px]">{WEEKDAYS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}</select></label><label className="text-[10px] font-medium text-slate-500">时间<input type="time" value={form.time} onChange={(event) => setForm({ ...form, time: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-2 text-[11px]" /></label></div>}
            {form.scheduleType === 'daily' && <label className="text-[10px] font-medium text-slate-500">每天执行时间<input type="time" value={form.time} onChange={(event) => setForm({ ...form, time: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px]" /></label>}
            {form.scheduleType === 'interval' && <label className="text-[10px] font-medium text-slate-500">执行间隔（分钟）<input type="number" min={1} value={form.intervalMinutes} onChange={(event) => setForm({ ...form, intervalMinutes: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px]" /></label>}
            {form.scheduleType === 'once' && <label className="text-[10px] font-medium text-slate-500">执行时间<input type="datetime-local" value={form.runAt} onChange={(event) => setForm({ ...form, runAt: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px]" /></label>}
            <label className="flex items-center justify-between gap-3 rounded-lg border border-slate-200 px-3 py-2 text-[10px] text-slate-600 sm:col-span-2"><span><b className="block font-medium text-slate-700">启用行动</b><span className="text-[9px] text-slate-400">关闭时仍可保存，并可手动立即运行</span></span><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} className="size-4 accent-cyan-600" /></label>
          </div>
          <div className="flex justify-end gap-2 border-t border-slate-100 px-5 py-3"><button type="button" onClick={() => setEditing(undefined)} className="rounded-lg px-3 py-2 text-[11px] text-slate-500">取消</button><button type="button" disabled={busy !== undefined} onClick={() => void save()} className="rounded-lg bg-cyan-600 px-4 py-2 text-[11px] font-medium text-white disabled:opacity-40">{busy !== undefined ? '保存中…' : '保存行动'}</button></div>
        </section>
      </div>}
    </section>
  )
}
