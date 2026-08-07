import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Collapse, Drawer, Empty, Input, message, Segmented, Space, Statistic, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import {
  captureDiscover,
  captureScanChat,
  captureScanRelated,
  getDebugAgentProcesses,
  getDebugFailures,
  getDebugModules,
  getDebugProactiveRun,
  getDebugProactiveRuns,
  getDebugWatermarks,
} from './api'
import { agentModeLabels, agentSourceMeta } from './agentProcesses'
import PageHeader from './components/PageHeader'
import CoreTrends from './debug/CoreTrends'
import { usePageContext } from './pageContext'
import type { AgentProcess, AgentProcessSnapshot, FailureEvent, ModuleRun, ProactiveRun, ProactiveRunDetail, WatermarkRow } from './types'

const { Text, Paragraph } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

// useDebugResource centralizes the load/refresh/abort/error boilerplate every
// sub-tab shares so each tab is just its own rendering.
function useDebugResource<T>(loader: (signal: AbortSignal) => Promise<T>, deps: unknown[] = []) {
  const [data, setData] = useState<T>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [tick, setTick] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    loader(controller.signal)
      .then((result) => { setData(result); setError(undefined) })
      .catch((cause: unknown) => { if (!(cause instanceof DOMException)) setError(errorText(cause)) })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tick, ...deps])

  const refresh = useCallback(() => setTick((v) => v + 1), [])
  return { data, loading, error, refresh }
}

// RawJSON renders any value as a collapsed JSON block that expands on click,
// keeping the row list dense while full detail stays one click away.
function RawJSON({ value, label }: { value: unknown; label: string }) {
  const text = JSON.stringify(value, null, 2)
  return (
    <Collapse
      ghost
      size="small"
      items={[{ key: 'json', label: <Text type="secondary">{label}</Text>, children: <pre className="debug-json">{text}</pre> }]}
    />
  )
}

const moduleLabels: Record<string, string> = {
  capture: '消息采集（M2）',
  pipeline: '流水线协调',
  extract: '待办提取（M3）',
  execute: '任务执行（M5）',
  scheduledtask: '定时任务调度',
  factengine: '事实提取',
  factrollup: '事实汇总',
  proactive: '主动巡视',
  'meeting-sweep': '会议巡扫',
  'meeting-capture': '会议记录采集',
  'morning-brief': '晨间简报',
  'daily-digest': '个人日报',
}

const jobLabels: Record<string, string> = {
  discover: '发现会话',
  scan_related: '采集相关会话',
  extract: '提取待办',
  extract_reconcile: '补偿提取待办',
  execute: '执行任务',
  execute_reconcile: '补偿执行任务',
  scheduled_tasks: '调度定时任务',
  extract_facts: '提取事实',
  world_maintenance: '世界维护',
  fact_rollup: '汇总事实',
  proactive_heartbeat: '主动巡视',
  meeting_sweep: '巡扫会议',
  meeting_minutes: '采集会议纪要',
  morning_brief: '生成晨间简报',
  personal_daily_digest: '生成个人日报',
}

const statusPresentation: Record<string, { label: string; color: string }> = {
  ok: { label: '正常', color: 'green' },
  error: { label: '异常', color: 'red' },
  unknown: { label: '状态未知', color: 'default' },
  queued: { label: '已排队', color: 'blue' },
  running: { label: '运行中', color: 'processing' },
  skipped: { label: '已跳过', color: 'default' },
}

function moduleLabel(module: string): string {
  return moduleLabels[module] ?? `未配置中文名（${module}）`
}

const moduleColumns: TableColumnsType<ModuleRun> = [
  {
    title: '系统模块', dataIndex: 'module', width: 190,
    render: (v: string) => <Text strong>{moduleLabel(v)}</Text>,
  },
  {
    title: '最近状态', dataIndex: 'status', width: 110,
    render: (v: string) => {
      const presentation = statusPresentation[v] ?? { label: `未知状态（${v}）`, color: 'default' }
      return <Tag color={presentation.color}>{presentation.label}</Tag>
    },
  },
  { title: '最近任务', dataIndex: 'job', width: 170, render: (v: string) => v ? (jobLabels[v] ?? `未配置中文名（${v}）`) : '—' },
  { title: '最近时间', dataIndex: 'time', width: 200, render: (v: string) => <Text className="mono">{v || '—'}</Text> },
  { title: '窗口内次数', dataIndex: 'runs', width: 100 },
  {
    title: '窗口内失败', dataIndex: 'failures', width: 100,
    render: (v: number) => (v > 0 ? <Tag color="orange">{v}</Tag> : <Text type="secondary">0</Text>),
  },
  {
    title: '关键字段', key: 'fields',
    render: (_, row) => {
      const entries = Object.entries(row.fields).filter(([k]) => k !== 'status' && k !== 'job')
      if (entries.length === 0) return <Text type="secondary">—</Text>
      return <Space size={12} wrap>{entries.map(([k, v]) => <Text key={k} className="mono" type="secondary">{k}={v}</Text>)}</Space>
    },
  },
]

function ModulesTab() {
  const { data, loading, error, refresh } = useDebugResource<{ items: ModuleRun[] }>((signal) => getDebugModules(signal))
  const rows = data?.items ?? []
  // 「当前有问题」= 最近一次运行就是失败；历史失败（窗口里有、但最近一次已 ok）只做降级提示，不弹红框。
  const failingNow = rows.filter((r) => !r.current_ok && r.status !== 'unknown')
  const healedRecently = rows.filter((r) => r.current_ok && r.failures > 0)

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">各 cron 模块最近一次运行（解析自日志尾部；cron 日志在 stderr 文件里）。</Text>
      </Space>
      {error && <Alert type="error" showIcon title="模块运行加载失败" description={error} />}
      {failingNow.length > 0 && (
        <Collapse
          size="small"
          className="runtime-failure-collapse"
          items={[{
            key: 'current-failures',
            label: <Text type="danger" strong>{failingNow.length} 个模块最近一次运行失败（需处理）</Text>,
            children: (
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                {failingNow.map((row) => (
                  <div key={row.module}>
                    <Text strong>{moduleLabel(row.module)}</Text>
                    <div><Text type="secondary" className="mono">{row.time || '—'}</Text></div>
                    <div><Text type="danger" className="mono">{row.last_error || row.raw}</Text></div>
                  </div>
                ))}
              </Space>
            ),
          }]}
        />
      )}
      {failingNow.length === 0 && <Text type="secondary">当前所有模块最近一次运行正常{healedRecently.length > 0 ? `；${healedRecently.length} 个模块在日志窗口内曾失败、现已恢复` : ''}。</Text>}
      <Table<ModuleRun>
        rowKey="module" size="small" columns={moduleColumns} dataSource={rows} loading={loading}
        pagination={false}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开该模块最近一条日志与全部字段" />, rowExpandable: () => true }}
        scroll={{ x: 900 }}
        locale={{ emptyText: <Empty description="日志窗口内暂无模块运行记录（进程刚启动或日志被轮转）" /> }}
      />
    </Space>
  )
}

const failureColumns: TableColumnsType<FailureEvent> = [
  { title: '时间', dataIndex: 'time', width: 200, render: (v: string) => <Text className="mono">{v || '—'}</Text> },
  {
    title: '阶段/模块', key: 'module', width: 130,
    render: (_, row) => <Text strong>{row.stage ? row.stage.toUpperCase() : (moduleLabels[row.module] ?? row.module)}</Text>,
  },
  {
    title: '作用范围', key: 'scope', width: 220,
    render: (_, row) => <Text className="mono">{row.scope_type}={row.scope_id}</Text>,
  },
  {
    title: '触发', key: 'trigger', width: 150,
    render: (_, row) => <Text className="mono" type="secondary">{row.job || row.trigger || '—'}</Text>,
  },
  {
    title: 'logid', dataIndex: 'logid', width: 230,
    render: (v: string) => v ? <Text className="mono" copyable ellipsis>{v}</Text> : <Text type="secondary">—</Text>,
  },
  {
    title: '次数', dataIndex: 'count', width: 75,
    render: (v: number) => v > 1 ? <Tag color="orange">{v}</Tag> : v,
  },
  {
    title: '状态', dataIndex: 'recovered', width: 100,
    render: (recovered: boolean) =>
      recovered ? <Tag color="green">已恢复</Tag> : <Tag color="red">仍需关注</Tag>,
  },
  { title: '错误', dataIndex: 'error', ellipsis: true, render: (v: string) => <Text className="mono" type="danger">{v}</Text> },
]

function FailuresTab() {
  const { data, loading, error, refresh } = useDebugResource<{ items: FailureEvent[] }>((signal) => getDebugFailures(24, signal))
  const [scope, setScope] = useState<'open' | 'all'>('open')
  const rows = data?.items ?? []
  const stillOpen = rows.filter((r) => !r.recovered)
  const displayedRows = scope === 'open' ? stillOpen : rows
  const occurrences = rows.reduce((total, row) => total + row.count, 0)
  const openOccurrences = stillOpen.reduce((total, row) => total + row.count, 0)
  const healedOccurrences = occurrences - openOccurrences

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Segmented
          size="small"
          value={scope}
          onChange={(value) => setScope(value as 'open' | 'all')}
          options={[{ label: `仍需关注 ${stillOpen.length}`, value: 'open' }, { label: `全部 ${rows.length}`, value: 'all' }]}
        />
        <Text type="secondary">默认只展示仍未恢复的问题；展开后可查看完整原始记录。</Text>
      </Space>
      {error && <Alert type="error" showIcon title="报错时间线加载失败" description={error} />}
      {!error && rows.length === 0 && (
        <Alert type="success" showIcon title="近 24 小时无运行错误" />
      )}
      {rows.length > 0 && (
        <Alert
          type={stillOpen.length > 0 ? 'warning' : 'info'} showIcon
          title={`近 24h 共 ${occurrences} 次报错：${openOccurrences} 次仍需关注，${healedOccurrences} 次已恢复`}
        />
      )}
      <Table<FailureEvent>
        rowKey={(r) => r.logid || `${r.time}-${r.module}-${r.scope_id}-${r.error}`} size="small" columns={failureColumns} dataSource={displayedRows} loading={loading}
        pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: displayedRows.length <= 20 }}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开原始日志行" />, rowExpandable: () => true }}
        scroll={{ x: 1350 }}
        locale={{ emptyText: <Empty description="近 24 小时无运行错误" /> }}
      />
    </Space>
  )
}

function RuntimeTab() {
  return (
    <Space orientation="vertical" size={20} style={{ width: '100%' }}>
      <Card size="small" title="核心趋势" variant="borderless"><CoreTrends /></Card>
      <Card size="small" title="模块运行" variant="borderless"><ModulesTab /></Card>
    </Space>
  )
}

const agentColumns: TableColumnsType<AgentProcess> = [
  {
    title: '类型', dataIndex: 'kind', width: 90,
    render: (value: AgentProcess['kind']) => <Tag color={value === 'codex' ? 'blue' : 'purple'}>{value === 'codex' ? 'Codex' : 'Trae'}</Tag>,
  },
  { title: '实例', dataIndex: 'mode', width: 120, render: (value: AgentProcess['mode']) => agentModeLabels[value] },
  {
    title: '来源', dataIndex: 'source', width: 120,
    render: (value: AgentProcess['source']) => <Tag color={agentSourceMeta[value].color}>{agentSourceMeta[value].label}</Tag>,
  },
  {
    title: '关系', key: 'relation', width: 150,
    render: (_, row) => row.nested
      ? <Text type="secondary">派生自 PID {row.root_pid}</Text>
      : <Text>根会话</Text>,
  },
  { title: 'PID', dataIndex: 'pid', width: 90, render: (value: number) => <Text className="mono">{value}</Text> },
  { title: '已运行', dataIndex: 'elapsed', width: 110, render: (value: string) => <Text className="mono">{value}</Text> },
  { title: '命令', dataIndex: 'command', ellipsis: true, render: (value: string) => <Text className="mono">{value}</Text> },
]

function AgentProcessesTab() {
  const { data, loading, error, refresh } = useDebugResource<AgentProcessSnapshot>((signal) => getDebugAgentProcesses(signal))
  // 常驻 app-server 只说明运行时活着，不代表有任务在跑，这里不展示。
  const agentRows = useMemo(
    () => (data?.items ?? []).filter((item) => item.mode !== 'app-server'),
    [data],
  )

  useEffect(() => {
    const timer = window.setInterval(refresh, 3000)
    return () => window.clearInterval(timer)
  }, [refresh])

  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">每 3 秒自动刷新 · 采样时间 {data?.sampled_at ?? '—'}</Text>
      </Space>
      {error && <Alert type="error" showIcon title="实时 Agent 加载失败" description={error} />}
      <Space size={32} wrap>
        <Statistic title="Codex 正在执行" value={data?.summary.codex_executing ?? 0} />
        <Statistic title="Trae 桌面端" value={data?.summary.trae_desktop ?? 0} />
        <Statistic title="Trae CLI 任务" value={data?.summary.trae_cli ?? 0} />
        <Statistic title="Jarvis Codex" value={data?.summary.jarvis_codex ?? 0} />
        <Statistic title="Jarvis Trae" value={data?.summary.jarvis_trae ?? 0} />
      </Space>
      <Table<AgentProcess>
        rowKey={(row) => `${row.kind}-${row.pid}`}
        size="small"
        columns={agentColumns}
        dataSource={agentRows}
        loading={loading && data === undefined}
        pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: agentRows.length <= 20 }}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开进程信息" />, rowExpandable: () => true }}
        scroll={{ x: 1080 }}
        locale={{ emptyText: <Empty description="当前没有 Codex 或 Trae 运行时" /> }}
      />
    </Space>
  )
}

const watermarkColumns: TableColumnsType<WatermarkRow> = [
  { title: '会话', key: 'chat', width: 280, render: (_, row) => <Space orientation="vertical" size={0}><Text>{row.group_name || '(未命名)'}</Text><Text type="secondary" className="mono">{row.chat_id}</Text></Space> },
  { title: '最后消息 ID', dataIndex: 'last_message_id', width: 260, render: (v: string) => <Text className="mono">{v}</Text> },
  {
    title: '最后消息内容', dataIndex: 'last_message_content', width: 420,
    render: (v: string) => <Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }} ellipsis={{ rows: 2, tooltip: v }}>{v || '—'}</Paragraph>,
  },
  { title: '最后抽取时间', dataIndex: 'last_scanned_at', width: 180 },
  { title: '更新时间', dataIndex: 'updated_at', width: 180 },
]

function WatermarksTab() {
  const { data, loading, error, refresh } = useDebugResource<{ items: WatermarkRow[] }>((signal) => getDebugWatermarks(signal))

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">每个会话的 M3 抽取游标（水位）。空表说明所有消息将被重新抽取。</Text>
      </Space>
      {error && <Alert type="error" showIcon title="水位加载失败" description={error} />}
      <Table<WatermarkRow>
        rowKey="chat_id" size="small" columns={watermarkColumns} dataSource={data?.items ?? []} loading={loading}
        pagination={{ pageSize: 20, showSizeChanger: false, hideOnSinglePage: (data?.items.length ?? 0) <= 20 }} scroll={{ x: 1320 }}
      />
    </Space>
  )
}

function ProactiveRunsTab() {
  const { data, loading, error, refresh } = useDebugResource<{ items: ProactiveRun[] }>((signal) => getDebugProactiveRuns(50, signal))
  const [selected, setSelected] = useState<ProactiveRun>()
  const [detail, setDetail] = useState<ProactiveRunDetail>()
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<string>()

  useEffect(() => {
    if (!selected) {
      setDetail(undefined)
      setDetailError(undefined)
      return
    }
    const controller = new AbortController()
    setDetail(undefined)
    setDetailError(undefined)
    setDetailLoading(true)
    getDebugProactiveRun(selected.id, controller.signal)
      .then(setDetail)
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setDetailError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setDetailLoading(false) })
    return () => controller.abort()
  }, [selected])

  const columns: TableColumnsType<ProactiveRun> = [
    { title: '轮次', dataIndex: 'id', width: 80, render: (value: number) => <Text className="mono">#{value}</Text> },
    { title: '开始时间', dataIndex: 'started_at', width: 205, render: (value: string) => <Text className="mono">{value}</Text> },
    {
      title: '触发', dataIndex: 'trigger_type', width: 100,
      render: (value: ProactiveRun['trigger_type']) => <Tag>{value === 'schedule' ? '定时' : '手动'}</Tag>,
    },
    {
      title: '状态', dataIndex: 'status', width: 100,
      render: (value: ProactiveRun['status']) => (
        <Tag color={value === 'succeeded' ? 'green' : value === 'failed' ? 'red' : 'blue'}>
          {value === 'succeeded' ? '成功' : value === 'failed' ? '失败' : '运行中'}
        </Tag>
      ),
    },
    { title: 'Agent', key: 'agent', width: 250, render: (_, row) => <Text>{row.engine} · {row.model}</Text> },
    { title: '耗时', dataIndex: 'duration_ms', width: 100, render: (value: number | null) => value == null ? '—' : `${(value / 1000).toFixed(1)}s` },
    { title: '错误', dataIndex: 'error_detail', ellipsis: true, render: (value: string | null) => value ? <Text type="danger">{value}</Text> : <Text type="secondary">—</Text> },
    { title: '操作', width: 120, render: (_, row) => <Button size="small" onClick={() => setSelected(row)}>查看输入输出</Button> },
  ]

  return (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">持久化保存每轮主动巡视的完整 Prompt、最终输出、模型、状态和耗时；列表仅加载摘要。</Text>
      </Space>
      {error && <Alert type="error" showIcon title="主动巡视记录加载失败" description={error} />}
      <Table<ProactiveRun>
        rowKey="id"
        size="small"
        columns={columns}
        dataSource={data?.items ?? []}
        loading={loading}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        scroll={{ x: 1180 }}
        onRow={(row) => ({ onDoubleClick: () => setSelected(row) })}
        locale={{ emptyText: <Empty description="还没有主动巡视运行记录" /> }}
      />
      <Drawer
        title={selected ? `主动巡视 #${selected.id}` : '主动巡视'}
        open={Boolean(selected)}
        size={920}
        onClose={() => setSelected(undefined)}
        destroyOnHidden
      >
        {detailError && <Alert type="error" showIcon title="运行详情加载失败" description={detailError} />}
        {detailLoading && <Text type="secondary">正在加载完整输入输出…</Text>}
        {detail && (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <Space wrap>
              <Tag color={detail.status === 'succeeded' ? 'green' : detail.status === 'failed' ? 'red' : 'blue'}>{detail.status}</Tag>
              <Text>{detail.engine} · {detail.model}</Text>
              <Text type="secondary">{detail.started_at}</Text>
            </Space>
            {detail.error_detail && <Alert type="error" showIcon title="本轮失败" description={detail.error_detail} />}
            <Tabs
              items={[
                { key: 'input', label: '输入 Prompt', children: <pre className="debug-log">{detail.input}</pre> },
                { key: 'output', label: '输出', children: <pre className="debug-log">{detail.output ?? (detail.status === 'running' ? '(运行中，尚无输出)' : '(无输出)')}</pre> },
              ]}
            />
          </Space>
        )}
      </Drawer>
    </Space>
  )
}

// TriggerTab 是手动触发面板：本地手动跑一轮 M1 采集，无需等 cron。均为同步调用，
// 采集完成才返回，因此按钮全程 loading。
function TriggerTab() {
  const [running, setRunning] = useState<string>()
  const [chatID, setChatID] = useState('')
  const [lastResult, setLastResult] = useState<string>()

  const run = useCallback(async (key: string, label: string, fn: () => Promise<unknown>) => {
    setRunning(key)
    setLastResult(undefined)
    try {
      const result = await fn()
      message.success(`${label} 完成`)
      setLastResult(`${label} 成功：${JSON.stringify(result)}`)
    } catch (cause) {
      const text = errorText(cause)
      message.error(`${label} 失败：${text}`)
      setLastResult(`${label} 失败：${text}`)
    } finally {
      setRunning(undefined)
    }
  }, [])

  const busy = running !== undefined

  return (
    <Space orientation="vertical" size={20} style={{ width: '100%' }}>
      <Alert
        type="info"
        showIcon
        title="手动触发 M2 采集，无需等 cron"
        description="全部为同步调用：采集会话消息期间按钮持续 loading，完成后弹出结果。跑完可在「健康」查看核心趋势，或在「抽取水位」查看消费进度。"
      />
      <Card size="small" title="全量采集" variant="borderless">
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Space wrap>
            <Button
              type="primary"
              loading={running === 'scan-related'}
              disabled={busy && running !== 'scan-related'}
              onClick={() => run('scan-related', '采集所有已监听会话', captureScanRelated)}
            >
              采集所有已监听会话
            </Button>
            <Text type="secondary">对所有 related 会话跑一次增量采集（等价一次性全量 scan）。</Text>
          </Space>
          <Space wrap>
            <Button
              loading={running === 'discover'}
              disabled={busy && running !== 'discover'}
              onClick={() => run('discover', '会话发现', captureDiscover)}
            >
              会话发现
            </Button>
            <Text type="secondary">重新枚举可见会话并按规则纳入监听（等价 -discover-once），不回补历史。</Text>
          </Space>
        </Space>
      </Card>
      <Card size="small" title="采集单个会话" variant="borderless">
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Space.Compact style={{ width: '100%', maxWidth: 560 }}>
            <Input
              placeholder="输入 chat_id（如 oc_xxx）"
              value={chatID}
              onChange={(e) => setChatID(e.target.value)}
              onPressEnter={() => chatID.trim() && run('scan-chat', `采集会话 ${chatID.trim()}`, () => captureScanChat(chatID.trim()))}
              disabled={busy}
            />
            <Button
              type="primary"
              loading={running === 'scan-chat'}
              disabled={(busy && running !== 'scan-chat') || chatID.trim() === ''}
              onClick={() => run('scan-chat', `采集会话 ${chatID.trim()}`, () => captureScanChat(chatID.trim()))}
            >
              采集
            </Button>
          </Space.Compact>
          <Text type="secondary">对指定 chat_id 立即增量采集（等价 -scan-chat）；首次采集从当前时间起，不回补历史。</Text>
        </Space>
      </Card>
      {lastResult && <Alert type="info" showIcon title="最近一次结果" description={<Text className="mono">{lastResult}</Text>} />}
    </Space>
  )
}

export default function Debug() {
  const { context, setViewState } = usePageContext()
  type DebugView = 'health' | 'proactive-runs' | 'tools' | 'agents' | 'failures'
  const routeView = context.view_state.view
  const activeView: DebugView = routeView === 'proactive-runs' || routeView === 'tools' || routeView === 'agents' || routeView === 'failures'
    ? routeView
    : 'health'

  return (
    <>
      <PageHeader title="运行状态" subtitle="核心运行监控、实时 Agent、异常与诊断工具" />
      <Card variant="borderless">
      <Tabs
        activeKey={activeView}
        onChange={(view) => setViewState({ view })}
        destroyOnHidden
        items={[
          { key: 'health', label: '健康', children: <RuntimeTab /> },
          { key: 'proactive-runs', label: '主动巡视', children: <ProactiveRunsTab /> },
          {
            key: 'tools',
            label: '高级工具',
            children: (
              <Tabs
                size="small"
                destroyOnHidden
                items={[
                  { key: 'trigger', label: '手动触发', children: <TriggerTab /> },
                  { key: 'watermarks', label: '抽取水位', children: <WatermarksTab /> },
                ]}
              />
            ),
          },
          { key: 'agents', label: '实时 Agent', children: <AgentProcessesTab /> },
          { key: 'failures', label: '异常', children: <FailuresTab /> },
        ]}
      />
      </Card>
    </>
  )
}
