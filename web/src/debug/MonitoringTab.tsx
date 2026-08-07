import { ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Segmented, Skeleton, Tag, Tooltip, Typography } from 'antd'
import { useCallback, useEffect, useState } from 'react'
import { getDebugMonitoring } from '../api'
import type { MonitoringSnapshot } from '../types'
import {
  formatMonitoringCount,
  formatMonitoringDuration,
  monitoringRangeBounds,
  type MonitoringRange,
} from './monitoringPresentation'

const { Text } = Typography

interface MetricProps {
  label: string
  value: string
  partial?: boolean
}

function Metric({ label, value, partial = false }: MetricProps) {
  return (
    <div className="monitoring-metric">
      <Text type="secondary" className="monitoring-metric-label">{label}</Text>
      <div className="monitoring-metric-value">
        {value}
        {partial && (
          <Tooltip title="所选时间内有部分旧运行没有 Token 数据，当前仅统计已上报的运行">
            <span className="monitoring-partial-dot" aria-label="Token 数据不完整" />
          </Tooltip>
        )}
      </div>
    </div>
  )
}

interface StageHeaderProps {
  stage: 'M3' | 'M5'
  title: string
  subtitle: string
  failedRuns: number
}

function StageHeader({ stage, title, subtitle, failedRuns }: StageHeaderProps) {
  return (
    <div className="monitoring-stage-header">
      <div className="monitoring-stage-heading">
        <span className={`monitoring-stage-mark monitoring-stage-mark-${stage.toLowerCase()}`}>{stage}</span>
        <div>
          <div className="monitoring-stage-title">{title}</div>
          <Text type="secondary" className="monitoring-stage-subtitle">{subtitle}</Text>
        </div>
      </div>
      {failedRuns > 0 && <Tag color="red">失败 {formatMonitoringCount(failedRuns)}</Tag>}
    </div>
  )
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function MonitoringTab() {
  const [range, setRange] = useState<MonitoringRange>('today')
  const [snapshot, setSnapshot] = useState<MonitoringSnapshot>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshTick, setRefreshTick] = useState(0)

  const refresh = useCallback(() => setRefreshTick((value) => value + 1), [])

  useEffect(() => {
    const timer = window.setInterval(refresh, 30_000)
    return () => window.clearInterval(timer)
  }, [refresh])

  useEffect(() => {
    const controller = new AbortController()
    const { from, until } = monitoringRangeBounds(range)
    setLoading(true)
    getDebugMonitoring(from.toISOString(), until.toISOString(), controller.signal)
      .then((result) => {
        setSnapshot(result)
        setError(undefined)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [range, refreshTick])

  const tokenValue = (value: number | null) => formatMonitoringCount(value)

  return (
    <div className="monitoring-panel">
      <div className="monitoring-toolbar">
        <Segmented<MonitoringRange>
          value={range}
          onChange={setRange}
          options={[
            { label: '今日', value: 'today' },
            { label: '24 小时', value: '24h' },
            { label: '7 天', value: '7d' },
          ]}
        />
        <div className="monitoring-toolbar-meta">
          <Text type="secondary">每 30 秒刷新</Text>
          <Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={refresh}>刷新</Button>
        </div>
      </div>

      {error && <Alert type="error" showIcon title="运行监控加载失败" description={error} />}

      {!snapshot && loading ? (
        <div className="monitoring-card-grid">
          <Card><Skeleton active paragraph={{ rows: 3 }} /></Card>
          <Card><Skeleton active paragraph={{ rows: 3 }} /></Card>
        </div>
      ) : snapshot ? (
        <div className="monitoring-card-grid">
          <Card className="monitoring-stage-card">
            <StageHeader stage="M3" title="消息处理" subtitle="扫描消息并生成 Todo" failedRuns={snapshot.m3.failed_runs} />
            <div className="monitoring-metrics monitoring-metrics-m3">
              <Metric label="处理消息" value={formatMonitoringCount(snapshot.m3.processed_messages)} />
              <Metric label="生成 Todo" value={formatMonitoringCount(snapshot.m3.todos_created)} />
              <Metric label="平均耗时" value={formatMonitoringDuration(snapshot.m3.average_duration_ms)} />
              <Metric label="最大耗时" value={formatMonitoringDuration(snapshot.m3.max_duration_ms)} />
              <Metric
                label="Token 消耗"
                value={tokenValue(snapshot.m3.total_tokens)}
                partial={snapshot.m3.total_tokens != null && !snapshot.m3.token_coverage_complete}
              />
            </div>
          </Card>

          <Card className="monitoring-stage-card">
            <StageHeader stage="M5" title="任务处理" subtitle="执行已生成的任务" failedRuns={snapshot.m5.failed_runs} />
            <div className="monitoring-metrics monitoring-metrics-m5">
              <Metric label="处理任务" value={formatMonitoringCount(snapshot.m5.processed_tasks)} />
              <Metric label="平均耗时" value={formatMonitoringDuration(snapshot.m5.average_duration_ms)} />
              <Metric label="最大耗时" value={formatMonitoringDuration(snapshot.m5.max_duration_ms)} />
              <Metric
                label="Token 消耗"
                value={tokenValue(snapshot.m5.total_tokens)}
                partial={snapshot.m5.total_tokens != null && !snapshot.m5.token_coverage_complete}
              />
            </div>
          </Card>
        </div>
      ) : null}
    </div>
  )
}
