import { FrozenContextPanel } from '../slots'
import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Empty,
  Modal,
  Pagination,
  Space,
  Spin,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import {
  ApiOutlined,
  BulbOutlined,
  CalendarOutlined,
  CheckCircleOutlined,
  CloseOutlined,
  CommentOutlined,
  ExclamationCircleOutlined,
  FileTextOutlined,
  HistoryOutlined,
  LinkOutlined,
  MessageOutlined,
  PaperClipOutlined,
  PullRequestOutlined,
  SafetyOutlined,
  ToolOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import type { ExecutionRun, Task, TaskEvent } from '../types'
import { getTaskRun } from '../api'
import StatusBadge from '../components/StatusBadge'
import { useAgentIdentity } from '../agentIdentity'
import { actionLabels, taskStatusMeta as statusMeta } from '../status'
import TaskRunOutputPanel from './TaskRunOutputPanel'
import { EnrichmentBlock } from './TaskEnrichments'
import { EffectCard, EffectsCard } from './TaskEffects'
import type { EffectRecall } from './TaskEffects'
import { effectItems, enrichmentItems, errorText, formatTime, stringValue } from './taskValues'
import {
  failureKindOf,
  failureMeta,
  isAgentClosure,
  modelCloseReason,
  objectField,
  questionOf,
  questionText,
  strField,
  taskHandlerMeta,
  taskProjectName,
  taskSourceName,
} from './taskPresentation'

const { Link, Paragraph, Text, Title } = Typography

const taskEventLabels: Record<string, string> = {
  created: '任务已创建',
  execution_started: '开始执行',
  rerun_requested: '请求重跑',
  updated: '主动维护',
  // Retired with the approval stage; kept so历史事件仍读得懂。
  approval_requested: '等待审批',
  approval_granted: '已批准执行',
  approval_rejected: '已驳回',
  reapply_started: '重新落地',
  human_input_requested: '等待我的回应',
  human_response_received: '已回复并继续',
  resumed: '恢复原 Session',
  supplemented: '我的补充',
  execution_succeeded: '执行成功',
  execution_failed: '执行失败',
  execution_observing: '查完，无需动手',
  execution_interrupted: '执行已打断',
  feishu_message_recalled: '撤回飞书消息',
  stale_failed: '执行超时',
  stale_requeued: '执行中断，重新排队',
  closed: '主动收口',
}

const actorLabels: Record<string, string> = {
  user: '我',
  m5: 'M5',
  proactive: '主动 Agent',
  scheduled_task: '定时恢复器',
  system: '系统',
  seed: '初始化',
  migration: '迁移',
}



interface TaskDetailModalProps {
  task?: Task
  runs: ExecutionRun[]
  runsPage: number
  runsTotal: number
  onRunsPageChange: (page: number) => void
  events: TaskEvent[]
  runsLoading: boolean
  eventsLoading: boolean
  runsError?: string
  eventsError?: string
  executing: boolean
  resumeSubmitting: boolean
  interrupting: boolean
  recallingMessageID?: string
  recallError?: string
  onRecallMessage: (task: Task, messageID: string) => void
  onClose: () => void
  onExecute: (task: Task) => void
  onRerun: (task: Task) => void
  onResume: (task: Task) => void
  onInterrupt: (task: Task) => void
}

type HistoryItem =
  | { key: string; at: string; kind: 'event'; event: TaskEvent; run?: ExecutionRun }
  | { key: string; at: string; kind: 'supplement'; note: string; scope: string }
  | { key: string; at: string; kind: 'run'; run: ExecutionRun }

function formatDuration(ms: number | null): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}





function formatShortTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function runStatusColor(status: string): string {
  if (status === 'succeeded') return 'var(--color-success)'
  if (status === 'failed') return 'var(--color-error)'
  if (status === 'waiting' || status === 'needs_human') return 'var(--color-warning)'
  if (status === 'running') return 'var(--color-info)'
  return 'var(--color-text-tertiary)'
}

function taskEventColor(event: TaskEvent): string {
  if (event.actor_type === 'user' || event.event_type === 'supplemented') return 'var(--color-warning)'
  if (event.to_status === 'done') return 'var(--color-success)'
  if (event.to_status === 'failed') return 'var(--color-error)'
  if (event.to_status === 'waiting' || event.to_status === 'needs_human') return 'var(--color-warning)'
  if (event.to_status === 'executing') return 'var(--color-info)'
  return 'var(--color-text-tertiary)'
}

function RunDetails({ runID, index, recall }: { runID: number; index?: ExecutionRun; recall: EffectRecall }) {
  const [open, setOpen] = useState(false)
  const [run, setRun] = useState<ExecutionRun>()
  const [error, setError] = useState<string>()
  const [retry, setRetry] = useState(0)
  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    setRun(undefined)
    setError(undefined)
    getTaskRun(runID, controller.signal)
      .then((value) => { if (!controller.signal.aborted) setRun(value) })
      .catch((cause: unknown) => { if (!controller.signal.aborted) setError(errorText(cause)) })
    return () => controller.abort()
  }, [open, runID, index?.status, index?.finished_at, retry])
  return (
    <details className="task-run-details" open={open} onToggle={(event) => {
      if (event.target === event.currentTarget) setOpen(event.currentTarget.open)
    }}>
      <summary>
        <span className="task-run-summary-main">
          {index && <span className="task-history-dot" style={{ background: runStatusColor(index.status) }} />}
          Run #{runID}{index ? ` · ${index.status}` : ''}
        </span>
        {index && <Text type="secondary">{formatDuration(index.duration_ms)}</Text>}
      </summary>
      {open && (error
        ? <Alert type="error" title="执行记录加载失败" description={error} action={<Button onClick={() => setRetry((value) => value + 1)}>重试</Button>} />
        : run ? <RunContent run={run} recall={recall} /> : <Spin />)}
    </details>
  )
}

function RunContent({ run, recall }: { run: ExecutionRun; recall: EffectRecall }) {
  const enrichments = run.output?.enrichments ?? []
  const runEffects = effectItems(run.effects ?? run.output?.effects)
  const question = run.output?.question
  return (
    <div className="task-run-body">
      <Space size={8} wrap>
        <Tag>{actionLabels[run.action_type] || run.action_type}</Tag>
        <Text type="secondary">沙箱 {run.sandbox}</Text>
        {run.codex_session_id && <Text type="secondary">session {run.codex_session_id.slice(0, 12)}…</Text>}
        {run.repo_path && (
          <Text type="secondary" className="mono">
            {run.repo_path}
          </Text>
        )}
      </Space>
      {run.summary && <Paragraph className="task-readable-text">{run.summary}</Paragraph>}
      {enrichments.length > 0 && (
        <div className="task-enrichment-list">
          {enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
        </div>
      )}
      {runEffects.length > 0 && (
        <div className="task-effect-list task-run-effect-list">
          {runEffects.map((effect, index) => <EffectCard key={index} effect={effect} recall={recall} />)}
        </div>
      )}
      {question?.title && (
        <Alert type="info" showIcon title="向我提出的问题" description={[question.title, question.body].filter(Boolean).join('\n\n')} />
      )}
      {run.error_detail && <Alert type="error" showIcon title="执行错误" description={<Text className="mono">{run.error_detail}</Text>} />}
      {run.output && Object.keys(run.output).length > 0 && (
        <details className="task-raw-details">
          <summary>Codex 原始输出</summary>
          <pre className="inline-json">{JSON.stringify(run.output, null, 2)}</pre>
        </details>
      )}
    </div>
  )
}

function InlineCodeText({ text }: { text: string }) {
  return <>{text.split(/(`[^`]+`)/g).filter(Boolean).map((part, index) => (
    part.startsWith('`') && part.endsWith('`')
      ? <code key={index}>{part.slice(1, -1)}</code>
      : <span key={index}>{part}</span>
  ))}</>
}

function ResultContent({ task, actions }: { task: Task; actions: ReactNode }) {
  const { name: agentName } = useAgentIdentity()
  const result = task.execution_result
  const summary = task.summary?.trim() || strField(result, 'summary')
  const error = strField(result, 'error')
  const question = questionText(task)
  const enrichments = enrichmentItems(result?.enrichments)
  const stateCopy = taskStateCopy(task, agentName)
  const closedByModel = isAgentClosure(task)
  const closeReason = modelCloseReason(task)

  const sectionTitle = (() => {
    if (task.status === 'pending') return '下一步'
    if (task.status === 'executing') return '正在推进'
    if (task.status === 'waiting') return '等待中'
    if (task.status === 'needs_human') return '需要你回复'
    if (closedByModel) return '模型关闭原因'
    if (task.status === 'done') return '完成结果'
    if (task.status === 'observing') return '调查结论'
    return '异常原因'
  })()

  return (
    <div className="task-primary-card">
      <div className="task-section-kicker">{sectionTitle}</div>
      {task.status === 'failed' && (
        <Alert
          type={failureKindOf(task) === 'manual' || failureKindOf(task) === 'interrupted' ? 'warning' : 'error'}
          showIcon
          title={failureMeta[failureKindOf(task) || 'unknown'].label}
          description={error || summary || '任务没有记录失败详情。'}
        />
      )}
      {closedByModel && (
        <Alert
          type="info"
          showIcon
          title="Agent 的判断"
          description={closeReason || '数据异常：这次模型关闭没有记录理由。'}
        />
      )}
      {task.status !== 'failed' && !closedByModel && (
        <Paragraph className="task-readable-text task-primary-summary">{stateCopy.current}</Paragraph>
      )}
      {task.status === 'needs_human' ? (
        <Alert type="warning" showIcon title="Agent 的问题" description={question || stateCopy.next} />
      ) : (
        <Text type="secondary"><strong>接下来：</strong>{stateCopy.next}</Text>
      )}
      {enrichments.length > 0 && (
        <div className="task-enrichment-list">
          {enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
        </div>
      )}
      <Space className="task-decision-actions" wrap>{actions}</Space>
    </div>
  )
}

function taskStateCopy(task: Task, agentName: string): { current: string; next: string } {
  const result = task.execution_result
  const summary = task.summary?.trim() || strField(result, 'summary')
  const error = strField(result, 'error')
  if (isAgentClosure(task)) {
    return {
      current: modelCloseReason(task) || '数据异常：这次模型关闭没有记录理由。',
      next: '当前任务已由 Agent 停止追踪；如果判断有误，可以重跑任务。',
    }
  }
  if (task.status === 'done') {
    return {
      current: summary || '任务已完成。',
      next: '当前任务不需要继续操作。',
    }
  }
  if (task.status === 'observing') {
    return {
      current: summary || task.summary || '调查已经完成，当前没有需要执行的动作。',
      next: '无需继续处理；后续出现新变化时会形成新的工作事项。',
    }
  }
  if (task.status === 'failed') {
    const kind = failureKindOf(task)
    return {
      current: error || summary || '任务执行失败。',
      next: kind === 'manual'
        ? '这是你手动标记的失败；需要时可以重跑任务。'
        : '检查失败原因后重跑任务。',
    }
  }
  if (task.status === 'executing') {
    return {
      current: `${agentName} 正在执行任务。`,
      next: '可以等待执行完成；如需停止，可使用“打断执行”。',
    }
  }
  if (task.status === 'waiting') {
    const waiting = result?.waiting && typeof result.waiting === 'object'
      ? result.waiting as Record<string, unknown>
      : null
    return {
      current: summary || String(waiting?.reason || '任务正在等待外部条件。'),
      next: waiting?.wake_at
        ? `将在 ${String(waiting.wake_at)} 自动恢复同一个执行会话。`
        : '已预约自动恢复同一个执行会话。',
    }
  }
  if (task.status === 'needs_human') {
    return {
      current: summary || `${agentName} 已暂停当前执行会话。`,
      next: questionText(task) || '回复后将继续同一个执行会话，不会重跑任务。',
    }
  }
  return {
    current: task.target || task.title,
    next: `开始执行后，${agentName} 将使用完整任务上下文完成工作。`,
  }
}

function ProgressStrip({
  events,
  onSelect,
}: {
  events: TaskEvent[]
  onSelect: (event: TaskEvent) => void
}) {
  const ordered = useMemo(
    () => [...events].sort((a, b) => new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime()),
    [events],
  )
  const visible = ordered.slice(0, 5)
  return (
    <section className="task-progress-strip">
      <div className="task-progress-header">
        <Text strong>任务进展（共 {events.length} 条）</Text>
        {ordered.length > visible.length && (
          <Button type="link" size="small" onClick={() => onSelect(ordered[0])}>
            查看全部
          </Button>
        )}
      </div>
      {visible.length === 0 ? (
        <Text type="secondary">暂无任务进展</Text>
      ) : (
        <div className="task-progress-items">
          {visible.map((event, index) => (
            <button key={event.id} className="task-progress-item" onClick={() => onSelect(event)}>
              <span className="task-progress-node">
                <span className="task-progress-dot" style={{ background: taskEventColor(event) }} />
                {index < visible.length - 1 && <span className="task-progress-line" />}
              </span>
              <span className="task-progress-copy">
                <strong>{taskEventLabels[event.event_type] || event.event_type}</strong>
                <small>{formatShortTime(event.occurred_at)} · {actorLabels[event.actor_type] || event.actor_type}</small>
              </span>
            </button>
          ))}
          {ordered.length > visible.length && (
            <div className="task-progress-more">+{ordered.length - visible.length}</div>
          )}
        </div>
      )}
    </section>
  )
}

function supplementScope(noteAt: string, events: TaskEvent[]): string {
  const timestamp = new Date(noteAt).getTime()
  const related = events.find((event) => {
    if (event.event_type !== 'approval_granted' && event.event_type !== 'rerun_requested') return false
    return Math.abs(new Date(event.occurred_at).getTime() - timestamp) <= 5000
  })
  if (related?.event_type === 'approval_granted') return '审批批注'
  if (related?.event_type === 'rerun_requested') return '重跑指示'
  return '任务级补充'
}

function buildHistory(task: Task, events: TaskEvent[], runs: ExecutionRun[]): HistoryItem[] {
  const runByID = new Map(runs.map((run) => [run.id, run]))
  const referencedRuns = new Set<number>()
  const items: HistoryItem[] = []

  for (const event of events) {
    if (event.event_type === 'supplemented') continue
    const run = event.run_id ? runByID.get(event.run_id) : undefined
    if (run) referencedRuns.add(run.id)
    items.push({ key: `event-${event.id}`, at: event.occurred_at, kind: 'event', event, run })
  }
  for (const [index, supplement] of (task.execution_supplements ?? []).entries()) {
    items.push({
      key: `supplement-${index}-${supplement.at}`,
      at: supplement.at,
      kind: 'supplement',
      note: supplement.note,
      scope: supplementScope(supplement.at, events),
    })
  }
  for (const run of runs) {
    if (!referencedRuns.has(run.id)) {
      items.push({ key: `run-${run.id}`, at: run.started_at, kind: 'run', run })
    }
  }
  return items.sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime())
}

function TaskHistory({
  task,
  events,
  runs,
  loading,
  eventsError,
  runsError,
  recall,
}: {
  task: Task
  events: TaskEvent[]
  runs: ExecutionRun[]
  loading: boolean
  eventsError?: string
  runsError?: string
  recall: EffectRecall
}) {
  const history = useMemo(() => buildHistory(task, events, runs), [task, events, runs])
  if (loading) return <div className="task-detail-loading"><Spin /></div>
  return (
    <div className="task-history">
      {eventsError && <Alert type="error" showIcon title="任务进展加载失败" description={eventsError} />}
      {runsError && <Alert type="error" showIcon title="执行历史加载失败" description={runsError} />}
      {history.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无任务历史" />
      ) : history.map((item) => {
        if (item.kind === 'supplement') {
          return (
            <article key={item.key} id={`task-history-${item.key}`} className="task-history-item task-history-user-note">
              <div className="task-history-marker"><CommentOutlined /></div>
              <div className="task-history-content">
                <div className="task-history-heading">
                  <Space size={8}><Text strong>我的{item.scope}</Text><Tag color="gold">{item.scope}</Tag></Space>
                  <Text type="secondary">{formatTime(item.at)}</Text>
                </div>
                <blockquote>{item.note}</blockquote>
              </div>
            </article>
          )
        }
        if (item.kind === 'run') {
          return (
            <article key={item.key} id={`task-history-${item.key}`} className="task-history-item">
              <div className="task-history-marker"><HistoryOutlined /></div>
              <div className="task-history-content">
                <div className="task-history-heading">
                  <Text strong>执行记录</Text>
                  <Text type="secondary">{formatTime(item.at)}</Text>
                </div>
                <RunDetails runID={item.run.id} index={item.run} recall={recall} />
              </div>
            </article>
          )
        }
        const { event, run } = item
        const userEvent = event.actor_type === 'user'
        return (
          <article
            key={item.key}
            id={`task-history-event-${event.id}`}
            className={`task-history-item ${userEvent ? 'task-history-user-event' : ''}`}
          >
            <div className="task-history-marker" style={{ color: taskEventColor(event) }}>
              {event.to_status === 'failed' ? <ExclamationCircleOutlined /> : <CheckCircleOutlined />}
            </div>
            <div className="task-history-content">
              <div className="task-history-heading">
                <Space size={8}>
                  <Text strong>{taskEventLabels[event.event_type] || event.event_type}</Text>
                  {userEvent && <Tag color="gold">我的操作</Tag>}
                </Space>
                <Text type="secondary">{formatTime(event.occurred_at)}</Text>
              </div>
              <Text type="secondary">
                {actorLabels[event.actor_type] || event.actor_type} · v{event.task_version}
                {event.from_status ? ` · ${event.from_status} → ${event.to_status}` : ` · ${event.to_status}`}
              </Text>
              {event.run_id && <RunDetails runID={event.run_id} index={run} recall={recall} />}
            </div>
          </article>
        )
      })}
    </div>
  )
}



function TaskMeta({ task }: { task: Task }) {
  const group = objectField(taskCapture(task), 'group')
  const project = objectField(taskCapture(task), 'project')
  const assigner = objectField(taskCapture(task), 'assigner')
  const handler = taskHandlerMeta(task)
  return (
    <aside className="task-meta-card">
      <div className="task-section-kicker">任务信息</div>
      <Descriptions size="small" column={1} colon={false}>
        <Descriptions.Item label="交办人">{stringValue(assigner?.name) || '—'}</Descriptions.Item>
        <Descriptions.Item label="来源会话">{stringValue(group?.name) || '—'}</Descriptions.Item>
        <Descriptions.Item label="所属项目">{stringValue(project?.name) || (task.project_id != null ? `#${task.project_id}` : '未关联')}</Descriptions.Item>
        <Descriptions.Item label="来源">{task.source_type}{task.source_id != null ? ` #${task.source_id}` : ''}</Descriptions.Item>
        <Descriptions.Item label="最终处理">{handler?.label || '尚未收口'}</Descriptions.Item>
        <Descriptions.Item label="Todo">{task.todo_id != null ? `#${task.todo_id}` : '—'}</Descriptions.Item>
        <Descriptions.Item label="Task">#{task.id}</Descriptions.Item>
        <Descriptions.Item label="版本">v{task.version}</Descriptions.Item>
      </Descriptions>
    </aside>
  )
}

function ContextPanel({ task }: { task: Task }) {
  return <FrozenContextPanel content={task.source_payload} />
}

export default function TaskDetailModal({
  task,
  runs,
  runsPage,
  runsTotal,
  onRunsPageChange,
  events,
  runsLoading,
  eventsLoading,
  runsError,
  eventsError,
  executing,
  resumeSubmitting,
  interrupting,
  recallingMessageID,
  recallError,
  onRecallMessage,
  onClose,
  onExecute,
  onRerun,
  onResume,
  onInterrupt,
}: TaskDetailModalProps) {
  const [activeTab, setActiveTab] = useState('history')
  const [contextOpen, setContextOpen] = useState(false)
  useEffect(() => {
    setActiveTab('history')
    setContextOpen(false)
  }, [task?.id])
  if (!task) return null

  const failure = failureKindOf(task)
  const handler = taskHandlerMeta(task)
  const recall: EffectRecall = {
    pending: recallingMessageID,
    run: (messageID: string) => onRecallMessage(task, messageID),
  }
  const jumpToHistory = (event: TaskEvent) => {
    setActiveTab('history')
    window.setTimeout(() => {
      document.getElementById(`task-history-event-${event.id}`)?.scrollIntoView({
        behavior: 'smooth',
        block: 'center',
      })
    }, 0)
  }

  const actions = (() => {
    if (task.status === 'pending') {
      return <Button type="primary" loading={executing} onClick={() => onExecute(task)}>开始执行</Button>
    }
    if (task.status === 'done' || task.status === 'failed' || task.status === 'observing') {
      return <Button onClick={() => onRerun(task)}>重跑</Button>
    }
    if (task.status === 'waiting') {
      return <Text type="secondary">到达唤醒时间后会自动继续</Text>
    }
    if (task.status === 'needs_human') {
      return <Button type="primary" loading={resumeSubmitting} onClick={() => onResume(task)}>回复并继续</Button>
    }
    return <>
      <Button danger loading={interrupting} onClick={() => onInterrupt(task)}>打断执行</Button>
    </>
  })()

  return (<>
      <Modal
        open
        footer={null}
        closable={false}
        centered
        width={1180}
        mask={{ closable: true }}
        onCancel={onClose}
        className="task-detail-modal"
        destroyOnHidden
      >
        <div className="task-detail-shell">
        <header className="task-detail-header">
          <div className="task-detail-title">
            <Space size={10} wrap>
              <StatusBadge label={statusMeta[task.status].label} color={statusMeta[task.status].color} />
              {failure && <Tag color={failureMeta[failure].color}>{failureMeta[failure].label}</Tag>}
              {handler && <Tag color={handler.color} title={handler.detail}>{handler.label}</Tag>}
              <Title level={3}>{task.title}</Title>
            </Space>
            <Text type="secondary">
              {taskProjectName(task)}
              {' · '}{taskSourceName(task)}
              {' · '}{actionLabels[task.action_type] || task.action_type}
              {' · '}Task #{task.id}
              {' · '}更新于 {formatTime(task.updated_at)}
            </Text>
          </div>
          <div className="task-detail-header-actions">
            {task.source_url && <Button size="small" href={task.source_url} target="_blank" rel="noopener noreferrer">消息原文</Button>}
            <Button size="small" icon={<FileTextOutlined />} onClick={() => setContextOpen(true)}>上下文依据</Button>
            <Button type="text" icon={<CloseOutlined />} aria-label="关闭" onClick={onClose} />
          </div>
        </header>

        <div className="task-detail-scroll">
          {recallError && <Alert type="error" showIcon title="撤回飞书消息失败" description={recallError} />}

          <div className="task-detail-main-grid task-detail-main-single">
            <section aria-label="任务结论与产出">
              <ResultContent task={task} actions={actions} />
              <EffectsCard effects={effectItems(task.execution_result?.effects)} recall={recall} />
            </section>
          </div>

          {eventsLoading ? (
            <section className="task-progress-strip"><Spin size="small" /></section>
          ) : eventsError ? (
            <Alert type="error" showIcon title="任务进展加载失败" description={eventsError} />
          ) : (
            <ProgressStrip events={events} onSelect={jumpToHistory} />
          )}

          <Collapse
            className="task-secondary-meta"
            ghost
            items={[{ key: 'meta', label: '任务信息', children: <TaskMeta task={task} /> }]}
          />

          <Tabs
            className="task-detail-tabs"
            activeKey={activeTab}
            onChange={setActiveTab}
            items={[
              {
                key: 'history',
                label: '任务历史',
                children: (
                  <>
                    <Pagination current={runsPage} total={runsTotal} pageSize={20} showSizeChanger={false} onChange={onRunsPageChange} showTotal={(total) => `共 ${total} 次执行`} />
                    <TaskHistory
                      task={task}
                      events={events}
                      runs={runs}
                      loading={eventsLoading || runsLoading}
                      eventsError={eventsError}
                      runsError={runsError}
                      recall={recall}
                    />
                  </>
                ),
              },
              {
                key: 'process',
                label: <span><HistoryOutlined /> 执行过程</span>,
                children: <TaskRunOutputPanel taskID={task.id} active={activeTab === 'process'} />,
              },
            ]}
          />

        </div>
        </div>
      </Modal>
      <Modal
        title="上下文依据"
        open={contextOpen}
        footer={null}
        width={760}
        zIndex={1300}
        destroyOnHidden
        className="task-context-modal"
        onCancel={() => setContextOpen(false)}
      >
        <ContextPanel task={task} />
      </Modal>
    </>)
}

function taskCapture(task: Task): Record<string, unknown> {
  const value = task.source_payload && typeof task.source_payload === 'object' && !Array.isArray(task.source_payload)
    ? (task.source_payload as Record<string, unknown>).capture : null
 return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}
