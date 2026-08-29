import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  createScheduledTask,
  deleteScheduledTask,
  getTask,
  listTextFiles,
  listScheduledTasks,
  triggerScheduledTask,
  updateTextFile,
  updateScheduledTask,
} from '../../../api'
import { usePageContext } from '../../../pageContext'
import type { ScheduledTask, Task, TextFile } from '../../../types'
import MarkdownReport from '../../../components/MarkdownReport'
import {
  OKR_ACTIONS,
  WEEKDAYS,
  actionKeyForSchedule,
  actionScheduleText,
  formValueForAction,
  nextActionRunAt,
  scheduledTaskInput,
  type OKRActionDefinition,
  type OKRActionFormValue,
  type OKRActionKey,
} from '../actionConfig'
import { getReminderBatches, getReminderPreview } from '../api'
import { useBoard } from '../board'
import type { ReminderBatch, ReminderPreview } from '../types'

const taskStatusMeta: Record<Task['status'], { label: string; tone: string }> = {
  pending: { label: '等待执行', tone: 'bg-slate-100 text-slate-600' },
  executing: { label: '执行中', tone: 'bg-blue-50 text-blue-700' },
  waiting: { label: '等待继续', tone: 'bg-amber-50 text-amber-700' },
  needs_human: { label: '需要处理', tone: 'bg-red-50 text-red-700' },
  awaiting_approval: { label: '等待审批', tone: 'bg-violet-50 text-violet-700' },
  done: { label: '已完成', tone: 'bg-emerald-50 text-emerald-700' },
  failed: { label: '失败', tone: 'bg-red-50 text-red-700' },
  observing: { label: '已检查', tone: 'bg-slate-100 text-slate-600' },
}

const approvalLabels: Record<OKRActionDefinition['approval'], string> = {
  automatic: '自动执行',
  draft_only: '仅生成草稿',
  review_then_send: '审核后发送',
  require_approval: '提交前审批',
}

function errorText(cause: unknown): string {
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

function actionTone(definition: OKRActionDefinition): string {
  if (definition.effect === 'external_write') return 'bg-amber-50 text-amber-700 ring-amber-100'
  if (definition.effect === 'read_only') return 'bg-blue-50 text-blue-700 ring-blue-100'
  return 'bg-emerald-50 text-emerald-700 ring-emerald-100'
}

export function ActionCenter() {
  const { quarter, week } = useBoard()
  const { navigate } = usePageContext()
  const [schedules, setSchedules] = useState<ScheduledTask[]>([])
  const [lastTasks, setLastTasks] = useState<Record<number, Task>>({})
  const [reminderPreview, setReminderPreview] = useState<ReminderPreview>()
  const [reminderBatches, setReminderBatches] = useState<ReminderBatch[]>([])
  const [loading, setLoading] = useState(true)
  const [busyKey, setBusyKey] = useState<OKRActionKey>()
  const [notice, setNotice] = useState<{ kind: 'success' | 'error'; text: string }>()
  const [editing, setEditing] = useState<OKRActionDefinition>()
  const [form, setForm] = useState<OKRActionFormValue>()
  const [detailKey, setDetailKey] = useState<OKRActionKey>()
  const [workflowFiles, setWorkflowFiles] = useState<TextFile[]>([])
  const [workflowDrafts, setWorkflowDrafts] = useState<Record<string, string>>({})
  const [workflowOpen, setWorkflowOpen] = useState(false)
  const [workflowKey, setWorkflowKey] = useState('')
  const [workflowLoading, setWorkflowLoading] = useState(false)
  const [savingWorkflowKey, setSavingWorkflowKey] = useState<string>()

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
      const moduleSchedules = response.items.filter((item) => item.dispatch_kind === 'create_task' && (
        item.context_snapshot.module === 'weekly-report' || actionKeyForSchedule(item)
      ))
      setSchedules(moduleSchedules)
      const taskPairs = await Promise.all(moduleSchedules
        .filter((item) => item.last_task_id)
        .map(async (item) => [item.id, await getTask(item.last_task_id!, signal)] as const))
      setLastTasks(Object.fromEntries(taskPairs))
    } catch (cause) {
      if (!signal?.aborted) setNotice({ kind: 'error', text: `动作读取失败：${errorText(cause)}` })
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  const loadWorkflowFiles = useCallback(async (signal?: AbortSignal) => {
    setWorkflowLoading(true)
    try {
      const response = await listTextFiles(signal)
      const items = response.items.filter((item) => item.stage === 'weekly_report')
      setWorkflowFiles(items)
      setWorkflowDrafts(Object.fromEntries(items.map((item) => [item.key, item.content])))
      setWorkflowKey((current) => current && items.some((item) => item.key === current) ? current : items[0]?.key || '')
    } catch (cause) {
      if (!signal?.aborted) setNotice({ kind: 'error', text: `流程配置读取失败：${errorText(cause)}` })
    } finally {
      if (!signal?.aborted) setWorkflowLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void loadWorkflowFiles(controller.signal)
    return () => controller.abort()
  }, [loadWorkflowFiles])

  useEffect(() => {
    if (!quarter || !week) return
    let active = true
    Promise.all([getReminderPreview(quarter, week), getReminderBatches(quarter, week)])
      .then(([preview, batches]) => {
        if (!active) return
        setReminderPreview(preview)
        setReminderBatches(batches.batches)
      })
      .catch(() => undefined)
    return () => { active = false }
  }, [quarter, week])

  useEffect(() => {
    const hasActiveRun = schedules.some((item) => item.status === 'running') || Object.values(lastTasks).some((task) => ['pending', 'executing', 'waiting'].includes(task.status))
    if (!hasActiveRun) return
    const timer = window.setInterval(() => void load(), 3500)
    return () => window.clearInterval(timer)
  }, [lastTasks, load, schedules])

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
    if (editing.key === 'publish_report' && form.enabled && !form.target.trim()) {
      setNotice({ kind: 'error', text: '启用对外提交前必须填写目标群、文档目录或接收人。' })
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
      setNotice({ kind: 'success', text: `${editing.title}已保存。${form.enabled ? '下一次将按计划执行。' : '当前保持停用。'}` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `保存失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const toggle = async (definition: OKRActionDefinition, schedule: ScheduledTask) => {
    const nextForm = { ...formValueForAction(definition, schedule), enabled: !schedule.enabled }
    if (definition.key === 'publish_report' && nextForm.enabled && !nextForm.target.trim()) {
      setNotice({ kind: 'error', text: '启用对外提交前必须先配置提交目标。' })
      openEditor(definition)
      return
    }
    setBusyKey(definition.key)
    try {
      await updateScheduledTask(schedule.id, scheduledTaskInput(definition, nextForm))
      setNotice({ kind: 'success', text: `${definition.title}已${schedule.enabled ? '停用' : '启用'}。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `更新失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const trigger = async (definition: OKRActionDefinition, schedule: ScheduledTask) => {
    setBusyKey(definition.key)
    try {
      await triggerScheduledTask(schedule.id)
      setNotice({ kind: 'success', text: `${definition.title}已提交为一次 Task；页面会继续读取真实执行结果。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `立即运行失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const remove = async (definition: OKRActionDefinition, schedule: ScheduledTask) => {
    if (!window.confirm(`删除“${definition.title}”的动作配置？业务数据不会删除。`)) return
    setBusyKey(definition.key)
    try {
      await deleteScheduledTask(schedule.id)
      setDetailKey(undefined)
      setNotice({ kind: 'success', text: `${definition.title}配置已删除。` })
      await load()
    } catch (cause) {
      setNotice({ kind: 'error', text: `删除失败：${errorText(cause)}` })
    } finally {
      setBusyKey(undefined)
    }
  }

  const saveWorkflow = async () => {
    const content = workflowDrafts[workflowKey]?.trim() || ''
    if (!content) {
      setNotice({ kind: 'error', text: 'Markdown 配置不能为空。' })
      return
    }
    setSavingWorkflowKey(workflowKey)
    try {
      const updated = await updateTextFile(workflowKey, { content })
      setWorkflowFiles((current) => current.map((item) => item.key === updated.key ? updated : item))
      setWorkflowDrafts((current) => ({ ...current, [updated.key]: updated.content }))
      setNotice({ kind: 'success', text: `${updated.name}已保存，后续动作会实时读取。` })
    } catch (cause) {
      setNotice({ kind: 'error', text: `流程配置保存失败：${errorText(cause)}` })
    } finally {
      setSavingWorkflowKey(undefined)
    }
  }

  const configured = OKR_ACTIONS.filter((item) => scheduleByKey.has(item.key)).length
  const enabled = OKR_ACTIONS.filter((item) => scheduleByKey.get(item.key)?.enabled).length
  const attention = OKR_ACTIONS.filter((item) => {
    const schedule = scheduleByKey.get(item.key)
    const task = schedule ? lastTasks[schedule.id] : undefined
    return Boolean(schedule?.last_error_detail || task && ['failed', 'needs_human', 'awaiting_approval'].includes(task.status))
  }).length
  const nextAction = OKR_ACTIONS
    .map((definition) => ({ definition, schedule: scheduleByKey.get(definition.key) }))
    .filter((item): item is { definition: OKRActionDefinition; schedule: ScheduledTask } => Boolean(item.schedule?.enabled && item.schedule.status !== 'completed'))
    .sort((left, right) => nextActionRunAt(left.definition, left.schedule).localeCompare(nextActionRunAt(right.definition, right.schedule)))[0]

  return (
    <div className="space-y-3">
      <section className="grid gap-2 sm:grid-cols-4">
        {[
          { label: '已配置', value: `${configured}/4`, note: '业务动作' },
          { label: '运行中', value: String(enabled), note: '已启用计划' },
          { label: '下次动作', value: nextAction ? formatDateTime(nextActionRunAt(nextAction.definition, nextAction.schedule)) : '暂无', note: nextAction?.definition.title || '尚未启用' },
          { label: '需要处理', value: String(attention), note: '失败、人工或审批' },
        ].map((item) => (
          <article key={item.label} className="rounded-xl border border-slate-200 bg-white px-3.5 py-3 shadow-[0_1px_2px_rgba(15,23,42,0.03)]">
            <div className="text-[10px] font-medium text-slate-400">{item.label}</div>
            <div className="mt-1 text-lg font-semibold tracking-tight text-slate-800">{item.value}</div>
            <div className="mt-0.5 truncate text-[10px] text-slate-400">{item.note}</div>
          </article>
        ))}
      </section>

      <section className="overflow-hidden rounded-xl border border-slate-200 bg-white shadow-[0_1px_2px_rgba(15,23,42,0.04)]">
        <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
          <div>
            <h2 className="text-xs font-semibold text-slate-800">OKR 动作</h2>
            <p className="mt-0.5 text-[10px] text-slate-400">业务配置保存在通用 ScheduledTask；每次触发仍是一条独立 Task。</p>
          </div>
          <div className="flex items-center gap-2 text-[10px] text-slate-400">
            <span>{quarter || '当前季度'} · {week || '当前周'}</span>
            <button type="button" onClick={() => setWorkflowOpen(true)} className="rounded-md border border-blue-200 bg-blue-50 px-2.5 py-1 font-medium text-blue-700 hover:bg-blue-100">流程与模板</button>
            <button type="button" onClick={() => navigate('scheduled-tasks')} className="rounded-md border border-slate-200 px-2.5 py-1 text-slate-500 hover:border-blue-200 hover:text-blue-600">通用自动化</button>
            <button type="button" disabled={loading} onClick={() => void load()} className="rounded-md border border-slate-200 px-2.5 py-1 text-slate-500 hover:border-blue-200 hover:text-blue-600 disabled:opacity-50">刷新</button>
          </div>
        </div>

        {notice && <div className={`border-b px-4 py-2 text-[11px] ${notice.kind === 'success' ? 'border-emerald-100 bg-emerald-50 text-emerald-700' : 'border-red-100 bg-red-50 text-red-700'}`}>{notice.text}</div>}

        <div className="divide-y divide-slate-100">
          {OKR_ACTIONS.map((definition) => {
            const schedule = scheduleByKey.get(definition.key)
            const lastTask = schedule ? lastTasks[schedule.id] : undefined
            const taskMeta = lastTask ? taskStatusMeta[lastTask.status] : undefined
            const isBusy = busyKey === definition.key
            const expanded = detailKey === definition.key
            return (
              <article key={definition.key} className="px-4 py-3">
                <div className="grid items-start gap-3 lg:grid-cols-[minmax(250px,1.15fr)_minmax(180px,.8fr)_minmax(190px,.9fr)_auto]">
                  <div className="flex min-w-0 items-start gap-3">
                    <span className={`mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg text-[11px] font-semibold ring-1 ${actionTone(definition)}`}>{definition.shortLabel}</span>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <h3 className="text-[12px] font-semibold text-slate-800">{definition.title}</h3>
                        {!schedule && <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[9px] text-slate-500">未配置</span>}
                        {schedule && <span className={`rounded-full px-2 py-0.5 text-[9px] font-medium ${schedule.enabled ? 'bg-emerald-50 text-emerald-700' : 'bg-slate-100 text-slate-500'}`}>{schedule.enabled ? '已启用' : '已停用'}</span>}
                        <span className="rounded-full bg-violet-50 px-2 py-0.5 text-[9px] text-violet-600">{approvalLabels[definition.approval]}</span>
                      </div>
                      <p className="mt-1 text-[10px] leading-4 text-slate-500">{definition.description}</p>
                    </div>
                  </div>

                  <div className="text-[10px] leading-5 text-slate-500">
                    <div><span className="text-slate-400">计划：</span><b className="font-medium text-slate-700">{actionScheduleText(definition, schedule)}</b></div>
                    <div className="truncate" title={definition.scope}><span className="text-slate-400">范围：</span>{definition.scope}</div>
                    {definition.key === 'remind_missing' && reminderPreview && <div className="text-amber-700">当前 {reminderPreview.summary.needsReminderOwnerCount} 人待提醒 · 缺 {reminderPreview.summary.missingCount} 条</div>}
                  </div>

                  <div className="min-w-0 text-[10px] leading-5 text-slate-500">
                    <div className="flex items-center gap-1.5">
                      <span className="text-slate-400">最近：</span>
                      {taskMeta ? <span className={`rounded-full px-2 py-0.5 text-[9px] font-medium ${taskMeta.tone}`}>{taskMeta.label}</span> : <span>尚未执行</span>}
                      {schedule?.last_started_at && <span className="text-slate-400">{formatDateTime(schedule.last_started_at)}</span>}
                    </div>
                    <div className={`truncate ${schedule?.last_error_detail || lastTask?.status === 'failed' ? 'text-red-600' : ''}`} title={schedule?.last_error_detail || taskSummary(lastTask)}>{schedule?.last_error_detail || taskSummary(lastTask)}</div>
                    {schedule?.enabled && <div className="text-slate-400">下次 {formatDateTime(nextActionRunAt(definition, schedule))}</div>}
                  </div>

                  <div className="flex flex-wrap justify-end gap-1.5">
                    <button type="button" disabled={isBusy} onClick={() => openEditor(definition)} className="rounded-md border border-slate-200 px-2.5 py-1 text-[10px] text-slate-600 hover:border-blue-200 hover:text-blue-600 disabled:opacity-40">{schedule ? '编辑' : '配置'}</button>
                    {schedule && <button type="button" disabled={isBusy || schedule.status === 'running'} onClick={() => void toggle(definition, schedule)} className="rounded-md border border-slate-200 px-2.5 py-1 text-[10px] text-slate-600 hover:border-blue-200 hover:text-blue-600 disabled:opacity-40">{schedule.enabled ? '停用' : '启用'}</button>}
                    {schedule && <button type="button" disabled={isBusy || schedule.status === 'running'} onClick={() => void trigger(definition, schedule)} className="rounded-md bg-slate-800 px-2.5 py-1 text-[10px] font-medium text-white hover:bg-slate-700 disabled:opacity-40">立即运行</button>}
                    {schedule && <button type="button" onClick={() => setDetailKey(expanded ? undefined : definition.key)} className="rounded-md px-2 py-1 text-[10px] text-slate-400 hover:bg-slate-50 hover:text-slate-600">{expanded ? '收起' : '详情'}</button>}
                  </div>
                </div>

                {expanded && schedule && (
                  <div className="mt-3 grid gap-3 rounded-lg border border-slate-100 bg-slate-50/70 p-3 text-[10px] text-slate-500 sm:grid-cols-3">
                    <div><div className="mb-1 font-medium text-slate-400">动作对象</div><div className="leading-5 text-slate-700">{definition.recipientRule}</div><div className="leading-5">{definition.scope}</div></div>
                    <div><div className="mb-1 font-medium text-slate-400">真实执行结果</div><div className="leading-5 text-slate-700">{taskSummary(lastTask)}</div>{lastTask && <div className="mt-1">Task #{lastTask.id} · {taskStatusMeta[lastTask.status].label}</div>}{definition.key === 'remind_missing' && reminderBatches[0] && <div className="mt-1">最近批次：{reminderBatches[0].recipientCount} 人 · 缺 {reminderBatches[0].missingCount} 条</div>}</div>
                    <div><div className="mb-1 font-medium text-slate-400">安全边界</div><div className="leading-5">{definition.approval === 'automatic' ? '仅允许只读取数和内部事实更新。' : definition.approval === 'draft_only' ? '只生成内部草稿，不创建文档、不发消息。' : '外部写入前必须经过审批或已配置的发送策略。'}</div><button type="button" disabled={isBusy || schedule.status === 'running'} onClick={() => void remove(definition, schedule)} className="mt-2 text-red-500 hover:underline disabled:opacity-40">删除动作配置</button></div>
                  </div>
                )}
              </article>
            )
          })}
        </div>
      </section>

      <p className="px-1 text-[10px] leading-5 text-slate-400">“周一/周二”由 Skill 按 Asia/Shanghai 做日期门禁；底层继续复用每日调度，暂不新增第二套 weekly cron。外部发送和提交模板默认保持停用。</p>

      {editing && form && (
        <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-950/25 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setEditing(undefined) }}>
          <section role="dialog" aria-modal="true" aria-label={`配置${editing.title}`} className="w-full max-w-lg overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
            <div className="border-b border-slate-100 px-5 py-4">
              <h2 className="text-sm font-semibold text-slate-800">配置 · {editing.title}</h2>
              <p className="mt-1 text-[10px] leading-4 text-slate-400">这里只配置业务参数；执行逻辑来自 {editing.skill} Skill。</p>
            </div>
            <div className="space-y-4 px-5 py-4">
              {editing.cadence === 'weekly' ? (
                <div className="grid grid-cols-2 gap-3">
                  <label className="text-[10px] font-medium text-slate-500">执行日
                    <select value={form.weekday} onChange={(event) => setForm({ ...form, weekday: Number(event.target.value) })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 bg-white px-3 text-[11px] text-slate-700 outline-none focus:border-blue-400">
                      {WEEKDAYS.map((item) => <option key={item.value} value={item.value}>{item.label}</option>)}
                    </select>
                  </label>
                  <label className="text-[10px] font-medium text-slate-500">执行时间
                    <input type="time" value={form.time} onChange={(event) => setForm({ ...form, time: event.target.value })} className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px] text-slate-700 outline-none focus:border-blue-400" />
                  </label>
                </div>
              ) : (
                <label className="block text-[10px] font-medium text-slate-500">巡检间隔
                  <div className="mt-1.5 flex items-center gap-2"><input type="number" min={1} value={form.intervalMinutes} onChange={(event) => setForm({ ...form, intervalMinutes: Number(event.target.value) })} className="h-9 min-w-0 flex-1 rounded-lg border border-slate-200 px-3 text-[11px] text-slate-700 outline-none focus:border-blue-400" /><span className="text-[10px] text-slate-400">分钟</span></div>
                </label>
              )}
              {editing.key === 'publish_report' && (
                <label className="block text-[10px] font-medium text-slate-500">提交目标
                  <input value={form.target} onChange={(event) => setForm({ ...form, target: event.target.value })} placeholder="例如：管理群、open_chat_id 或指定文档目录" className="mt-1.5 h-9 w-full rounded-lg border border-slate-200 px-3 text-[11px] text-slate-700 outline-none focus:border-blue-400" />
                </label>
              )}
              <div className="rounded-lg border border-slate-100 bg-slate-50 p-3 text-[10px] leading-5 text-slate-500"><div><b className="font-medium text-slate-700">范围：</b>{editing.scope}</div><div><b className="font-medium text-slate-700">对象：</b>{editing.recipientRule}</div><div><b className="font-medium text-slate-700">策略：</b>{approvalLabels[editing.approval]}</div></div>
              <label className="flex items-center justify-between gap-4 rounded-lg border border-slate-200 px-3 py-2.5 text-[11px] text-slate-600"><span><b className="block font-medium text-slate-700">启用计划</b><span className="text-[9px] text-slate-400">关闭时仍可保存配置并手动运行</span></span><input type="checkbox" checked={form.enabled} onChange={(event) => setForm({ ...form, enabled: event.target.checked })} className="size-4 accent-blue-600" /></label>
              {editing.effect === 'external_write' && <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-[10px] leading-5 text-amber-700">这是外部动作。保存不会立刻发送；只有启用后到达计划时间或点击“立即运行”才会创建 Task，并继续遵守审批策略。</div>}
            </div>
            <div className="flex justify-end gap-2 border-t border-slate-100 px-5 py-3">
              <button type="button" onClick={() => setEditing(undefined)} className="rounded-lg px-3 py-2 text-[11px] text-slate-500 hover:bg-slate-50">取消</button>
              <button type="button" disabled={busyKey === editing.key} onClick={() => void save()} className="rounded-lg bg-blue-600 px-4 py-2 text-[11px] font-medium text-white hover:bg-blue-700 disabled:opacity-50">{busyKey === editing.key ? '保存中…' : '保存配置'}</button>
            </div>
          </section>
        </div>
      )}

      {workflowOpen && (
        <div className="fixed inset-0 z-[85] flex items-center justify-center bg-slate-950/30 p-4" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) setWorkflowOpen(false) }}>
          <section role="dialog" aria-modal="true" aria-label="管理周报流程与模板" className="flex max-h-[88vh] w-full max-w-6xl flex-col overflow-hidden rounded-2xl border border-slate-200 bg-white shadow-2xl">
            <div className="flex items-start justify-between gap-4 border-b border-slate-100 px-5 py-4">
              <div>
                <h2 className="text-sm font-semibold text-slate-800">周报流程与 Markdown 模板</h2>
                <p className="mt-1 text-[10px] leading-4 text-slate-400">文件是唯一真源；保存后，后续催填、巡检和材料动作会实时读取。调度、审批、身份和幂等仍由系统强制保证。</p>
              </div>
              <button type="button" onClick={() => setWorkflowOpen(false)} className="rounded-lg px-2 py-1 text-lg leading-none text-slate-400 hover:bg-slate-100 hover:text-slate-600" aria-label="关闭">×</button>
            </div>

            {workflowLoading ? <div className="flex min-h-80 items-center justify-center text-xs text-slate-400">正在读取流程配置…</div> : (
              <div className="grid min-h-0 flex-1 md:grid-cols-[220px_minmax(0,1fr)]">
                <nav className="border-b border-slate-100 bg-slate-50/70 p-3 md:border-b-0 md:border-r">
                  <div className="mb-2 px-2 text-[9px] font-semibold uppercase tracking-[.12em] text-slate-400">可管理内容</div>
                  <div className="grid gap-1 sm:grid-cols-2 md:grid-cols-1">
                    {workflowFiles.map((item) => {
                      const dirty = workflowDrafts[item.key] !== item.content
                      return <button key={item.key} type="button" onClick={() => setWorkflowKey(item.key)} className={`rounded-lg px-3 py-2.5 text-left transition ${workflowKey === item.key ? 'bg-white text-blue-700 shadow-sm ring-1 ring-blue-100' : 'text-slate-600 hover:bg-white/80'}`}>
                        <span className="flex items-center gap-2 text-[11px] font-medium"><span className={`size-1.5 rounded-full ${dirty ? 'bg-amber-400' : 'bg-slate-300'}`} />{item.name}</span>
                        <span className="mt-1 block line-clamp-2 text-[9px] leading-4 text-slate-400">{item.description}</span>
                      </button>
                    })}
                  </div>
                </nav>

                {(() => {
                  const item = workflowFiles.find((candidate) => candidate.key === workflowKey)
                  if (!item) return <div className="flex min-h-80 items-center justify-center text-xs text-slate-400">配置文件未注册或读取失败</div>
                  const draft = workflowDrafts[workflowKey] ?? ''
                  const dirty = draft !== item.content
                  return <div className="flex min-h-0 flex-col">
                    <div className="flex flex-wrap items-center justify-between gap-2 border-b border-slate-100 px-4 py-3">
                      <div className="min-w-0">
                        <div className="flex items-center gap-2"><h3 className="text-xs font-semibold text-slate-700">{item.name}</h3>{dirty && <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[9px] text-amber-700">未保存</span>}</div>
                        <div className="mt-1 truncate text-[9px] text-slate-400" title={item.path}>{item.path}</div>
                      </div>
                      <div className="flex gap-2">
                        <button type="button" disabled={!dirty} onClick={() => setWorkflowDrafts((current) => ({ ...current, [workflowKey]: item.content }))} className="rounded-md border border-slate-200 px-2.5 py-1.5 text-[10px] text-slate-500 hover:bg-slate-50 disabled:opacity-40">撤销未保存</button>
                        <button type="button" disabled={!dirty || savingWorkflowKey === workflowKey || !draft.trim()} onClick={() => void saveWorkflow()} className="rounded-md bg-blue-600 px-3 py-1.5 text-[10px] font-medium text-white hover:bg-blue-700 disabled:opacity-40">{savingWorkflowKey === workflowKey ? '保存中…' : '保存并生效'}</button>
                      </div>
                    </div>
                    <div className="grid min-h-0 flex-1 lg:grid-cols-2">
                      <div className="flex min-h-0 flex-col border-b border-slate-100 p-4 lg:border-b-0 lg:border-r">
                        <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.12em] text-slate-400">Markdown 编辑</div>
                        <textarea value={draft} onChange={(event) => setWorkflowDrafts((current) => ({ ...current, [workflowKey]: event.target.value }))} spellCheck={false} className="min-h-72 flex-1 resize-none rounded-xl border border-slate-200 bg-slate-50/50 p-3 font-mono text-[11px] leading-5 text-slate-700 outline-none focus:border-blue-300 focus:bg-white" />
                      </div>
                      <div className="min-h-0 overflow-auto p-4">
                        <div className="mb-2 text-[9px] font-semibold uppercase tracking-[.12em] text-slate-400">预览</div>
                        <MarkdownReport className="daily-digest-markdown" content={draft || '暂无内容'} />
                      </div>
                    </div>
                  </div>
                })()}
              </div>
            )}
          </section>
        </div>
      )}
    </div>
  )
}
