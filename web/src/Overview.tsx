import { useEffect, useState } from 'react'
import { Alert, Card, Col, Row, Statistic, Tag, Typography } from 'antd'
import { getOverview } from './api'
import type { Overview as OverviewData } from './types'

const { Text, Title } = Typography

const todoStatusMeta: Record<string, { label: string; color: string }> = {
  extracted: { label: '待评估', color: 'blue' },
  scoring: { label: '评估中', color: 'processing' },
  need_info: { label: '待补信息', color: 'orange' },
  need_decision: { label: '待决策', color: 'gold' },
  confirmed: { label: '已确认', color: 'green' },
  dismissed: { label: '已忽略', color: 'default' },
  expired: { label: '已过期', color: 'red' },
}

const taskStatusMeta: Record<string, { label: string; color: string }> = {
  pending: { label: '待执行', color: 'blue' },
  executing: { label: '执行中', color: 'gold' },
  done: { label: '已完成', color: 'green' },
  failed: { label: '失败', color: 'red' },
}

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

  return (
    <div className="overview">
      {error && <Alert type="error" showIcon message="看板数据加载失败" description={error} closable onClose={() => setError(undefined)} />}

      <Title level={5} style={{ marginTop: 8 }}>行动线索（Todo）</Title>
      <Row gutter={16}>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="待我处理" value={data?.todos.pending ?? 0} valueStyle={{ color: '#d48806' }} />
            <Text type="secondary">need_info / need_decision</Text>
          </Card>
        </Col>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="Leader 交办未闭环" value={data?.todos.leader_open ?? 0} valueStyle={{ color: '#cf1322' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="进行中(未闭环)" value={data?.todos.open ?? 0} />
          </Card>
        </Col>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="累计" value={data?.todos.total ?? 0} />
          </Card>
        </Col>
      </Row>
      <Card variant="borderless" style={{ marginTop: 12 }} loading={loading}>
        <Text type="secondary">各状态分布：</Text>
        <div style={{ marginTop: 8 }}>
          {(data?.todos.by_status ?? []).map((item) => (
            <Tag key={item.status} color={todoStatusMeta[item.status]?.color ?? 'default'} style={{ marginBottom: 4 }}>
              {todoStatusMeta[item.status]?.label ?? item.status} · {item.count}
            </Tag>
          ))}
          {(data?.todos.by_status?.length ?? 0) === 0 && !loading && <Text type="secondary">暂无数据</Text>}
        </div>
      </Card>

      <Title level={5} style={{ marginTop: 24 }}>任务与执行结果（Task）</Title>
      <Row gutter={16}>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="待执行/执行中" value={data?.tasks.pending ?? 0} valueStyle={{ color: '#096dd9' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="已完成" value={data?.tasks.done ?? 0} valueStyle={{ color: '#389e0d' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="失败" value={data?.tasks.failed ?? 0} valueStyle={{ color: '#cf1322' }} />
          </Card>
        </Col>
        <Col span={6}>
          <Card variant="borderless" loading={loading}>
            <Statistic title="累计" value={data?.tasks.total ?? 0} />
          </Card>
        </Col>
      </Row>
      <Card variant="borderless" style={{ marginTop: 12 }} loading={loading}>
        <Text type="secondary">各状态分布：</Text>
        <div style={{ marginTop: 8 }}>
          {(data?.tasks.by_status ?? []).map((item) => (
            <Tag key={item.status} color={taskStatusMeta[item.status]?.color ?? 'default'} style={{ marginBottom: 4 }}>
              {taskStatusMeta[item.status]?.label ?? item.status} · {item.count}
            </Tag>
          ))}
          {(data?.tasks.by_status?.length ?? 0) === 0 && !loading && <Text type="secondary">暂无数据</Text>}
        </div>
      </Card>
    </div>
  )
}
