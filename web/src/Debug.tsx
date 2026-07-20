import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Collapse, Empty, Input, message, Segmented, Space, Statistic, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import {
  captureDiscover,
  captureScanChat,
  captureScanRelated,
  getDebugLogs,
  getDebugModules,
  getDebugScans,
  getDebugStatus,
  getDebugTasks,
  getDebugTodos,
  getDebugWatermarks,
} from './api'
import PageHeader from './components/PageHeader'
import type { DebugRecord, DebugStatus, LogTail, ModuleRun, ScanRow, StatusCount, WatermarkRow } from './types'

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

function StatusPills({ title, rows }: { title: string; rows: StatusCount[] }) {
  return (
    <Card size="small" title={title} variant="borderless">
      {rows.length === 0 ? (
        <Text type="secondary">暂无数据</Text>
      ) : (
        <Space size={20} wrap>
          {rows.map((r) => (
            <Text key={r.status}>{r.status}：<Text strong>{r.count}</Text></Text>
          ))}
        </Space>
      )}
    </Card>
  )
}

function StatusTab() {
  const { data, loading, error, refresh } = useDebugResource<DebugStatus>((signal) => getDebugStatus(signal))

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        {data && <Text type="secondary">采样时间 {data.time}</Text>}
      </Space>
      {error && <Alert type="error" showIcon message="状态加载失败" description={error} />}
      <Card size="small" title="依赖健康" variant="borderless" loading={loading}>
        <Space size={24} wrap>
          {data?.dependencies.map((dep) => (
            <Badge
              key={dep.name}
              status={dep.status === 'ok' ? 'success' : 'error'}
              text={<span>{dep.name}{dep.status !== 'ok' && dep.detail ? <Text type="secondary"> · {dep.detail}</Text> : null}</span>}
            />
          ))}
        </Space>
      </Card>
      <Card size="small" title="流水线积压" variant="borderless" loading={loading}>
        <Space size={32} wrap>
          {data?.backlog.map((m) => (
            <Statistic
              key={m.key}
              title={<span>{m.label}{m.detail ? <Text type="secondary" style={{ fontSize: 11 }}> · {m.detail}</Text> : null}</span>}
              value={m.value < 0 ? '读取失败' : m.value}
              valueStyle={{ fontSize: 20, color: m.value > 0 ? '#d46b08' : undefined }}
            />
          ))}
        </Space>
      </Card>
      <StatusPills title="Todo 状态分布" rows={data?.todo_by_status ?? []} />
      <StatusPills title="Task 状态分布" rows={data?.task_by_status ?? []} />
      <Card size="small" title="数据表计数" variant="borderless" loading={loading}>
        <Space size={24} wrap>
          {data?.tables.map((t) => (
            <Text key={t.table}>{t.table}：<Text strong>{t.count < 0 ? '读取失败' : t.count}</Text></Text>
          ))}
        </Space>
      </Card>
    </Space>
  )
}

const moduleLabels: Record<string, string> = {
  capture: 'M1 采集',
  memory: 'M2 记忆',
  extract: 'M3 抽取',
  decide: 'M4 决策',
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
        locale={{ emptyText: <Empty description="日志窗口内暂无 cron 运行记录（进程刚启动或日志被轮转）" /> }}
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

// asString safely reads a stringy field off a loose debug record for the
// summary column, tolerating missing/typed values.
function asString(record: DebugRecord, key: string): string {
  const v = record[key]
  return v == null ? '' : String(v)
}

function RecentTab({ kind }: { kind: 'todos' | 'tasks' }) {
  const loader = kind === 'todos' ? getDebugTodos : getDebugTasks
  const { data, loading, error, refresh } = useDebugResource<{ items: DebugRecord[] }>((signal) => loader(20, signal), [kind])
  const rows = data?.items ?? []

  const columns: TableColumnsType<DebugRecord> = useMemo(() => [
    { title: 'ID', dataIndex: 'ID', width: 70, render: (_, row) => asString(row, 'ID') },
    { title: '标题', dataIndex: 'Title', ellipsis: true, render: (_, row) => asString(row, 'Title') || <Text type="secondary">—</Text> },
    {
      title: '状态', dataIndex: 'Status', width: 120,
      render: (_, row) => { const s = asString(row, 'Status'); return s ? <Tag>{s}</Tag> : '—' },
    },
    { title: 'action', dataIndex: 'ActionType', width: 130, render: (_, row) => asString(row, 'ActionType') || '—' },
  ], [])

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      <Space>
        <Button size="small" onClick={refresh} loading={loading}>刷新</Button>
        <Text type="secondary">最近 20 条{kind === 'todos' ? ' Todo（含 context_snapshot / target / context / resolution）' : ' Task（含 background / plan / execution_result）'}，展开看完整 JSON。</Text>
      </Space>
      {error && <Alert type="error" showIcon message="明细加载失败" description={error} />}
      <Table<DebugRecord>
        rowKey={(row) => asString(row, 'ID')} size="small" columns={columns} dataSource={rows} loading={loading}
        pagination={{ pageSize: 10, showSizeChanger: false }}
        expandable={{ expandedRowRender: (row) => <RawJSON value={row} label="展开完整 JSON" />, rowExpandable: () => true }}
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
      <PageHeader title="调试" subtitle="运行时诊断：依赖健康/积压、模块运行、采集流水、抽取水位、最近 Todo/Task 与运行日志" />
      <Card variant="borderless">
      <Tabs
        items={[
          { key: 'trigger', label: '手动触发', children: <TriggerTab /> },
          { key: 'status', label: '健康与积压', children: <StatusTab /> },
          { key: 'modules', label: '模块运行', children: <ModulesTab /> },
          { key: 'scans', label: '采集流水', children: <ScansTab /> },
          { key: 'watermarks', label: '抽取水位', children: <WatermarksTab /> },
          { key: 'todos', label: '最近 Todo', children: <RecentTab kind="todos" /> },
          { key: 'tasks', label: '最近 Task', children: <RecentTab kind="tasks" /> },
          { key: 'logs', label: '运行日志', children: <LogsTab /> },
        ]}
      />
      </Card>
    </>
  )
}
