import { ReloadOutlined } from '@ant-design/icons'
import { Alert, Button, Segmented, Skeleton, Space, Table, Tag, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { getDebugMonitoring } from '../api'
import type { MonitoringPoint, MonitoringSnapshot } from '../types'
import {
  formatMonitoringCount,
  formatMonitoringDuration,
  formatMonitoringRate,
  monitoringRangeBounds,
  type MonitoringRange,
} from './monitoringPresentation'

const { Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

interface MiniTrendProps {
  points: MonitoringPoint[]
  value: (point: MonitoringPoint) => number | null
  format: (value: number) => string
  detail: (point: MonitoringPoint) => string
  bars?: boolean
}

function bucketLabel(value: string): string {
  const date = new Date(value)
  return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:00`
}

function MiniTrend({ points, value, format, detail, bars = false }: MiniTrendProps) {
  const active = points.filter((point) => point.recording_active)
  const plotted = active
    .map((point, index) => ({ point, index, value: value(point) }))
    .filter((item): item is { point: MonitoringPoint; index: number; value: number } => item.value != null)
  if (plotted.length === 0) return <div className="core-trend-empty">暂无数据</div>

  const width = 220
  const height = 68
  const inset = 5
  const maxValue = Math.max(...plotted.map((item) => item.value))
  const scaleMax = Math.max(maxValue, 1)
  const x = (index: number) => active.length <= 1 ? width / 2 : inset + index * ((width - inset * 2) / (active.length - 1))
  const y = (metric: number) => height - inset - metric / scaleMax * (height - inset * 2)
  const path = plotted.map((item, index) => `${index === 0 ? 'M' : 'L'} ${x(item.index)} ${y(item.value)}`).join(' ')
  const barWidth = Math.max(2, Math.min(12, (width - inset * 2) / Math.max(active.length, 1) * 0.55))

  return (
    <div className="core-trend-chart">
      <svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="趋势图">
        <line x1={inset} y1={height - inset} x2={width - inset} y2={height - inset} className="core-trend-axis" />
        {bars ? plotted.map((item) => {
          const top = y(item.value)
          return (
            <rect key={item.point.bucket_start} x={x(item.index) - barWidth / 2} y={top} width={barWidth} height={height - inset - top} className="core-trend-bar">
              <title>{`${bucketLabel(item.point.bucket_start)}：${detail(item.point)}`}</title>
            </rect>
          )
        }) : (
          <>
            <path d={path} className="core-trend-line" />
            {plotted.map((item) => (
              <circle key={item.point.bucket_start} cx={x(item.index)} cy={y(item.value)} r="2.4" className="core-trend-point">
                <title>{`${bucketLabel(item.point.bucket_start)}：${detail(item.point)}`}</title>
              </circle>
            ))}
          </>
        )}
      </svg>
      <div className="core-trend-scale"><span>{bucketLabel(active[0].bucket_start)}</span><span>{format(maxValue)}</span><span>{bucketLabel(active[active.length - 1].bucket_start)}</span></div>
    </div>
  )
}

interface TrendRow {
  key: 'm2' | 'm3' | 'm5'
  stage: string
  description: string
  scopeLabel: string
  scopeCount: number
  runCount: number
  averageDuration: number | null
  failedRuns: number
  failureRate: number | null
  totalTokens: number | null
  tokenCoverageComplete: boolean
  recordedSince: string | null
  series: MonitoringPoint[]
}

export default function CoreTrends() {
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
      .then((result) => { setSnapshot(result); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [range, refreshTick])

  const rows = useMemo<TrendRow[]>(() => snapshot ? [
    {
      key: 'm2', stage: 'M2', description: '消息采集', scopeLabel: '入库',
      scopeCount: snapshot.m2.inserted_messages, runCount: snapshot.m2.run_count,
      averageDuration: snapshot.m2.average_duration_ms,
      failedRuns: snapshot.m2.failed_runs, failureRate: snapshot.m2.failure_rate,
      totalTokens: null, tokenCoverageComplete: true,
      recordedSince: snapshot.m2.recorded_since, series: snapshot.m2.series,
    },
    {
      key: 'm3', stage: 'M3', description: '会话处理', scopeLabel: '会话',
      scopeCount: snapshot.m3.chat_count, runCount: snapshot.m3.run_count,
      averageDuration: snapshot.m3.average_duration_ms,
      failedRuns: snapshot.m3.failed_runs, failureRate: snapshot.m3.failure_rate,
      totalTokens: snapshot.m3.total_tokens, tokenCoverageComplete: snapshot.m3.token_coverage_complete,
      recordedSince: snapshot.m3.recorded_since, series: snapshot.m3.series,
    },
    {
      key: 'm5', stage: 'M5', description: '任务执行', scopeLabel: '任务',
      scopeCount: snapshot.m5.processed_tasks, runCount: snapshot.m5.run_count,
      averageDuration: snapshot.m5.average_duration_ms,
      failedRuns: snapshot.m5.failed_runs, failureRate: snapshot.m5.failure_rate,
      totalTokens: snapshot.m5.total_tokens, tokenCoverageComplete: snapshot.m5.token_coverage_complete,
      recordedSince: snapshot.m5.recorded_since, series: snapshot.m5.series,
    },
  ] : [], [snapshot])

  const columns: TableColumnsType<TrendRow> = [
    {
      title: '阶段', width: 120,
      render: (_, row) => <Space orientation="vertical" size={2}><Tag color={row.key === 'm2' ? 'green' : row.key === 'm3' ? 'blue' : 'cyan'}>{row.stage}</Tag><Text type="secondary">{row.description}</Text></Space>,
    },
    {
      title: '运行趋势', width: 260,
      render: (_, row) => <div className="core-trend-cell">
        <div><Text strong>{row.scopeLabel} {formatMonitoringCount(row.scopeCount)}</Text><Text type="secondary"> · 运行 {formatMonitoringCount(row.runCount)}</Text></div>
        <MiniTrend
          points={row.series} value={(point) => point.run_count} format={formatMonitoringCount}
          detail={(point) => `${row.scopeLabel} ${point.scope_count}，运行 ${point.run_count}`}
        />
      </div>,
    },
    {
      title: '平均耗时', width: 260,
      render: (_, row) => <div className="core-trend-cell">
        <Text strong>{formatMonitoringDuration(row.averageDuration)}</Text>
        <MiniTrend
          points={row.series} value={(point) => point.average_duration_ms} format={formatMonitoringDuration}
          detail={(point) => `平均 ${formatMonitoringDuration(point.average_duration_ms)}，完成 ${point.finished_runs} 次`}
        />
      </div>,
    },
    {
      title: '报错趋势', width: 260,
      render: (_, row) => <div className="core-trend-cell">
        <div><Text strong type={row.failedRuns > 0 ? 'danger' : undefined}>失败 {formatMonitoringCount(row.failedRuns)}</Text><Text type="secondary"> · {formatMonitoringRate(row.failureRate)}</Text></div>
        <MiniTrend
          points={row.series} value={(point) => point.failed_runs} format={formatMonitoringCount} bars
          detail={(point) => `失败 ${point.failed_runs}，失败率 ${formatMonitoringRate(point.failure_rate)}`}
        />
      </div>,
    },
    {
      title: (
        <Space size={4}>Token趋势<Tooltip title="M2 不调用模型，无 Token；M3/M5 总 Token = 输入 Token + 输出 Token，旧运行可能没有上报。"><Text type="secondary">ⓘ</Text></Tooltip></Space>
      ),
      width: 260,
      render: (_, row) => <div className="core-trend-cell">
        <Space size={6}><Text strong>{formatMonitoringCount(row.totalTokens)}</Text>{row.totalTokens != null && !row.tokenCoverageComplete && <Tag color="orange">部分数据</Tag>}</Space>
        <MiniTrend
          points={row.series} value={(point) => point.total_tokens} format={formatMonitoringCount}
          detail={(point) => `Token ${formatMonitoringCount(point.total_tokens)}${point.token_coverage_complete ? '' : '（部分数据）'}`}
        />
      </div>,
    },
  ]

  return (
    <div className="core-trends">
      <div className="core-trends-toolbar">
        <Segmented<MonitoringRange>
          value={range} onChange={setRange}
          options={[{ label: '今日', value: 'today' }, { label: '24 小时', value: '24h' }, { label: '7 天', value: '7d' }]}
        />
        <Space size={12}><Text type="secondary">每 30 秒刷新</Text><Button size="small" icon={<ReloadOutlined />} loading={loading} onClick={refresh}>刷新</Button></Space>
      </div>
      {error && <Alert type="error" showIcon title="核心趋势加载失败" description={error} />}
      {!snapshot && loading ? <Skeleton active paragraph={{ rows: 5 }} /> : (
        <Table<TrendRow>
          rowKey="key" size="small" columns={columns} dataSource={rows} pagination={false}
          scroll={{ x: 1160 }} locale={{ emptyText: '暂无运行记录' }}
        />
      )}
      {rows.some((row) => row.recordedSince) && (
        <Text type="secondary" className="core-trends-boundary">
          趋势仅展示已持久化的运行记录；历史记录开始前保持空白。
        </Text>
      )}
    </div>
  )
}
