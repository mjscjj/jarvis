import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Collapse, Drawer, Empty, Input, message, Segmented, Space, Statistic, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import {
  captureDiscover,
  captureScanChat,
  captureScanRelated,
  getDebugAgentProcesses,
  getDebugFailures,
  getDebugLogs,
  getDebugModules,
  getDebugProactiveRun,
  getDebugProactiveRuns,
  getDebugScans,
  getDebugWatermarks,
} from './api'
import { agentModeLabels, agentSourceMeta } from './agentProcesses'
import PageHeader from './components/PageHeader'
import type { AgentProcess, AgentProcessSnapshot, FailureEvent, LogTail, ModuleRun, ProactiveRun, ProactiveRunDetail, ScanRow, WatermarkRow } from './types'

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
  capture: 'M1 采集',
  memory: 'M2 记忆',
  extract: 'M3 抽取',
  execute: 'M5 执行',
}

const moduleColumns: TableColumnsType<ModuleRun> = [
  {
    title: '模块', dataIndex: 'module', width: 130,
    render: (v: string) => <Text strong>{moduleLabels[v] ?? v}</Text>,
  },
  {
    title: '最近状态', dataIndex: 'status', width: 100,
    render: (v: string) => <Tag color={v === 'ok' ? 'green' : v === 'unknown' ? 'default' : 'red'}>{v}</Tag>,
  },
  { title: 'job', dataIndex: 'job', width: 130, render: (v: string) => v || '—' },
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
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">各 cron 模块最近一次运行（解析自日志尾部；cron 日志在 stderr 文件里）。</Text>
      </Space>
      {error && <Alert type="error" showIcon message="模块运行加载失败" description={error} />}
      {failingNow.length > 0 && (
        <Alert
          type="error" showIcon message="模块最近一次运行失败（需处理）"
          description={<Space direction="vertical" size={2}>{failingNow.map((r) => <Text key={r.module} className="mono">{r.last_error || r.raw}</Text>)}</Space>}
        />
      )}
      {failingNow.length === 0 && healedRecently.length > 0 && (
        <Alert
          type="success" showIcon message="当前全部正常（窗口内曾有失败，最近一次已恢复）"
          description={<Space direction="vertical" size={2}>{healedRecently.map((r) => <Text key={r.module} type="secondary" className="mono">{moduleLabels[r.module] ?? r.module}：窗口内 {r.failures} 次失败，最近一次已 ok</Text>)}</Space>}
        />
      )}
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
  const rows = data?.items ?? []
  const stillOpen = rows.filter((r) => !r.recovered)
  const occurrences = rows.reduce((total, row) => total + row.count, 0)
  const openOccurrences = stillOpen.reduce((total, row) => total + row.count, 0)
  const healedOccurrences = occurrences - openOccurrences

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">近 24 小时 cron 与 M3/M5 运行错误，按 chat、Todo、Task 或 job 判断同范围恢复。</Text>
      </Space>
      {error && <Alert type="error" showIcon message="报错时间线加载失败" description={error} />}
      {!error && rows.length === 0 && (
        <Alert type="success" showIcon message="近 24 小时无运行错误" />
      )}
      {rows.length > 0 && (
        <Alert
          type={stillOpen.length > 0 ? 'warning' : 'info'} showIcon
          message={`近 24h 共 ${occurrences} 次报错：${openOccurrences} 次仍需关注，${healedOccurrences} 次已恢复`}
        />
      )}
      <Table<FailureEvent>
        rowKey={(r) => r.logid || `${r.time}-${r.module}-${r.scope_id}-${r.error}`} size="small" columns={failureColumns} dataSource={rows} loading={loading}
        pagination={false}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开原始日志行" />, rowExpandable: () => true }}
        scroll={{ x: 1350 }}
        locale={{ emptyText: <Empty description="近 24 小时无运行错误" /> }}
      />
    </Space>
  )
}

const scanColumns: TableColumnsType<ScanRow> = [
  { title: 'ID', dataIndex: 'id', width: 70 },
  { title: '类型', dataIndex: 'scan_type', width: 130 },
  {
    title: '状态', dataIndex: 'status', width: 90,
    render: (v: string) => <Tag color={v === 'ok' ? 'green' : 'red'}>{v}</Tag>,
  },
  { title: '拉取', dataIndex: 'fetched_count', width: 70 },
  { title: '入库', dataIndex: 'inserted_count', width: 70 },
  {
    title: '错误', key: 'error', width: 260,
    render: (_, row) => (row.error_type ? <Text type="danger">{row.error_type}: {row.error_message}</Text> : <Text type="secondary">—</Text>),
  },
  { title: '开始时间', dataIndex: 'started_at', width: 180 },
  { title: '耗时(ms)', dataIndex: 'duration_ms', width: 90, render: (v: number | null) => v ?? '—' },
]

function ScansTab() {
  const { data, loading, error, refresh } = useDebugResource<{ items: ScanRow[] }>((signal) => getDebugScans(50, signal))

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Button size="small" onClick={refresh} loading={loading} style={{ alignSelf: 'flex-start' }}>刷新</Button>
      {error && <Alert type="error" showIcon message="采集流水加载失败" description={error} />}
      <Table<ScanRow>
        rowKey="id" size="small" columns={scanColumns} dataSource={data?.items ?? []} loading={loading}
        pagination={{ pageSize: 20, showSizeChanger: false }}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开完整记录 JSON" />, rowExpandable: () => true }}
        scroll={{ x: 960 }}
      />
    </Space>
  )
}

function RuntimeTab() {
  return (
    <Space direction="vertical" size={20} style={{ width: '100%' }}>
      <Card size="small" title="模块运行" variant="borderless"><ModulesTab /></Card>
      <Card size="small" title="采集流水" variant="borderless"><ScansTab /></Card>
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
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">每 3 秒自动刷新 · 采样时间 {data?.sampled_at ?? '—'}</Text>
      </Space>
      {error && <Alert type="error" showIcon message="实时 Agent 加载失败" description={error} />}
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
        pagination={false}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开进程信息" />, rowExpandable: () => true }}
        scroll={{ x: 1080 }}
        locale={{ emptyText: <Empty description="当前没有 Codex 或 Trae 运行时" /> }}
      />
    </Space>
  )
}

const watermarkColumns: TableColumnsType<WatermarkRow> = [
  { title: '会话', key: 'chat', width: 280, render: (_, row) => <Space direction="vertical" size={0}><Text>{row.group_name || '(未命名)'}</Text><Text type="secondary" className="mono">{row.chat_id}</Text></Space> },
  { title: '最后消息 ID', dataIndex: 'last_message_id', width: 260, render: (v: string) => <Text className="mono">{v}</Text> },
  { title: '最后抽取时间', dataIndex: 'last_scanned_at', width: 180 },
  { title: '更新时间', dataIndex: 'updated_at', width: 180 },
]

function WatermarksTab() {
  const { data, loading, error, refresh } = useDebugResource<{ items: WatermarkRow[] }>((signal) => getDebugWatermarks(signal))

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">每个会话的 M3 抽取游标（水位）。空表说明所有消息将被重新抽取。</Text>
      </Space>
      {error && <Alert type="error" showIcon message="水位加载失败" description={error} />}
      <Table<WatermarkRow>
        rowKey="chat_id" size="small" columns={watermarkColumns} dataSource={data?.items ?? []} loading={loading}
        pagination={false} scroll={{ x: 900 }}
      />
    </Space>
  )
}

function LogsTab() {
  const { data, loading, error, refresh } = useDebugResource<LogTail>((signal) => getDebugLogs(600, signal))
  const [source, setSource] = useState<string>('all')

  const sources = data?.sources ?? []
  const filtered = (data?.lines ?? []).filter((l) => source === 'all' || l.source === source)
  const rendered = filtered.map((l) => (sources.length > 1 ? `[${l.source}] ${l.text}` : l.text)).join('\n')

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Segmented
          size="small"
          value={source}
          onChange={(v) => setSource(String(v))}
          options={[{ label: '全部', value: 'all' }, ...sources.map((s) => ({ label: s, value: s }))]}
        />
        {data?.truncated && <Text type="secondary">（仅尾部）</Text>}
      </Space>
      {error && <Alert type="error" showIcon message="日志加载失败" description={error} />}
      {data?.notes?.map((note) => <Alert key={note} type="info" showIcon message={note} />)}
      <pre className="debug-log">{rendered || '(窗口内无日志)'}</pre>
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
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space wrap>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">持久化保存每轮主动巡视的完整 Prompt、最终输出、模型、状态和耗时；列表仅加载摘要。</Text>
      </Space>
      {error && <Alert type="error" showIcon message="主动巡视记录加载失败" description={error} />}
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
        width={920}
        onClose={() => setSelected(undefined)}
        destroyOnHidden
      >
        {detailError && <Alert type="error" showIcon message="运行详情加载失败" description={detailError} />}
        {detailLoading && <Text type="secondary">正在加载完整输入输出…</Text>}
        {detail && (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Space wrap>
              <Tag color={detail.status === 'succeeded' ? 'green' : detail.status === 'failed' ? 'red' : 'blue'}>{detail.status}</Tag>
              <Text>{detail.engine} · {detail.model}</Text>
              <Text type="secondary">{detail.started_at}</Text>
            </Space>
            {detail.error_detail && <Alert type="error" showIcon message="本轮失败" description={detail.error_detail} />}
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
    <Space direction="vertical" size={20} style={{ width: '100%' }}>
      <Alert
        type="info"
        showIcon
        message="手动触发 M1 采集，无需等 cron"
        description="全部为同步调用：采集会话消息期间按钮持续 loading，完成后弹出结果。跑完可去「采集流水」「抽取水位」子 tab 看效果。"
      />
      <Card size="small" title="全量采集" variant="borderless">
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
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
        <Space direction="vertical" size={12} style={{ width: '100%' }}>
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
      {lastResult && <Alert type="info" showIcon message="最近一次结果" description={<Text className="mono">{lastResult}</Text>} />}
    </Space>
  )
}

export default function Debug() {
  return (
    <>
      <PageHeader title="运行状态" subtitle="实时 Agent、模块与采集运行、主动巡视输入输出、报错时间线、抽取水位与运行日志" />
      <Card variant="borderless">
      <Tabs
        defaultActiveKey="failures"
        items={[
          { key: 'trigger', label: '手动触发', children: <TriggerTab /> },
          { key: 'runtime', label: '模块运行', children: <RuntimeTab /> },
          { key: 'agents', label: '实时 Agent', children: <AgentProcessesTab /> },
          { key: 'proactive-runs', label: '主动巡视', children: <ProactiveRunsTab /> },
          { key: 'failures', label: '报错时间线', children: <FailuresTab /> },
          { key: 'watermarks', label: '抽取水位', children: <WatermarksTab /> },
          { key: 'logs', label: '运行日志', children: <LogsTab /> },
        ]}
      />
      </Card>
    </>
  )
}
