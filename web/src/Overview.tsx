import { useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Col, Row, Space, Tag, Typography } from 'antd'
import { ReloadOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import { agentModeLabels, agentSourceMeta } from './agentProcesses'
import { getDailyDigests, getDebugAgentProcesses, getDebugFailures, getDigests, getOverview } from './api'
import PageHeader from './components/PageHeader'
import StatusStackBar from './components/StatusStackBar'
import { usePageContext } from './pageContext'
import { taskStatusMeta, todoStatusMeta } from './status'
import type {
  AgentProcess,
  AgentProcessSnapshot,
  DailyDigest,
  Digest,
  FailureEvent,
  Overview as OverviewData,
  StatusCount,
} from './types'

const { Text } = Typography

const digestStatusMeta: Record<DailyDigest['status'], { color: string; label: string }> = {
  pending: { color: 'default', label: '待生成' },
  generating: { color: 'processing', label: '生成中' },
  done: { color: 'success', label: '已生成' },
  failed: { color: 'error', label: '失败' },
}

interface CompactMetricProps {
  label: string
  value: number | string
  detail: string
  tone?: 'default' | 'warning' | 'error' | 'success'
  onClick: () => void
}

function CompactMetric({ label, value, detail, tone = 'default', onClick }: CompactMetricProps) {
  return (
    <button type="button" className={`overview-key-metric overview-key-metric-${tone}`} onClick={onClick}>
      <span className="overview-key-label">{label}</span>
      <span className="overview-key-value">{value}</span>
      <span className="overview-key-detail">{detail}</span>
    </button>
  )
}

function MiniStat({ label, value, tone }: { label: string; value: number | string; tone?: 'error' | 'success' }) {
  return (
    <div className="overview-mini-stat">
      <Text type="secondary">{label}</Text>
      <span className={tone ? `overview-mini-value overview-mini-value-${tone}` : 'overview-mini-value'}>{value}</span>
    </div>
  )
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function isAbortError(cause: unknown): boolean {
  return cause instanceof DOMException && cause.name === 'AbortError'
}

function countStatus(items: StatusCount[] | undefined, ...statuses: string[]): number {
  if (!items) return 0
  const targets = new Set(statuses)
  return items.reduce((sum, item) => sum + (targets.has(item.status) ? item.count : 0), 0)
}

function timeOfDay(value: string | null | undefined): string {
  if (!value) return '—'
  const parsed = dayjs(value)
  return parsed.isValid() ? parsed.format('HH:mm') : value
}

function shortText(value: string, maxLength = 120): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value
}

export default function Overview() {
  const { navigate } = usePageContext()
  const todayDate = dayjs().format('YYYY-MM-DD')
  const [data, setData] = useState<OverviewData>()
  const [digest, setDigest] = useState<Digest>()
  const [dailyItems, setDailyItems] = useState<DailyDigest[]>([])
  const [failures, setFailures] = useState<FailureEvent[]>([])
  const [agents, setAgents] = useState<AgentProcessSnapshot>()
  const [loading, setLoading] = useState(false)
  const [agentLoading, setAgentLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [agentError, setAgentError] = useState<string>()
  const [refreshVersion, setRefreshVersion] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    async function loadOverview() {
      setLoading(true)
      const results = await Promise.allSettled([
        getOverview(controller.signal),
        getDigests(1, controller.signal),
        getDailyDigests(todayDate, controller.signal),
        getDebugFailures(24, controller.signal),
      ])
      if (controller.signal.aborted) return

      const issues: string[] = []
      const [overviewResult, digestResult, dailyResult, failureResult] = results

      if (overviewResult.status === 'fulfilled') setData(overviewResult.value)
      else if (!isAbortError(overviewResult.reason)) issues.push(`任务概览：${errorText(overviewResult.reason)}`)

      if (digestResult.status === 'fulfilled') setDigest(digestResult.value)
      else if (!isAbortError(digestResult.reason)) issues.push(`今日进展：${errorText(digestResult.reason)}`)

      if (dailyResult.status === 'fulfilled') setDailyItems(dailyResult.value.items)
      else if (!isAbortError(dailyResult.reason)) issues.push(`每日总结：${errorText(dailyResult.reason)}`)

      if (failureResult.status === 'fulfilled') setFailures(failureResult.value.items)
      else if (!isAbortError(failureResult.reason)) issues.push(`运行错误：${errorText(failureResult.reason)}`)

      setError(issues.length > 0 ? issues.join('\n') : undefined)
      setLoading(false)
    }

    void loadOverview()
    return () => controller.abort()
  }, [refreshVersion, todayDate])

  useEffect(() => {
    const controller = new AbortController()
    let inFlight = false
    let firstLoad = true
    let stopped = false

    async function loadAgents() {
      if (inFlight) return
      inFlight = true
      if (firstLoad) setAgentLoading(true)
      try {
        const result = await getDebugAgentProcesses(controller.signal)
        if (stopped) return
        setAgents(result)
        setAgentError(undefined)
      } catch (cause: unknown) {
        if (!stopped && !isAbortError(cause)) setAgentError(errorText(cause))
      } finally {
        inFlight = false
        if (!stopped && firstLoad) {
          firstLoad = false
          setAgentLoading(false)
        }
      }
    }

    void loadAgents()
    const timer = window.setInterval(() => void loadAgents(), 3000)
    return () => {
      stopped = true
      controller.abort()
      window.clearInterval(timer)
    }
  }, [refreshVersion])

  const todoTotal = useMemo(
    () => data?.todos.by_status?.reduce((sum, item) => sum + item.count, 0) ?? 0,
    [data],
  )
  const taskTotal = useMemo(
    () => data?.tasks.by_status?.reduce((sum, item) => sum + item.count, 0) ?? 0,
    [data],
  )
  const taskNeedsHuman = countStatus(data?.tasks.by_status, 'needs_human', 'awaiting_approval')
  const taskExecuting = countStatus(data?.tasks.by_status, 'executing')
  const taskWaiting = countStatus(data?.tasks.by_status, 'waiting')
  // The only human gate is on the Task: M5 parks there when it wants me to
  // approve a proposal or answer a question. Clues never wait on me.
  const needsAttention = taskNeedsHuman
  const unresolvedFailures = failures.filter((item) => !item.recovered)
  const unresolvedFailureCount = unresolvedFailures.reduce((sum, item) => sum + Math.max(item.count, 1), 0)
  const today = digest?.mine.find((item) => item.date === todayDate) ?? digest?.mine[0]
  const todayGroupMessages = digest?.key_groups.reduce(
    (sum, group) => sum + (group.days.find((item) => item.date === todayDate)?.messages ?? 0),
    0,
  ) ?? 0
  const personDigest = dailyItems.find((item) => item.scope === 'person')
  const groupDigests = dailyItems.filter((item) => item.scope === 'group')
  const finishedGroupDigests = groupDigests.filter((item) => item.status === 'done').length
  const activeTaskAgents = (agents?.summary.codex_executing ?? 0) + (agents?.summary.trae_cli ?? 0)
  const jarvisAgents = (agents?.summary.jarvis_codex ?? 0) + (agents?.summary.jarvis_trae ?? 0)
  // 常驻 app-server 只说明运行时活着，不代表有任务在跑，这里不展示。
  const agentPool = useMemo(
    () => (agents?.items ?? []).filter((item) => item.mode !== 'app-server'),
    [agents],
  )
  const visibleAgents = useMemo(
    () => [...agentPool]
      .filter((item) => !item.nested || item.mode === 'exec' || item.mode === 'cli')
      .sort((left, right) => Number(right.mode === 'exec' || right.mode === 'cli')
        - Number(left.mode === 'exec' || left.mode === 'cli')
        || Number(right.jarvis_owned) - Number(left.jarvis_owned)
        || left.kind.localeCompare(right.kind))
      .slice(0, 5),
    [agentPool],
  )
  const hiddenAgentCount = Math.max(0, agentPool.length - visibleAgents.length)
  const hasRuntimeProblem = unresolvedFailureCount > 0 || Boolean(agentError)

  return (
    <div className="overview overview-compact">
      <PageHeader title="Overview" subtitle={`今天 · ${dayjs().format('MM-DD')}`}>
        <Space size={12}>
          <Badge status={hasRuntimeProblem ? 'error' : 'success'} text={hasRuntimeProblem ? '有异常' : '运行正常'} />
          <Text type="secondary" className="overview-updated-at">{timeOfDay(agents?.sampled_at)}</Text>
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={loading || agentLoading}
            onClick={() => setRefreshVersion((value) => value + 1)}
          >
            刷新
          </Button>
        </Space>
      </PageHeader>

      {error && (
        <Alert
          type="error"
          showIcon
          message="部分数据加载失败"
          description={<span style={{ whiteSpace: 'pre-line' }}>{error}</span>}
          closable
          onClose={() => setError(undefined)}
          className="overview-alert"
        />
      )}

      <div className="overview-status-strip" aria-busy={loading && data === undefined}>
        <CompactMetric
          label="待我处理"
          value={data ? needsAttention : '—'}
          detail="待审批或待我答复的 Task"
          tone={needsAttention > 0 ? 'warning' : 'success'}
          onClick={() => navigate('tasks')}
        />
        <CompactMetric
          label="Leader 未闭环"
          value={data?.todos.leader_open ?? (data ? 0 : '—')}
          detail="仍在推进的交办"
          tone={(data?.todos.leader_open ?? 0) > 0 ? 'error' : 'default'}
          onClick={() => navigate('todos')}
        />
        <CompactMetric
          label="Task 执行中"
          value={data ? taskExecuting : '—'}
          detail={`${taskWaiting} 个等待唤醒`}
          onClick={() => navigate('tasks')}
        />
        <CompactMetric
          label="运行异常"
          value={loading && failures.length === 0 ? '—' : unresolvedFailureCount}
          detail="最近 24 小时"
          tone={unresolvedFailureCount > 0 ? 'error' : 'success'}
          onClick={() => navigate('debug')}
        />
      </div>

      {unresolvedFailureCount > 0 && (
        <Alert
          type="warning"
          showIcon
          message={`${unresolvedFailureCount} 个未恢复错误 · ${shortText(unresolvedFailures[0]?.error || '查看运行状态了解详情')}`}
          action={<Button type="link" size="small" onClick={() => navigate('debug')}>查看</Button>}
          className="overview-alert"
        />
      )}

      <Row gutter={[12, 12]} className="overview-main-grid">
        <Col xs={24} xl={14}>
          <Card
            size="small"
            title="今天"
            extra={<Button type="link" size="small" onClick={() => navigate('progress')}>进度与总结</Button>}
            className="overview-panel"
            loading={loading && digest === undefined}
          >
            <div className="overview-mini-grid">
              <MiniStat label="新增 Todo" value={today?.todos_created ?? 0} />
              <MiniStat label="生成 Task" value={today?.tasks_created ?? 0} />
              <MiniStat label="完成 Task" value={today?.tasks_done ?? 0} tone="success" />
              <MiniStat label="失败 Task" value={today?.tasks_failed ?? 0} tone={(today?.tasks_failed ?? 0) > 0 ? 'error' : undefined} />
              <MiniStat label="群消息" value={todayGroupMessages} />
            </div>

            <div className="overview-summary-list">
              <div className="overview-summary-row">
                <Space size={8} wrap>
                  <Text strong>个人总结</Text>
                  {personDigest ? (
                    <Tag color={digestStatusMeta[personDigest.status].color}>{digestStatusMeta[personDigest.status].label}</Tag>
                  ) : (
                    <Tag>未生成</Tag>
                  )}
                </Space>
                <Text type="secondary">
                  {personDigest
                    ? `${timeOfDay(personDigest.generated_at)} · ${personDigest.source_count} 条证据`
                    : '今天尚未生成'}
                </Text>
              </div>
              <div className="overview-summary-row">
                <Text strong>群总结</Text>
                <Text type="secondary">
                  {groupDigests.length > 0 ? `${finishedGroupDigests}/${groupDigests.length} 已生成` : '暂无群总结'}
                </Text>
              </div>
            </div>
          </Card>
        </Col>

        <Col xs={24} xl={10}>
          <Card
            size="small"
            title="Agent 运行"
            extra={<Button type="link" size="small" onClick={() => navigate('debug')}>详情</Button>}
            className="overview-panel"
            loading={agentLoading && agents === undefined}
          >
            {agentError && <Alert type="error" showIcon message="Agent 运行态加载失败" description={agentError} className="overview-inline-alert" />}
            <div className="overview-mini-grid overview-runtime-stats">
              <MiniStat label="执行任务" value={activeTaskAgents} tone={activeTaskAgents > 0 ? 'success' : undefined} />
              <MiniStat label="Trae 桌面端" value={agents?.summary.trae_desktop ?? 0} />
              <MiniStat label="Jarvis 启动" value={jarvisAgents} />
            </div>

            <div className="overview-agent-list">
              {visibleAgents.length === 0 ? (
                <Text type="secondary">当前没有 Codex 或 Trae 运行时</Text>
              ) : visibleAgents.map((item: AgentProcess) => (
                <div className="overview-agent-row" key={`${item.kind}-${item.pid}`}>
                  <Space size={6}>
                    <Tag color={agentSourceMeta[item.source].color}>{agentSourceMeta[item.source].label}</Tag>
                    <Text>{item.kind === 'codex' ? 'Codex' : 'Trae'} · {agentModeLabels[item.mode]}</Text>
                    {(item.mode === 'exec' || item.mode === 'cli') && <Badge status="processing" />}
                  </Space>
                  <Text type="secondary" className="mono">{item.elapsed}</Text>
                </div>
              ))}
              {hiddenAgentCount > 0 && (
                <Button type="link" size="small" className="overview-agent-more" onClick={() => navigate('debug')}>
                  另有 {hiddenAgentCount} 个派生进程
                </Button>
              )}
            </div>
          </Card>
        </Col>
      </Row>

      <Card
        size="small"
        title="全局队列"
        className="overview-panel overview-queue-panel"
        loading={loading && data === undefined}
      >
        <div className="overview-queue-row">
          <div className="overview-queue-heading">
            <Space size={8} wrap>
              <Button type="link" size="small" onClick={() => navigate('todos')}>Todo</Button>
              <Tag color={(data?.todos.open ?? 0) > 0 ? 'warning' : 'default'}>{data?.todos.open ?? 0} 未闭环</Tag>
              <Text type="secondary">共 {data?.todos.total ?? 0}</Text>
            </Space>
          </div>
          <div className="overview-queue-bar">
            {todoTotal > 0
              ? <StatusStackBar items={data?.todos.by_status ?? []} meta={todoStatusMeta} height={8} showLegend={false} />
              : <Text type="secondary">暂无 Todo</Text>}
          </div>
        </div>
        <div className="overview-queue-row">
          <div className="overview-queue-heading">
            <Space size={8} wrap>
              <Button type="link" size="small" onClick={() => navigate('tasks')}>Task</Button>
              <Tag color={(data?.tasks.pending ?? 0) > 0 ? 'processing' : 'default'}>{data?.tasks.pending ?? 0} 进行中</Tag>
              <Text type="secondary">{data?.tasks.done ?? 0} 完成 · {data?.tasks.failed ?? 0} 失败</Text>
            </Space>
          </div>
          <div className="overview-queue-bar">
            {taskTotal > 0
              ? <StatusStackBar items={data?.tasks.by_status ?? []} meta={taskStatusMeta} height={8} showLegend={false} />
              : <Text type="secondary">暂无 Task</Text>}
          </div>
        </div>
      </Card>
    </div>
  )
}
