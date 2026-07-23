import { useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Input, Modal, Space, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { approveTask, executeTask, finishTask, interruptTask, listTaskEvents, listTaskRuns, listTasks, reapplyTask, rejectTask, rerunTask, resumeTask, supplementTask } from './api'
import type { ExecutionRun, Task, TaskEvent, TaskStatus } from './types'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { taskStatusMeta as statusMeta } from './status'
import { usePageContext } from './pageContext'
import TaskDetailModal from './tasks/TaskDetailModal'
import {
  canReapply,
  externalActions,
  failureKindOf,
  failureMeta,
  proposalOf,
  strField,
} from './tasks/taskPresentation'

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

// CellText 把一段可能较长的可读文本按最多 3 行截断展示（详情抽屉里看全文），空则 '—'。
function CellText({ text, danger }: { text: string | null; danger?: boolean }) {
  if (!text) return <Text type="secondary">—</Text>
  return (
    <Text
      type={danger ? 'danger' : undefined}
      className="table-cell-clamp"
      title={text}
    >{text}</Text>
  )
}

type TaskTab = 'human' | 'awaiting' | 'done' | 'failed' | 'others'

const tabStatuses: Record<TaskTab, TaskStatus[]> = {
  human: ['needs_human'],
  awaiting: ['awaiting_approval'],
  done: ['done'],
  failed: ['failed'],
  others: ['pending', 'executing', 'waiting'],
}

const tabLabels: Record<TaskTab, string> = {
  human: '待我处理',
  awaiting: '审批中',
  done: '执行成功',
  failed: '执行失败',
  others: '其他',
}

export default function Tasks({ onDetailOpen }: { onDetailOpen?: () => void }) {
  const { setSelection } = usePageContext()
  const [activeTab, setActiveTab] = useState<TaskTab>('awaiting')
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
  const [reapplyingId, setReapplyingId] = useState<number>()
  const [rejectTarget, setRejectTarget] = useState<Task>()
  const [rejectReason, setRejectReason] = useState('')
  const [rerunSubmitting, setRerunSubmitting] = useState(false)
  const [approveTarget, setApproveTarget] = useState<Task>()
  const [approveNote, setApproveNote] = useState('')
  const [approveSubmitting, setApproveSubmitting] = useState(false)
  const [resumeTarget, setResumeTarget] = useState<Task>()
  const [resumeResponse, setResumeResponse] = useState('')
  const [resumeSubmitting, setResumeSubmitting] = useState(false)
  const [runs, setRuns] = useState<ExecutionRun[]>([])
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsError, setRunsError] = useState<string>()
  const [events, setEvents] = useState<TaskEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState<string>()

  useEffect(() => {
    if (!detail) {
      setSelection(null)
      return
    }
    setSelection({
      kind: 'task',
      id: detail.id,
      label: `Task #${detail.id} ${detail.title}`,
    })
  }, [detail, setSelection])

  useEffect(() => () => setSelection(null), [setSelection])

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    listTasks(statuses, 1, 100, controller.signal)
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [statuses, refreshKey])

  // 有任务在执行中时静默轮询列表，点完「执行」后状态会从执行中变为完成/失败，无需手动刷新。
  const hasExecuting = items.some((task) => task.status === 'executing')
  useEffect(() => {
    if (!hasExecuting) return
    const timer = window.setInterval(() => {
      listTasks(statuses, 1, 100)
        .then((result) => setItems(result.items))
        .catch(() => { /* 轮询失败不打扰，下次再试 */ })
    }, 3000)
    return () => window.clearInterval(timer)
  }, [hasExecuting, statuses])

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
    setDetail(task)
  }

  const closeDetail = () => setDetail(undefined)

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
    if (externalActions.has(task.action_type)) {
      const ok = window.confirm(`「${task.title}」是对外动作（${task.action_type}），执行会真实触达外部。确认由 codex 执行？`)
      if (!ok) return
    }
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
    const ok = window.confirm(`确认打断「${task.title}」？\n\n这会立即停止当前 Codex 进程并把任务记为“已打断”。已经完成的外部操作不会自动回滚。`)
    if (!ok) return
    setInterruptingId(task.id)
    setError(undefined)
    try {
      await interruptTask(task.id, task.version)
      setDetail(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
      setRefreshKey((value) => value + 1)
    } finally {
      setInterruptingId(undefined)
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
      setDetail(undefined)
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
      setDetail(undefined)
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
      setDetail(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const runReapply = async (task: Task) => {
    const ok = window.confirm(`「${task.title}」将用你此前已批准的同一方案再次真实落地（不再重新审批）。确认重试？`)
    if (!ok) return
    setReapplyingId(task.id)
    setError(undefined)
    try {
      await reapplyTask(task.id)
      markLocalExecuting(task.id)
      setDetail(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setReapplyingId(undefined)
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

  const taskColumn: TableColumnsType<Task>[number] = {
    title: '任务',
    dataIndex: 'title',
    width: 280,
    render: (_, task) => (
      <Space orientation="vertical" size={2} style={{ width: '100%' }}>
        <Text strong className="table-cell-clamp" title={task.title}>{task.title}</Text>
        <Text type="secondary" style={{ fontSize: 12 }}>
          {task.todo_id != null ? `Todo #${task.todo_id}` : `${task.source_type} #${task.source_id ?? '—'}`} · {task.action_type}
        </Text>
      </Space>
    ),
  }
  const statusColumn: TableColumnsType<Task>[number] = {
    title: '状态',
    dataIndex: 'status',
    width: 100,
    render: (_, task) => (
      <Space orientation="vertical" size={2}>
        <StatusBadge label={statusMeta[task.status].label} color={statusMeta[task.status].color} />
        <Text type="secondary" style={{ fontSize: 12 }}>{formatBriefTime(task.updated_at)}</Text>
      </Space>
    ),
  }

  // 待审批 Tab 特有列：动作（proposal.action）、待你拍板/后续（needs_followup）。
  const awaitingCols: TableColumnsType<Task> = [
    { title: '动作', width: 320, render: (_, task) => <CellText text={proposalOf(task)?.proposal.action ?? strField(task.execution_result, 'action')} /> },
    { title: '待你拍板 / 后续', width: 260, render: (_, task) => <CellText text={proposalOf(task)?.needs_followup ?? strField(task.execution_result, 'needs_followup')} /> },
  ]

  // 非审批 Tab 共用列：执行摘要（summary→error→尚未执行）、待你拍板/后续（needs_followup）。
  const resultCols: TableColumnsType<Task> = [
    {
      title: '执行摘要', width: 340, render: (_, task) => {
        const failure = failureKindOf(task)
        // failed 任务：先标来源，再展示驳回原因 / 摘要 / 错误详情。
        if (failure) {
          const reason = strField(task.execution_result, 'reject_reason')
          const summaryText = strField(task.execution_result, 'summary')
          const errText = strField(task.execution_result, 'error')
          const detail = reason ?? summaryText ?? errText
          return (
            <Space orientation="vertical" size={2} style={{ width: '100%' }}>
              <FailureTag task={task} />
              {detail ? <CellText text={detail} danger={failure === 'codex' || failure === 'stale'} /> : <Text type="secondary">—</Text>}
            </Space>
          )
        }
        const summaryText = strField(task.execution_result, 'summary')
        if (summaryText) return <CellText text={summaryText} />
        const errText = strField(task.execution_result, 'error')
        if (errText) return <CellText text={errText} danger />
        return <Text type="secondary">尚未执行</Text>
      },
    },
    { title: '待你拍板 / 后续', width: 260, render: (_, task) => <CellText text={strField(task.execution_result, 'needs_followup')} /> },
  ]

  const columns: TableColumnsType<Task> = [
    taskColumn,
    statusColumn,
    ...(activeTab === 'awaiting' ? awaitingCols : resultCols),
    {
      title: '操作', width: 200, render: (_, task) => {
        if (task.status === 'pending') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button type="primary" size="small" loading={executingId === task.id} onClick={(e) => { e.stopPropagation(); runExecute(task) }}>执行</Button>
            <Button size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'done') }}>手动完成</Button>
            <Button danger size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'failed') }}>失败</Button>
          </Space>
        }
        if (task.status === 'executing') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <StatusBadge label="codex 执行中…" color={statusMeta.executing.color} />
            <Button danger size="small" loading={interruptingId === task.id} onClick={(e) => { e.stopPropagation(); runInterrupt(task) }}>打断</Button>
          </Space>
        }
        if (task.status === 'waiting') {
          return <StatusBadge label="等待定时唤醒" color={statusMeta.waiting.color} />
        }
        if (task.status === 'needs_human') {
          return <Button type="primary" size="small" loading={resumeSubmitting && resumeTarget?.id === task.id} onClick={(e) => { e.stopPropagation(); openResume(task) }}>回复并继续</Button>
        }
        if (task.status === 'awaiting_approval') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button type="primary" size="small" loading={approveSubmitting && approveTarget?.id === task.id} onClick={(e) => { e.stopPropagation(); openApprove(task) }}>批准落地</Button>
            <Button danger size="small" onClick={(e) => { e.stopPropagation(); openReject(task) }}>驳回</Button>
          </Space>
        }
        if (task.status === 'done' || task.status === 'failed') {
          return <Space onClick={(e) => e.stopPropagation()}>
            {canReapply(task) && (
              <Button type="primary" size="small" loading={reapplyingId === task.id} onClick={(e) => { e.stopPropagation(); runReapply(task) }}>重试落地</Button>
            )}
            <Button size="small" onClick={(e) => { e.stopPropagation(); openRerun(task) }}>重跑</Button>
          </Space>
        }
        return '—'
      },
    },
  ]

  return <>
    <PageHeader title="任务执行" subtitle="已确认的可执行任务，点行查看方案与结果">
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </PageHeader>
    {error && <Alert type="error" showIcon title="Task 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Tabs
        activeKey={activeTab}
        onChange={(key) => setActiveTab(key as TaskTab)}
        items={(Object.keys(tabLabels) as TaskTab[]).map((key) => ({
          key,
          label: activeTab === key
            ? <Badge count={items.length} offset={[8, -2]} size="small" overflowCount={999}>{tabLabels[key]}</Badge>
            : tabLabels[key],
        }))}
      />
      <Table<Task> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 1160 }} tableLayout="fixed" onRow={(task) => ({ onClick: () => openDetail(task), className: 'clickable-row' })} />
    </Card>
    <TaskDetailModal
      task={detail}
      runs={runs}
      events={events}
      runsLoading={runsLoading}
      eventsLoading={eventsLoading}
      runsError={runsError}
      eventsError={eventsError}
      executing={detail ? executingId === detail.id : false}
      reapplying={detail ? reapplyingId === detail.id : false}
      approveSubmitting={detail ? approveSubmitting && approveTarget?.id === detail.id : false}
      resumeSubmitting={detail ? resumeSubmitting && resumeTarget?.id === detail.id : false}
      interrupting={detail ? interruptingId === detail.id : false}
      onClose={closeDetail}
      onExecute={runExecute}
      onApprove={openApprove}
      onReject={openReject}
      onRerun={openRerun}
      onReapply={runReapply}
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
          title="Codex 正在等待你的回应"
          description={resumeTarget ? strField(resumeTarget.execution_result, 'needs_followup') || '请确认或补充所需信息。' : undefined}
        />
        <Text type="secondary">提交后会继续原 Codex Session，不会重跑任务，也不会重新生成已批准产物。</Text>
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
        <Alert type="warning" showIcon title="对外写入将真正落地" description="批准后 codex 会按已审阅的方案真实写出/发送。可在下方追加落地时的补充指示（可不填）。" />
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
        {rerunTarget && externalActions.has(rerunTarget.action_type) && (
          <Alert type="warning" showIcon title={`对外动作（${rerunTarget.action_type}）`} description="重跑会再次真实触达外部，请确认后再提交。" />
        )}
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
