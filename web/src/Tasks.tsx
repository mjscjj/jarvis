import { useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Form, Input, message, Modal, Select, Space, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { approveTask, createTask, executeTask, finishTask, getTask, interruptTask, listProjects, listTaskEvents, listTaskRuns, listTasks, recallEffectMessage, rejectTask, rerunTask, resumeTask, supplementTask } from './api'
import type { ExecutionRun, Project, Task, TaskEvent, TaskStatus } from './types'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { taskStatusMeta as statusMeta } from './status'
import { usePageContext } from './pageContext'
import TaskDetailModal from './tasks/TaskDetailModal'
import {
  failureKindOf,
  failureMeta,
  strField,
  taskConclusion,
  taskConclusionLabel,
  taskHandlerMeta,
  taskProjectName,
  taskSourceName,
} from './tasks/taskPresentation'
import './styles/workbench.css'

const { Text } = Typography
const taskActionModalZIndex = 1100

// 列表状态旁的时间：月日时分，例如「7/22 21:25」。
function formatBriefTime(value: string | null | undefined): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

// FailureTag 在列表/详情里给 failed 任务标注来源；非 failed 返回 null。
function FailureTag({ task }: { task: Task }) {
  const kind = failureKindOf(task)
  if (!kind) return null
  const meta = failureMeta[kind]
  return <Tag color={meta.color}>{meta.label}</Tag>
}

function HandlerTag({ task }: { task: Task }) {
  const meta = taskHandlerMeta(task)
  if (!meta) return null
  return <Tag color={meta.color} title={meta.detail}>{meta.label}</Tag>
}

type TaskTab = 'needs_me' | 'running' | 'waiting' | 'completed' | 'failed'

const tabStatuses: Record<TaskTab, TaskStatus[]> = {
  needs_me: ['needs_human', 'awaiting_approval'],
  running: ['pending', 'executing'],
  waiting: ['waiting'],
  completed: ['done', 'observing'],
  failed: ['failed'],
}

const tabLabels: Record<TaskTab, string> = {
  needs_me: '需要我',
  running: 'Jarvis 处理中',
  waiting: '等待外部',
  completed: '已完成',
  failed: '异常',
}

function taskTab(value: string | undefined): TaskTab {
  return value && value in tabStatuses ? value as TaskTab : 'needs_me'
}

function positivePage(value: string | undefined): number {
  const page = Number(value)
  return Number.isInteger(page) && page > 0 ? page : 1
}

interface CreateTaskFields {
  title: string
  instruction: string
  project_id?: number
}

export default function Tasks({ onDetailOpen }: { onDetailOpen?: () => void }) {
  const { context, setSelection, setViewState } = usePageContext()
  const [activeTab, setActiveTab] = useState<TaskTab>(() => taskTab(context.view_state.view))
  const [page, setPage] = useState(() => positivePage(context.view_state.page))
  const [total, setTotal] = useState(0)
  const statuses = useMemo<TaskStatus[]>(() => tabStatuses[activeTab], [activeTab])
  const [items, setItems] = useState<Task[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshKey, setRefreshKey] = useState(0)
  const [detail, setDetail] = useState<Task>()
  const [selected, setSelected] = useState<Task>()
  const [finishStatus, setFinishStatus] = useState<'done' | 'failed'>('done')
  const [summary, setSummary] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [executingId, setExecutingId] = useState<number>()
  const [interruptingId, setInterruptingId] = useState<number>()
  const [rerunTarget, setRerunTarget] = useState<Task>()
  const [rerunNote, setRerunNote] = useState('')
  const [rejectTarget, setRejectTarget] = useState<Task>()
  const [rejectReason, setRejectReason] = useState('')
  const [rerunSubmitting, setRerunSubmitting] = useState(false)
  const [approveTarget, setApproveTarget] = useState<Task>()
  const [approveNote, setApproveNote] = useState('')
  const [approveSubmitting, setApproveSubmitting] = useState(false)
  const [resumeTarget, setResumeTarget] = useState<Task>()
  const [resumeResponse, setResumeResponse] = useState('')
  const [resumeSubmitting, setResumeSubmitting] = useState(false)
  const [recallingMessageID, setRecallingMessageID] = useState<string>()
  const [recallError, setRecallError] = useState<string>()
  const [runs, setRuns] = useState<ExecutionRun[]>([])
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsError, setRunsError] = useState<string>()
  const [events, setEvents] = useState<TaskEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState<string>()
  const [createOpen, setCreateOpen] = useState(false)
  const [createSubmitting, setCreateSubmitting] = useState(false)
  const [createError, setCreateError] = useState<string>()
  const [projects, setProjects] = useState<Project[]>([])
  const [projectsLoading, setProjectsLoading] = useState(false)
  const [projectsError, setProjectsError] = useState<string>()
  const [createForm] = Form.useForm<CreateTaskFields>()

  const routedTaskID = context.active_key === 'tasks' && context.selection?.kind === 'task'
    ? context.selection.id
    : null

  useEffect(() => {
    const controller = new AbortController()
    setProjectsLoading(true)
    listProjects(1, 100, controller.signal)
      .then((result) => {
        setProjects(result.items)
        setProjectsError(undefined)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setProjectsError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setProjectsLoading(false) })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    setActiveTab(taskTab(context.view_state.view))
    setPage(positivePage(context.view_state.page))
  }, [context.view_state.page, context.view_state.view])

  useEffect(() => {
    if (routedTaskID === null) {
      setDetail(undefined)
      return
    }
    if (detail?.id === routedTaskID) return
    const controller = new AbortController()
    getTask(routedTaskID, controller.signal)
      .then((task) => {
        setDetail(task)
        setRecallError(undefined)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
    return () => controller.abort()
  }, [detail?.id, routedTaskID])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    listTasks(statuses, page, 20, controller.signal)
      .then((result) => { setItems(result.items); setTotal(result.total); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [statuses, page, refreshKey])

  // 有任务在执行中时静默轮询列表，点完「执行」后状态会从执行中变为完成/失败，无需手动刷新。
  const hasExecuting = items.some((task) => task.status === 'executing')
  useEffect(() => {
    if (!hasExecuting) return
    const timer = window.setInterval(() => {
      listTasks(statuses, page, 20)
        .then((result) => { setItems(result.items); setTotal(result.total) })
        .catch(() => { /* 轮询失败不打扰，下次再试 */ })
    }, 3000)
    return () => window.clearInterval(timer)
  }, [hasExecuting, page, statuses])

  // 打开详情抽屉时拉该 Task 的执行历史。detail 关闭（undefined）时清空。
  useEffect(() => {
    if (!detail) { setRuns([]); setRunsError(undefined); return }
    const controller = new AbortController()
    setRunsLoading(true)
    setRunsError(undefined)
    listTaskRuns(detail.id, controller.signal)
      .then((result) => setRuns(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setRunsError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setRunsLoading(false) })
    return () => controller.abort()
  }, [detail, refreshKey])

  const openDetail = (task: Task) => {
    onDetailOpen?.()
    setRecallError(undefined)
    setDetail(task)
    setSelection({
      kind: 'task',
      id: task.id,
      label: `Task #${task.id} ${task.title}`,
    })
  }

  const closeDetail = () => {
    setRecallError(undefined)
    setDetail(undefined)
    setSelection(null)
  }

  useEffect(() => {
    if (!detail) { setEvents([]); setEventsError(undefined); return }
    const controller = new AbortController()
    setEventsLoading(true)
    setEventsError(undefined)
    listTaskEvents(detail.id, controller.signal)
      .then((result) => setEvents(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setEventsError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setEventsLoading(false) })
    return () => controller.abort()
  }, [detail, refreshKey])

  const openFinish = (task: Task, status: 'done' | 'failed') => {
    setSelected(task)
    setFinishStatus(status)
    setSummary('')
  }

  const openCreate = () => {
    createForm.resetFields()
    setCreateError(undefined)
    setCreateOpen(true)
  }

  const submitCreate = async () => {
    const values = await createForm.validateFields()
    const title = values.title.trim()
    const instruction = values.instruction.trim()
    setCreateSubmitting(true)
    setCreateError(undefined)
    try {
      await createTask({
        title,
        action_type: 'agent_task',
        target: instruction,
        background: {},
        source_payload: instruction,
        ...(values.project_id === undefined ? {} : { project_id: values.project_id }),
      })
      setCreateOpen(false)
      createForm.resetFields()
      setActiveTab('running')
      setPage(1)
      setViewState({ view: 'running', page: 1 })
      setRefreshKey((value) => value + 1)
      message.success('任务已创建，Jarvis 正在处理')
    } catch (cause: unknown) {
      setCreateError(errorText(cause))
    } finally {
      setCreateSubmitting(false)
    }
  }

  const submit = async () => {
    if (!selected || !summary.trim()) { setError('执行结果不能为空'); return }
    setSubmitting(true)
    try {
      const result = finishStatus === 'done' ? { summary: summary.trim() } : { error: summary.trim() }
      await finishTask(selected.id, selected.version, finishStatus, result)
      setSelected(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  const markLocalExecuting = (taskID: number) => {
    setItems((prev) => prev.map((task) => (
      task.id === taskID ? { ...task, status: 'executing' as TaskStatus } : task
    )))
  }

  const runExecute = async (task: Task) => {
    setExecutingId(task.id)
    setError(undefined)
    try {
      await executeTask(task.id)
      markLocalExecuting(task.id)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const runInterrupt = async (task: Task) => {
    const ok = window.confirm(`确认打断「${task.title}」？\n\n这会立即停止当前执行进程并把任务记为“已打断”。已经完成的外部操作不会自动回滚。`)
    if (!ok) return
    setInterruptingId(task.id)
    setError(undefined)
    try {
      await interruptTask(task.id, task.version)
      closeDetail()
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
      setRefreshKey((value) => value + 1)
    } finally {
      setInterruptingId(undefined)
    }
  }

  // 撤回任务已发出的某条飞书消息：真实调用飞书撤回，成功后后端把「已撤回」标记写回
  // 该 effect，这里用返回的任务刷新详情，并重拉执行历史让 run 里的同一条也更新。
  const runRecallMessage = async (task: Task, messageID: string) => {
    const ok = window.confirm(`确认撤回这条飞书消息（${messageID}）？\n\n对方会看到「消息已撤回」，撤回后无法恢复。`)
    if (!ok) return
    setRecallingMessageID(messageID)
    setRecallError(undefined)
    try {
      const updated = await recallEffectMessage(task.id, messageID)
      setDetail(updated)
      setItems((prev) => prev.map((item) => (item.id === updated.id ? updated : item)))
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setRecallError(errorText(cause))
    } finally {
      setRecallingMessageID(undefined)
    }
  }

  const openApprove = (task: Task) => {
    setApproveTarget(task)
    setApproveNote('')
  }

  const submitApprove = async () => {
    if (!approveTarget) return
    const task = approveTarget
    setApproveSubmitting(true)
    setError(undefined)
    try {
      let version = task.version
      const note = approveNote.trim()
      if (note) {
        const updated = await supplementTask(task.id, task.version, note)
        version = updated.version
      }
      await approveTask(task.id, version)
      markLocalExecuting(task.id)
      setApproveTarget(undefined)
      closeDetail()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setApproveSubmitting(false)
    }
  }

  const openReject = (task: Task) => {
    setRejectTarget(task)
    setRejectReason('')
  }

  const openResume = (task: Task) => {
    setResumeTarget(task)
    setResumeResponse('')
  }

  const submitResume = async () => {
    if (!resumeTarget || !resumeResponse.trim()) return
    const task = resumeTarget
    setResumeSubmitting(true)
    setError(undefined)
    try {
      await resumeTask(task.id, task.version, resumeResponse.trim())
      markLocalExecuting(task.id)
      setResumeTarget(undefined)
      closeDetail()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setResumeSubmitting(false)
    }
  }

  const submitReject = async () => {
    if (!rejectTarget) return
    const task = rejectTarget
    setExecutingId(task.id)
    setError(undefined)
    try {
      await rejectTask(task.id, task.version, rejectReason.trim())
      setRejectTarget(undefined)
      closeDetail()
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const openRerun = (task: Task) => {
    setRerunTarget(task)
    setRerunNote('')
  }

  const submitRerun = async () => {
    if (!rerunTarget) return
    const task = rerunTarget
    setRerunSubmitting(true)
    setError(undefined)
    try {
      const note = rerunNote.trim()
      if (note) await supplementTask(task.id, task.version, note)
      await rerunTask(task.id)
      markLocalExecuting(task.id)
      setRerunTarget(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setRerunSubmitting(false)
    }
  }

  const columns: TableColumnsType<Task> = [
    {
      title: '工作事项',
      dataIndex: 'title',
      render: (_, task) => (
        <div className="workbench-task-main">
          <div className="workbench-task-title-row">
            <Text strong className="workbench-task-title" title={task.title}>{task.title}</Text>
            {task.status === 'observing' && <Tag variant="filled">无需行动</Tag>}
            <FailureTag task={task} />
            <HandlerTag task={task} />
          </div>
          <div className="workbench-task-conclusion">
            <span>{taskConclusionLabel(task)}</span>
            <Text title={taskConclusion(task)}>{taskConclusion(task)}</Text>
          </div>
          <Text type="secondary" className="workbench-task-origin">
            {taskProjectName(task)} · {taskSourceName(task)}
          </Text>
        </div>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 126,
      render: (_, task) => (
        <Space orientation="vertical" size={4}>
          <StatusBadge label={statusMeta[task.status].label} color={statusMeta[task.status].color} />
          <Text type="secondary" className="workbench-task-updated">{formatBriefTime(task.updated_at)} 更新</Text>
        </Space>
      ),
    },
    {
      title: '', width: 184, render: (_, task) => {
        if (task.status === 'pending') {
          return <Space size={6} wrap onClick={(e) => e.stopPropagation()}>
            <Button type="primary" size="small" loading={executingId === task.id} onClick={(e) => { e.stopPropagation(); runExecute(task) }}>执行</Button>
            <Button size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'done') }}>手动完成</Button>
            <Button danger size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'failed') }}>失败</Button>
          </Space>
        }
        if (task.status === 'executing') {
          return <Button size="small" onClick={(e) => { e.stopPropagation(); openDetail(task) }}>查看进展</Button>
        }
        if (task.status === 'waiting') {
          return <Button size="small" onClick={(e) => { e.stopPropagation(); openDetail(task) }}>查看等待</Button>
        }
        if (task.status === 'needs_human') {
          return <Button type="primary" size="small" loading={resumeSubmitting && resumeTarget?.id === task.id} onClick={(e) => { e.stopPropagation(); openResume(task) }}>回复并继续</Button>
        }
        if (task.status === 'awaiting_approval') {
          return <Button type="primary" size="small" onClick={(e) => { e.stopPropagation(); openDetail(task) }}>审阅</Button>
        }
        if (task.status === 'done' || task.status === 'failed') {
          return <Button size="small" onClick={(e) => { e.stopPropagation(); openDetail(task) }}>{task.status === 'done' ? '查看结果' : '查看原因'}</Button>
        }
        return <Button size="small" onClick={(e) => { e.stopPropagation(); openDetail(task) }}>查看结论</Button>
      },
    },
  ]

  return <>
    <PageHeader title="任务" subtitle="先处理需要你决定的事项，再查看 Jarvis 的推进、等待和历史结果">
      <Button type="primary" onClick={openCreate}>新建任务</Button>
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </PageHeader>
    {error && <Alert type="error" showIcon title="Task 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Tabs
        activeKey={activeTab}
        onChange={(key) => {
          const next = key as TaskTab
          setActiveTab(next)
          setPage(1)
          setViewState({ view: next, page: 1 })
        }}
        items={(Object.keys(tabLabels) as TaskTab[]).map((key) => ({
          key,
          label: activeTab === key
            ? <Badge count={total} offset={[8, -2]} size="small" overflowCount={999}>{tabLabels[key]}</Badge>
            : tabLabels[key],
        }))}
      />
      <Table<Task>
        className="workbench-task-table"
        rowKey="id"
        columns={columns}
        dataSource={items}
        loading={loading}
        pagination={{
          current: page,
          pageSize: 20,
          total,
          showSizeChanger: false,
          hideOnSinglePage: true,
          onChange: (nextPage) => {
            setPage(nextPage)
            setViewState({ view: activeTab, page: nextPage })
          },
        }}
        tableLayout="fixed"
        locale={{ emptyText: activeTab === 'needs_me' ? '暂时没有需要你处理的事项' : '这个分组暂无任务' }}
        onRow={(task) => ({
          onClick: () => openDetail(task),
          onKeyDown: (event) => {
            if (event.key === 'Enter' || event.key === ' ') {
              event.preventDefault()
              openDetail(task)
            }
          },
          tabIndex: 0,
          role: 'button',
          className: 'clickable-row',
        })}
      />
    </Card>
    <Modal
      zIndex={taskActionModalZIndex}
      title="新建任务"
      open={createOpen}
      confirmLoading={createSubmitting}
      onOk={submitCreate}
      onCancel={() => setCreateOpen(false)}
      okText="创建并执行"
      cancelButtonProps={{ disabled: createSubmitting }}
      maskClosable={!createSubmitting}
    >
      <Space orientation="vertical" size={12} style={{ width: '100%' }}>
        <Alert type="info" showIcon title="创建后立即交给 Jarvis 执行；需要审批的外部操作仍会等待你确认。" />
        {createError && <Alert type="error" showIcon title="任务创建失败" description={createError} />}
        <Form form={createForm} layout="vertical" requiredMark={false}>
          <Form.Item name="title" label="任务名称" rules={[{ required: true, whitespace: true, message: '请输入任务名称' }]}>
            <Input autoFocus placeholder="例如：检查 FactEngine 最近失败原因" />
          </Form.Item>
          <Form.Item name="instruction" label="任务要求" rules={[{ required: true, whitespace: true, message: '请输入完整任务要求' }]}>
            <Input.TextArea rows={6} placeholder="说清楚希望 Jarvis 完成什么；相关背景和验收要求也可以直接写在这里。" />
          </Form.Item>
          <Form.Item
            name="project_id"
            label="所属项目（可选）"
            extra={projectsError ? `项目加载失败：${projectsError}` : '选择后会把该项目的当前世界背景带给执行 Agent。'}
          >
            <Select
              allowClear
              showSearch
              loading={projectsLoading}
              placeholder="不选择则由 Jarvis 根据任务内容判断"
              optionFilterProp="label"
              options={projects.map((project) => ({ value: project.id, label: project.name }))}
            />
          </Form.Item>
        </Form>
      </Space>
    </Modal>
    <TaskDetailModal
      task={detail}
      runs={runs}
      events={events}
      runsLoading={runsLoading}
      eventsLoading={eventsLoading}
      runsError={runsError}
      eventsError={eventsError}
      executing={detail ? executingId === detail.id : false}
      approveSubmitting={detail ? approveSubmitting && approveTarget?.id === detail.id : false}
      resumeSubmitting={detail ? resumeSubmitting && resumeTarget?.id === detail.id : false}
      interrupting={detail ? interruptingId === detail.id : false}
      recallingMessageID={recallingMessageID}
      recallError={recallError}
      onRecallMessage={runRecallMessage}
      onClose={closeDetail}
      onExecute={runExecute}
      onApprove={openApprove}
      onReject={openReject}
      onRerun={openRerun}
      onResume={openResume}
      onInterrupt={runInterrupt}
    />
    <Modal zIndex={taskActionModalZIndex} title={finishStatus === 'done' ? '记录完成结果' : '记录失败原因'} open={Boolean(selected)} confirmLoading={submitting} onOk={submit} onCancel={() => setSelected(undefined)} okText="提交">
      <Input.TextArea rows={5} value={summary} onChange={(event) => setSummary(event.target.value)} placeholder={finishStatus === 'done' ? '完成了什么、产物在哪里' : '失败原因和需要的后续处理'} />
    </Modal>
    <Modal
      zIndex={taskActionModalZIndex}
      title={resumeTarget ? `回复并继续「${resumeTarget.title}」` : '回复并继续'}
      open={Boolean(resumeTarget)}
      confirmLoading={resumeSubmitting}
      onOk={submitResume}
      onCancel={() => setResumeTarget(undefined)}
      okText="提交并恢复原 Session"
    >
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        <Alert
          type="warning"
          showIcon
          title="Jarvis 正在等待你的回应"
          description={resumeTarget ? strField(resumeTarget.execution_result, 'needs_followup') || '请确认或补充所需信息。' : undefined}
        />
        <Text type="secondary">提交后会继续原执行会话，不会重跑任务，也不会重新生成已批准产物。</Text>
        <Input.TextArea rows={4} value={resumeResponse} onChange={(event) => setResumeResponse(event.target.value)} placeholder="确认操作，或补充 Agent 请求的信息" />
      </Space>
    </Modal>
    <Modal
      zIndex={taskActionModalZIndex}
      title={approveTarget ? `批准落地「${approveTarget.title}」` : '批准落地'}
      open={Boolean(approveTarget)}
      confirmLoading={approveSubmitting}
      onOk={submitApprove}
      onCancel={() => setApproveTarget(undefined)}
      okText="确认批准并落地"
    >
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        <Alert type="warning" showIcon title="对外写入将真正落地" description="批准后 Jarvis 会按已审阅的方案真实写出或发送。可在下方追加落地时的补充指示（可不填）。" />
        <Text type="secondary">可选填写补充信息/指示；留空则直接按已批准方案落地。填写后会持久保存到执行阶段补充，落地与之后重跑都会带上。</Text>
        <Input.TextArea rows={4} value={approveNote} onChange={(event) => setApproveNote(event.target.value)} placeholder="例如：标题加上【紧急】；抄送给 B；文档先放草稿区不要直接发公告等（可不填）" />
      </Space>
    </Modal>
    <Modal
      zIndex={taskActionModalZIndex}
      title={rerunTarget ? `重跑「${rerunTarget.title}」` : '重跑任务'}
      open={Boolean(rerunTarget)}
      confirmLoading={rerunSubmitting}
      onOk={submitRerun}
      onCancel={() => setRerunTarget(undefined)}
      okText="确认重跑"
    >
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        <Text type="secondary">可选填写补充信息/指示；留空则直接重跑。填写后会持久保存，之后每次重跑都会带上。</Text>
        <Input.TextArea rows={4} value={rerunNote} onChange={(event) => setRerunNote(event.target.value)} placeholder="例如：这次改用 xxx 文档模板；标题要包含季度；只发给 A 不要发给 B 等（可不填）" />
      </Space>
    </Modal>
    <Modal
      zIndex={taskActionModalZIndex}
      title={rejectTarget ? `驳回「${rejectTarget.title}」的方案` : '驳回方案'}
      open={Boolean(rejectTarget)}
      confirmLoading={Boolean(rejectTarget) && executingId === rejectTarget?.id}
      onOk={submitReject}
      onCancel={() => setRejectTarget(undefined)}
      okText="确认驳回"
      okButtonProps={{ danger: true }}
    >
      <Space orientation="vertical" size={8} style={{ width: '100%' }}>
        <Text type="secondary">驳回后任务将标记为失败，不会真正写出任何内容。可填写驳回原因（可不填）；之后可重跑重新产出方案。</Text>
        <Input.TextArea rows={4} value={rejectReason} onChange={(event) => setRejectReason(event.target.value)} placeholder="例如：措辞不合适 / 目标群选错了 / 内容还需补充数据（可不填）" />
      </Space>
    </Modal>
  </>
}
