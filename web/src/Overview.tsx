import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Col, Empty, Row, Space, Statistic, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import {
  ApiOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  FireOutlined,
  ReloadOutlined,
  RobotOutlined,
  WarningOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import { agentModeLabels, agentSourceMeta } from './agentProcesses'
import { getDailyDigests, getDebugAgentProcesses, getDebugFailures, getDigests, getOverview } from './api'
import MetricCard from './components/MetricCard'
import PageHeader from './components/PageHeader'
import EmptyState from './components/EmptyState'
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

const { Text, Title } = Typography

const digestStatusMeta: Record<DailyDigest['status'], { color: string; label: string }> = {
  pending: { color: 'default', label: '待生成' },
  generating: { color: 'processing', label: '生成中' },
  done: { color: 'success', label: '已生成' },
  failed: { color: 'error', label: '失败' },
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

function shortText(value: string, maxLength = 180): string {
  return value.length > maxLength ? `${value.slice(0, maxLength)}…` : value
}

const agentColumns: TableColumnsType<AgentProcess> = [
  {
    title: '类型',
    dataIndex: 'kind',
    width: 90,
    render: (value: AgentProcess['kind']) => (
      <Tag color={value === 'codex' ? 'blue' : 'purple'}>
        {value === 'codex' ? 'Codex' : 'Trae'}
      </Tag>
    ),
  },
  {
    title: '实例',
    dataIndex: 'mode',
    width: 120,
    render: (value: AgentProcess['mode']) => agentModeLabels[value],
  },
  {
    title: '来源',
    dataIndex: 'source',
    width: 120,
    render: (value: AgentProcess['source']) => <Tag color={agentSourceMeta[value].color}>{agentSourceMeta[value].label}</Tag>,
  },
  {
    title: 'PID',
    dataIndex: 'pid',
    width: 90,
    render: (value: number) => <Text className="mono">{value}</Text>,
  },
  {
    title: '已运行',
    dataIndex: 'elapsed',
    width: 110,
    render: (value: string) => <Text className="mono">{value}</Text>,
  },
]

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
  const needsAttention = (data?.todos.pending ?? 0) + taskNeedsHuman
  const unresolvedFailures = failures.filter((item) => !item.recovered)
  const unresolvedFailureCount = unresolvedFailures.reduce((sum, item) => sum + Math.max(item.count, 1), 0)
  const today = digest?.mine.find((item) => item.date === todayDate) ?? digest?.mine[0]
  const todayGroupMessages = digest?.key_groups.reduce(
    (sum, group) => sum + (group.days.find((item) => item.date === todayDate)?.messages ?? group.days[0]?.messages ?? 0),
    0,
  ) ?? 0
  const personDigest = dailyItems.find((item) => item.scope === 'person')
  const groupDigests = dailyItems.filter((item) => item.scope === 'group')
  const finishedGroupDigests = groupDigests.filter((item) => item.status === 'done').length
  const activeTaskAgents = (agents?.summary.codex_executing ?? 0) + (agents?.summary.trae_cli ?? 0)
  const jarvisAgents = (agents?.summary.jarvis_codex ?? 0) + (agents?.summary.jarvis_trae ?? 0)
  const visibleAgents = useMemo(
    () => [...(agents?.items ?? [])]
      .filter((item) => !item.nested || item.mode === 'exec' || item.mode === 'cli')
      .sort((left, right) => Number(right.jarvis_owned) - Number(left.jarvis_owned)
        || Number(right.mode === 'exec') - Number(left.mode === 'exec')
        || left.kind.localeCompare(right.kind))
      .slice(0, 8),
    [agents],
  )

  return (
    <div className="overview">
      <PageHeader title="Overview" subtitle="当前运行、待处理事项、今日进展与系统风险">
        <Button icon={<ReloadOutlined />} loading={loading || agentLoading} onClick={() => setRefreshVersion((value) => value + 1)}>
          刷新
        </Button>
      </PageHeader>

      {error && (
        <Alert
          type="error"
          showIcon
          message="部分概览数据加载失败"
          description={<span style={{ whiteSpace: 'pre-line' }}>{error}</span>}
          closable
          onClose={() => setError(undefined)}
          style={{ marginTop: 16 }}
        />
      )}

      {unresolvedFailureCount > 0 && (
        <Alert
          type="warning"
          showIcon
          message={`近 24 小时有 ${unresolvedFailureCount} 个未恢复错误`}
          description={unresolvedFailures[0]?.error ? shortText(unresolvedFailures[0].error) : '请查看运行状态了解详情。'}
          action={<Button size="small" onClick={() => navigate('debug')}>查看错误</Button>}
          style={{ marginTop: 16 }}
        />
      )}

      <Title level={5} style={{ marginTop: 24, fontWeight: 600 }}>
        <FireOutlined style={{ marginRight: 8, color: 'var(--color-primary)' }} />
        需要处理
      </Title>
      <Row gutter={[16, 16]} style={{ marginTop: 12 }}>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="待我处理"
            value={needsAttention}
            hint={`Todo ${data?.todos.pending ?? 0} · Task ${taskNeedsHuman}`}
            loading={loading && data === undefined}
            icon={<ClockCircleOutlined />}
            valueStyle={{ color: 'var(--status-warning)' }}
            onClick={() => navigate(data?.todos.pending ? 'confirmations' : 'tasks')}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="Leader 交办未闭环"
            value={data?.todos.leader_open ?? 0}
            hint="仍在推进的 Leader 事项"
            loading={loading && data === undefined}
            icon={<FireOutlined />}
            valueStyle={{ color: 'var(--status-error)' }}
            onClick={() => navigate('todos')}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="Task 执行中"
            value={taskExecuting}
            hint={`${taskWaiting} 个等待唤醒`}
            loading={loading && data === undefined}
            icon={<RobotOutlined />}
            valueStyle={{ color: 'var(--status-info)' }}
            onClick={() => navigate('tasks')}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="未恢复错误"
            value={unresolvedFailureCount}
            hint="最近 24 小时"
            loading={loading}
            icon={<WarningOutlined />}
            valueStyle={{ color: unresolvedFailureCount > 0 ? 'var(--status-error)' : 'var(--status-success)' }}
            onClick={() => navigate('debug')}
          />
        </Col>
      </Row>

      <Card
        variant="borderless"
        title={(
          <Space>
            <ApiOutlined />
            <span>正在运行</span>
          </Space>
        )}
        extra={(
          <Space>
            <Text type="secondary" style={{ fontSize: 12 }}>
              后台服务不等于正在执行任务 · 每 3 秒刷新 · {timeOfDay(agents?.sampled_at)}
            </Text>
            <Button type="link" size="small" onClick={() => navigate('debug')}>详细运行状态</Button>
          </Space>
        )}
        style={{ marginTop: 20, borderRadius: 14, border: '1px solid var(--color-border)' }}
      >
        {agentError && <Alert type="error" showIcon message="Agent 运行态加载失败" description={agentError} style={{ marginBottom: 16 }} />}
        <Row gutter={[16, 16]}>
          <Col xs={12} md={6}><Statistic title="Codex 后台服务" value={agents?.summary.codex_services ?? 0} loading={agentLoading && agents === undefined} /></Col>
          <Col xs={12} md={6}><Statistic title="正在执行任务" value={activeTaskAgents} loading={agentLoading && agents === undefined} /></Col>
          <Col xs={12} md={6}><Statistic title="Trae 桌面端" value={agents?.summary.trae_desktop ?? 0} loading={agentLoading && agents === undefined} /></Col>
          <Col xs={12} md={6}><Statistic title="Jarvis 启动" value={jarvisAgents} loading={agentLoading && agents === undefined} /></Col>
        </Row>
        <Table<AgentProcess>
          rowKey={(row) => `${row.kind}-${row.pid}`}
          size="small"
          columns={agentColumns}
          dataSource={visibleAgents}
          loading={agentLoading && agents === undefined}
          pagination={false}
          style={{ marginTop: 20 }}
          locale={{ emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前没有 Codex 或 Trae 实例" /> }}
        />
        {(agents?.items.length ?? 0) > visibleAgents.length && (
          <div style={{ marginTop: 8, textAlign: 'right' }}>
            <Button type="link" size="small" onClick={() => navigate('debug')}>
              另有 {(agents?.items.length ?? 0) - visibleAgents.length} 个派生进程
            </Button>
          </div>
        )}
      </Card>

      <Row gutter={[16, 16]} style={{ marginTop: 20 }}>
        <Col xs={24} xl={14}>
          <Card
            variant="borderless"
            title="今天"
            extra={<Button type="link" size="small" onClick={() => navigate('progress')}>查看进度</Button>}
            style={{ height: '100%', borderRadius: 14, border: '1px solid var(--color-border)' }}
            loading={loading && digest === undefined}
          >
            <Row gutter={[16, 20]}>
              <Col xs={12} md={6}><Statistic title="新增 Todo" value={today?.todos_created ?? 0} /></Col>
              <Col xs={12} md={6}><Statistic title="确认任务" value={today?.confirmed ?? 0} /></Col>
              <Col xs={12} md={6}><Statistic title="完成 Task" value={today?.tasks_done ?? 0} /></Col>
              <Col xs={12} md={6}><Statistic title="关键群消息" value={todayGroupMessages} /></Col>
            </Row>
          </Card>
        </Col>
        <Col xs={24} xl={10}>
          <Card
            variant="borderless"
            title="今日总结"
            extra={<Button type="link" size="small" onClick={() => navigate('progress')}>打开总结</Button>}
            style={{ height: '100%', borderRadius: 14, border: '1px solid var(--color-border)' }}
            loading={loading && dailyItems.length === 0}
          >
            <Space direction="vertical" size={14} style={{ width: '100%' }}>
              <Space wrap>
                <Text>个人总结</Text>
                {personDigest ? (
                  <Tag color={digestStatusMeta[personDigest.status].color}>{digestStatusMeta[personDigest.status].label}</Tag>
                ) : (
                  <Tag>尚未生成</Tag>
                )}
                {personDigest?.generated_at && <Text type="secondary">{timeOfDay(personDigest.generated_at)}</Text>}
              </Space>
              <Space wrap>
                <Text>群总结</Text>
                {groupDigests.length > 0 ? (
                  <Tag color={finishedGroupDigests === groupDigests.length ? 'success' : 'default'}>
                    {finishedGroupDigests} / {groupDigests.length} 已生成
                  </Tag>
                ) : (
                  <Tag>暂无群总结</Tag>
                )}
              </Space>
              <Text type="secondary">
                {personDigest
                  ? `个人总结已汇总 ${personDigest.source_count} 条证据。`
                  : '今天还没有个人总结。'}
              </Text>
            </Space>
          </Card>
        </Col>
      </Row>

      <Title level={5} style={{ marginTop: 24, fontWeight: 600 }}>
        <CheckCircleOutlined style={{ marginRight: 8, color: 'var(--color-primary)' }} />
        全局队列
      </Title>
      <Row gutter={[16, 16]} style={{ marginTop: 12 }}>
        <Col xs={24} xl={12}>
          <Card
            variant="borderless"
            title={`Todo · ${data?.todos.open ?? 0} 个未闭环`}
            extra={<Button type="link" size="small" onClick={() => navigate('todos')}>查看 Todo</Button>}
            style={{ height: '100%', borderRadius: 14, border: '1px solid var(--color-border)' }}
            loading={loading && data === undefined}
          >
            {todoTotal > 0 ? (
              <StatusStackBar items={data?.todos.by_status ?? []} meta={todoStatusMeta} />
            ) : (
              <EmptyState description="暂无 Todo" hint="系统抽取到行动线索后会显示在这里" />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card
            variant="borderless"
            title={`Task · ${data?.tasks.pending ?? 0} 个待执行或执行中`}
            extra={<Button type="link" size="small" onClick={() => navigate('tasks')}>查看 Task</Button>}
            style={{ height: '100%', borderRadius: 14, border: '1px solid var(--color-border)' }}
            loading={loading && data === undefined}
          >
            {taskTotal > 0 ? (
              <StatusStackBar items={data?.tasks.by_status ?? []} meta={taskStatusMeta} />
            ) : (
              <EmptyState description="暂无 Task" hint="确认 Todo 后会生成可执行任务" />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}
