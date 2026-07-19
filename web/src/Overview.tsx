import { useEffect, useMemo, useState } from 'react'
import { Alert, Card, Col, Divider, Row, Typography } from 'antd'
import {
  ClockCircleOutlined,
  FireOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  AppstoreOutlined,
  RocketOutlined,
} from '@ant-design/icons'
import { getOverview } from './api'
import MetricCard from './components/MetricCard'
import PageHeader from './components/PageHeader'
import EmptyState from './components/EmptyState'
import StatusStackBar from './components/StatusStackBar'
import { taskStatusMeta, todoStatusMeta } from './status'
import type { Overview as OverviewData } from './types'

const { Text, Title } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function Overview() {
  const [data, setData] = useState<OverviewData>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    getOverview(controller.signal)
      .then((result) => { setData(result); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [])

  const todoTotal = useMemo(() => data?.todos.by_status?.reduce((sum, item) => sum + item.count, 0) ?? 0, [data])
  const taskTotal = useMemo(() => data?.tasks.by_status?.reduce((sum, item) => sum + item.count, 0) ?? 0, [data])

  return (
    <div className="overview">
      {error && (
        <Alert
          type="error"
          showIcon
          message="看板数据加载失败"
          description={error}
          closable
          onClose={() => setError(undefined)}
          style={{ marginBottom: 16 }}
        />
      )}

      <PageHeader title="工作台" subtitle="今日待办、任务执行与整体进展一览" />

      <Title level={5} style={{ marginTop: 8, fontWeight: 600 }}>
        <ClockCircleOutlined style={{ marginRight: 8, color: 'var(--color-primary)' }} />
        行动线索
      </Title>
      <Row gutter={[16, 16]} style={{ marginTop: 12 }}>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="待我处理"
            value={data?.todos.pending ?? 0}
            hint="need_info / need_decision"
            loading={loading}
            icon={<ClockCircleOutlined />}
            valueStyle={{ color: 'var(--status-warning)' }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="Leader 交办未闭环"
            value={data?.todos.leader_open ?? 0}
            loading={loading}
            icon={<FireOutlined />}
            valueStyle={{ color: 'var(--status-error)' }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="进行中（未闭环）"
            value={data?.todos.open ?? 0}
            loading={loading}
            icon={<AppstoreOutlined />}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="累计"
            value={data?.todos.total ?? 0}
            loading={loading}
            icon={<CheckCircleOutlined />}
          />
        </Col>
      </Row>

      <Card variant="borderless" style={{ marginTop: 16, borderRadius: 14, border: '1px solid var(--color-border)' }} loading={loading}>
        <Text type="secondary" style={{ fontSize: 13 }}>状态分布</Text>
        {todoTotal > 0 ? (
          <div style={{ marginTop: 16 }}>
            <StatusStackBar items={data?.todos.by_status ?? []} meta={todoStatusMeta} />
          </div>
        ) : (
          <div style={{ padding: '12px 0' }}>
            <EmptyState description="暂无 Todo 数据" hint="先去「背景 → 会话背景」纳入监控，系统会自动抽取" />
          </div>
        )}
      </Card>

      <Divider style={{ margin: '28px 0 16px' }} />

      <Title level={5} style={{ marginTop: 8, fontWeight: 600 }}>
        <RocketOutlined style={{ marginRight: 8, color: 'var(--color-primary)' }} />
        任务执行
      </Title>
      <Row gutter={[16, 16]} style={{ marginTop: 12 }}>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="待执行 / 执行中"
            value={data?.tasks.pending ?? 0}
            loading={loading}
            icon={<RocketOutlined />}
            valueStyle={{ color: 'var(--status-info)' }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="已完成"
            value={data?.tasks.done ?? 0}
            loading={loading}
            icon={<CheckCircleOutlined />}
            valueStyle={{ color: 'var(--status-success)' }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="失败"
            value={data?.tasks.failed ?? 0}
            loading={loading}
            icon={<CloseCircleOutlined />}
            valueStyle={{ color: 'var(--status-error)' }}
          />
        </Col>
        <Col xs={24} sm={12} lg={6}>
          <MetricCard
            title="累计"
            value={data?.tasks.total ?? 0}
            loading={loading}
            icon={<AppstoreOutlined />}
          />
        </Col>
      </Row>

      <Card variant="borderless" style={{ marginTop: 16, borderRadius: 14, border: '1px solid var(--color-border)' }} loading={loading}>
        <Text type="secondary" style={{ fontSize: 13 }}>状态分布</Text>
        {taskTotal > 0 ? (
          <div style={{ marginTop: 16 }}>
            <StatusStackBar items={data?.tasks.by_status ?? []} meta={taskStatusMeta} />
          </div>
        ) : (
          <div style={{ padding: '12px 0' }}>
            <EmptyState description="暂无 Task 数据" hint="在「待确认」中批准 Todo 后会自动生成任务" />
          </div>
        )}
      </Card>
    </div>
  )
}
