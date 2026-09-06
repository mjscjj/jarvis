import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Skeleton, Typography } from 'antd'
import {
  ArrowRightOutlined,
  CheckCircleFilled,
  ClockCircleOutlined,
  ExclamationCircleFilled,
  PlayCircleFilled,
  ReloadOutlined,
  UserOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import { getDailyDigests, getDebugFailures, getDigests, listTasks } from './api'
import { usePageContext } from './pageContext'
import { taskStatusMeta } from './status'
import { taskOneLine, taskUpdatedOn, workbenchOverviewStatuses } from './review/workbenchOverview'
import type { DailyDigest, Digest, FailureEvent, Task, TaskList } from './types'
import './styles/today.css'

const { Text, Title } = Typography
const VISIBLE_ITEMS = 4

interface LoadIssue {
  label: string
  detail: string
}

interface OverviewProps {
  onOpenDailySummary: () => void
}

interface SummaryCardProps {
  className: string
  title: string
  description: string
  count: number | string
  icon: React.ReactNode
  actionLabel: string
  onAction: () => void
  loading: boolean
  hasItems: boolean
  emptyText: string
  children?: React.ReactNode
}

interface RiskRowProps {
  title: string
  detail: string
  icon?: React.ReactNode
  onClick: () => void
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function isAbortError(cause: unknown): boolean {
  return cause instanceof DOMException && cause.name === 'AbortError'
}

function briefTime(value: string | null | undefined): string {
  if (!value) return '时间未知'
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed.format('HH:mm') : value
}

function EmptyPanel({ text }: { text: string }) {
  return (
    <div className="workbench-overview-empty">
      <CheckCircleFilled />
      <span>{text}</span>
    </div>
  )
}

function TaskSummaryRow({ task, onClick }: { task: Task; onClick: () => void }) {
  const status = taskStatusMeta[task.status]
  return (
    <button type="button" className="workbench-overview-row" onClick={onClick}>
      <span className="workbench-overview-status" style={{ background: status.color }} aria-hidden="true" />
      <span className="workbench-overview-row-copy">
        <strong>{task.title}</strong>
        <span>{taskOneLine(task)}</span>
        <small>{status.label} · {briefTime(task.updated_at)}</small>
      </span>
      <ArrowRightOutlined aria-hidden="true" />
    </button>
  )
}

function RiskRow({ title, detail, icon = <ExclamationCircleFilled />, onClick }: RiskRowProps) {
  return (
    <button type="button" className="workbench-overview-row workbench-overview-risk-row" onClick={onClick}>
      <span className="workbench-overview-risk-icon" aria-hidden="true">{icon}</span>
      <span className="workbench-overview-row-copy">
        <strong>{title}</strong>
        <span>{detail}</span>
      </span>
      <ArrowRightOutlined aria-hidden="true" />
    </button>
  )
}

function SummaryCard({
  className,
  title,
  description,
  count,
  icon,
  actionLabel,
  onAction,
  loading,
  hasItems,
  emptyText,
  children,
}: SummaryCardProps) {
  return (
    <Card className={`workbench-overview-card ${className}`} variant="borderless">
      <div className="workbench-overview-card-heading">
        <span className="workbench-overview-card-icon" aria-hidden="true">{icon}</span>
        <div>
          <Text>{title}</Text>
          <Title level={3}>{count}</Title>
        </div>
        <Button type="link" size="small" onClick={onAction}>{actionLabel} <ArrowRightOutlined /></Button>
      </div>
      <Text type="secondary" className="workbench-overview-card-description">{description}</Text>
      <div className="workbench-overview-card-body">
        {loading ? <Skeleton active title={false} paragraph={{ rows: 3 }} /> : hasItems ? children : <EmptyPanel text={emptyText} />}
      </div>
    </Card>
  )
}

export default function Overview({ onOpenDailySummary }: OverviewProps) {
  const { navigate, setSelection } = usePageContext()
  const today = dayjs().format('YYYY-MM-DD')
  const [digest, setDigest] = useState<Digest>()
  const [dailyItems, setDailyItems] = useState<DailyDigest[]>([])
  const [attention, setAttention] = useState<TaskList>()
  const [active, setActive] = useState<TaskList>()
  const [terminal, setTerminal] = useState<TaskList>()
  const [failures, setFailures] = useState<FailureEvent[]>([])
  const [loading, setLoading] = useState(true)
  const [loadIssues, setLoadIssues] = useState<LoadIssue[]>([])
  const [refreshVersion, setRefreshVersion] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    async function load() {
      setLoading(true)
      const settled = await Promise.allSettled([
        getDigests(1, controller.signal),
        getDailyDigests(today, controller.signal),
        listTasks(workbenchOverviewStatuses.needsDecision, 1, VISIBLE_ITEMS, controller.signal),
        listTasks(workbenchOverviewStatuses.inProgress, 1, VISIBLE_ITEMS, controller.signal),
        listTasks([...workbenchOverviewStatuses.completed, ...workbenchOverviewStatuses.risk], 1, 100, controller.signal),
        getDebugFailures(24, controller.signal),
      ])
      if (controller.signal.aborted) return

      const issues: LoadIssue[] = []
      const recordIssue = (label: string, reason: unknown) => {
        if (!isAbortError(reason)) issues.push({ label, detail: errorText(reason) })
      }
      const [digestResult, dailyResult, attentionResult, activeResult, terminalResult, failureResult] = settled
      if (digestResult.status === 'fulfilled') setDigest(digestResult.value)
      else recordIssue('任务结果', digestResult.reason)
      if (dailyResult.status === 'fulfilled') setDailyItems(dailyResult.value.items)
      else recordIssue('总结状态', dailyResult.reason)
      if (attentionResult.status === 'fulfilled') setAttention(attentionResult.value)
      else recordIssue('需要我决定', attentionResult.reason)
      if (activeResult.status === 'fulfilled') setActive(activeResult.value)
      else recordIssue('进行中的任务', activeResult.reason)
      if (terminalResult.status === 'fulfilled') setTerminal(terminalResult.value)
      else recordIssue('今日结果', terminalResult.reason)
      if (failureResult.status === 'fulfilled') setFailures(failureResult.value.items)
      else recordIssue('风险', failureResult.reason)
      setLoadIssues(issues)
      setLoading(false)
    }
    void load()
    return () => controller.abort()
  }, [refreshVersion, today])

  const todayDigest = digest?.mine.find((item) => item.date === today)
  const todayTerminal = useMemo(
    () => (terminal?.items ?? []).filter((task) => taskUpdatedOn(task, today)),
    [terminal, today],
  )
  const completed = todayTerminal.filter((task) => task.status === 'done').slice(0, VISIBLE_ITEMS)
  const failed = todayTerminal.filter((task) => task.status === 'failed')
  const failedDigests = dailyItems.filter((item) => item.status === 'failed')
  const unresolvedFailures = failures.filter((item) => !item.recovered)
  const unresolvedFailureCount = unresolvedFailures.reduce((sum, item) => sum + Math.max(item.count, 1), 0)
  const failedTaskCount = todayDigest?.tasks_failed ?? failed.length
  const riskCount = failedTaskCount + failedDigests.length + unresolvedFailureCount

  function openTask(task: Task) {
    navigate('tasks')
    setSelection({ kind: 'task', id: task.id, label: `Task #${task.id} ${task.title}` })
  }

  return (
    <div className="workbench-overview">
      <div className="workbench-overview-intro">
        <div>
          <Title level={2}>今天的任务动态</Title>
        </div>
        <Button icon={<ReloadOutlined />} loading={loading} onClick={() => setRefreshVersion((value) => value + 1)}>刷新</Button>
      </div>

      {loadIssues.length > 0 && (
        <Alert
          type="warning"
          showIcon
          className="workbench-overview-alert"
          title={`部分信息暂时无法显示：${loadIssues.map((item) => item.label).join('、')}`}
          description={<details><summary>查看技术详情</summary>{loadIssues.map((item) => <div key={item.label}>{item.label}：{item.detail}</div>)}</details>}
          action={<Button type="link" size="small" onClick={() => navigate('debug')}>运行状态</Button>}
        />
      )}

      <div className="workbench-overview-grid">
        <SummaryCard
          className="is-attention"
          title="需要我决定"
          description="等待你拍板、授权或补充信息"
          count={attention?.total ?? '—'}
          icon={<UserOutlined />}
          actionLabel="查看全部"
          onAction={() => navigate('tasks', { view: 'needs_me' })}
          loading={loading && !attention}
          hasItems={Boolean(attention?.items.length)}
          emptyText="当前没有需要你决定的任务"
        >
          {attention?.items.map((task) => <TaskSummaryRow key={task.id} task={task} onClick={() => openTask(task)} />)}
        </SummaryCard>

        <SummaryCard
          className="is-active"
          title="进行中的任务"
          description="正在执行、待执行或等待外部条件"
          count={active?.total ?? '—'}
          icon={<PlayCircleFilled />}
          actionLabel="查看全部"
          onAction={() => navigate('tasks', { view: 'running' })}
          loading={loading && !active}
          hasItems={Boolean(active?.items.length)}
          emptyText="当前没有进行中的任务"
        >
          {active?.items.map((task) => <TaskSummaryRow key={task.id} task={task} onClick={() => openTask(task)} />)}
        </SummaryCard>

        <SummaryCard
          className="is-completed"
          title="已完成的任务"
          description="今天已经交付的结果"
          count={todayDigest?.tasks_done ?? completed.length}
          icon={<CheckCircleFilled />}
          actionLabel="查看全部"
          onAction={() => navigate('tasks', { view: 'completed' })}
          loading={loading && !terminal}
          hasItems={completed.length > 0}
          emptyText="今天还没有完成的任务"
        >
          {completed.map((task) => <TaskSummaryRow key={task.id} task={task} onClick={() => openTask(task)} />)}
        </SummaryCard>

        <SummaryCard
          className="is-risk"
          title="风险"
          description="可能影响今天交付的失败或系统异常"
          count={loading && !terminal ? '—' : riskCount}
          icon={<ExclamationCircleFilled />}
          actionLabel="查看异常"
          onAction={() => navigate('tasks', { view: 'failed' })}
          loading={loading && !terminal}
          hasItems={riskCount > 0}
          emptyText="暂未发现会影响今天交付的风险"
        >
          {failed.slice(0, 2).map((task) => (
            <RiskRow key={task.id} title={task.title} detail={taskOneLine(task)} onClick={() => openTask(task)} />
          ))}
          {failedTaskCount > failed.slice(0, 2).length && (
            <RiskRow title={`另有 ${failedTaskCount - failed.slice(0, 2).length} 项任务执行失败`} detail="预期结果可能尚未交付。" onClick={() => navigate('tasks', { view: 'failed' })} />
          )}
          {failedDigests.length > 0 && (
            <RiskRow title={`${failedDigests.length} 项总结生成失败`} detail="今天的工作回顾可能不完整。" icon={<ClockCircleOutlined />} onClick={onOpenDailySummary} />
          )}
          {unresolvedFailureCount > 0 && (
            <RiskRow title={`后台能力有 ${unresolvedFailureCount} 次异常尚未恢复`} detail="任务采集、执行或总结可能受到影响。" onClick={() => navigate('debug')} />
          )}
        </SummaryCard>
      </div>
    </div>
  )
}
