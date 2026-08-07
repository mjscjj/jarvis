import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Empty,
  Flex,
  Form,
  Input,
  Modal,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { EditOutlined, HistoryOutlined, ReloadOutlined } from '@ant-design/icons'
import { getRuntimeSettings, getSystemTaskRuns, updateRuntimeSettings } from './api'
import type { RuntimeSettings, RuntimeSettingsView, SystemTaskRun, SystemTaskRunList } from './types'

const { Text, Title } = Typography

type ScheduleField =
  | 'capture_discover_schedule'
  | 'capture_scan_schedule'
  | 'extract_schedule'
  | 'execute_schedule'
  | 'fact_engine_schedule'
  | 'fact_engine_rollup_schedule'
  | 'proactive_schedule'
  | 'scheduled_task_schedule'
  | 'daily_digest_schedule'

type EnabledField =
  | 'extract_enabled'
  | 'execute_auto_enabled'
  | 'fact_engine_enabled'
  | 'proactive_enabled'
  | 'scheduled_task_enabled'
  | 'daily_digest_enabled'

interface SystemTaskDefinition {
  key: string
  name: string
  category: string
  description: string
  job: string
  scheduleField: ScheduleField
  enabledField?: EnabledField
  parameters: (settings: RuntimeSettings) => string
}

const systemTasks: SystemTaskDefinition[] = [
  {
    key: 'discover',
    name: '会话发现',
    category: '飞书采集',
    description: '发现群聊和内部单聊，并更新可采集会话范围。',
    job: 'discover',
    scheduleField: 'capture_discover_schedule',
    parameters: (s) => `自动关注私聊 ${s.capture_auto_related_p2p_top_n} 个`,
  },
  {
    key: 'scan-related',
    name: '相关会话消息扫描',
    category: '飞书采集',
    description: '增量扫描已关注的群聊、话题和内部单聊；新消息会唤醒 M3。',
    job: 'scan_related',
    scheduleField: 'capture_scan_schedule',
    parameters: (s) => `${s.capture_scan_workers} 并发 · 单页 ${s.capture_page_size} 条`,
  },
  {
    key: 'extract-reconcile',
    name: 'M3 Todo 补偿抽取',
    category: 'Agent 流水线',
    description: '补偿处理事件唤醒遗漏或积压的消息，不制造新的来源证据。',
    job: 'extract_reconcile',
    scheduleField: 'extract_schedule',
    enabledField: 'extract_enabled',
    parameters: (s) => `${s.extract_concurrency} 个会话并发，每批最多 ${s.extract_batch_messages} 条消息`,
  },
  {
    key: 'execute-reconcile',
    name: 'M5 Task 补偿执行',
    category: 'Agent 流水线',
    description: '补偿领取可执行 Task；实时提交仍会直接唤醒执行器。',
    job: 'execute_reconcile',
    scheduleField: 'execute_schedule',
    enabledField: 'execute_auto_enabled',
    parameters: (s) => `每批 ${s.execute_batch_limit} 个 · ${s.execute_concurrency} 并发`,
  },
  {
    key: 'extract-facts',
    name: '持续世界建模',
    category: '事实引擎',
    description: '在主流水线之外读取消息、Todo 和 Task 增量，由 Agent 按需沉淀事实并维护人物、项目、群、资料和关系。',
    job: 'world_maintenance',
    scheduleField: 'fact_engine_schedule',
    enabledField: 'fact_engine_enabled',
    parameters: (s) => `${s.fact_engine_model} · 单次最多 ${s.fact_engine_max_material_chars} 字符`,
  },
  {
    key: 'rollup-facts',
    name: '事实日压缩',
    category: '事实引擎',
    description: '每天组装一次共享背景，每个 Agent 一次处理最多 5 个主体并各产出一条摘要。',
    job: 'fact_rollup',
    scheduleField: 'fact_engine_rollup_schedule',
    enabledField: 'fact_engine_enabled',
    parameters: (s) => `模型 ${s.fact_engine_rollup_model}`,
  },
  {
    key: 'proactive-heartbeat',
    name: '主动巡视 Agent',
    category: '主动 Agent',
    description: '以看护未完成工作为主，读取世界模型并可顺手维护明确变化；外部行动生成 Task 交给强 M5。',
    job: 'proactive_heartbeat',
    scheduleField: 'proactive_schedule',
    enabledField: 'proactive_enabled',
    parameters: (s) => `${s.proactive_cli} · ${s.proactive_model} · 启动后 ${s.proactive_startup_delay_seconds}s`,
  },
  {
    key: 'scheduled-tasks',
    name: '用户定时任务扫描',
    category: '自动任务',
    description: '领取到期的一次性、每日或间隔任务，并提交给 M5。',
    job: 'scheduled_tasks',
    scheduleField: 'scheduled_task_schedule',
    enabledField: 'scheduled_task_enabled',
    parameters: (s) => `每批最多 ${s.scheduled_task_batch_limit} 个`,
  },
  {
    key: 'personal-daily-digest',
    name: '个人日报生成',
    category: '自动任务',
    description: '按配置时间生成个人自然日工作摘要。',
    job: 'personal_daily_digest',
    scheduleField: 'daily_digest_schedule',
    enabledField: 'daily_digest_enabled',
    parameters: (s) => `${s.daily_digest_concurrency} 并发 · 群消息上限 ${s.daily_digest_message_limit}`,
  },
]

interface EditValues {
  schedule: string
  enabled: boolean
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function runStatus(status: string) {
  switch (status) {
    case 'ok':
      return { color: 'green', label: '成功' }
    case 'queued':
      return { color: 'blue', label: '已入队' }
    case 'skipped':
      return { color: 'default', label: '已跳过' }
    case 'error':
      return { color: 'red', label: '失败' }
    default:
      return { color: 'default', label: status || '未知' }
  }
}

function resultSummary(run: SystemTaskRun): string {
  const ignored = new Set(['logid', 'job', 'status', 'duration_ms'])
  return Object.entries(run.fields)
    .filter(([key]) => !ignored.has(key))
    .map(([key, value]) => `${key}=${value}`)
    .join(' · ') || '—'
}

function formatRunDuration(value: number | null): string {
  if (value == null) return '—'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(value < 10_000 ? 1 : 0)} 秒`
}

export default function SystemTasks() {
  const [view, setView] = useState<RuntimeSettingsView>()
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()
  const [success, setSuccess] = useState<string>()
  const [editing, setEditing] = useState<SystemTaskDefinition>()
  const [saving, setSaving] = useState(false)
  const [selected, setSelected] = useState<SystemTaskDefinition>()
  const [runs, setRuns] = useState<SystemTaskRunList>()
  const [runsLoading, setRunsLoading] = useState(false)
  const [runsError, setRunsError] = useState<string>()
  const [form] = Form.useForm<EditValues>()

  const reload = useCallback(() => {
    const controller = new AbortController()
    setLoading(true)
    getRuntimeSettings(controller.signal)
      .then((result) => {
        setView(result)
        setError(undefined)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [])

  useEffect(reload, [reload])

  const loadRuns = useCallback((task: SystemTaskDefinition) => {
    const controller = new AbortController()
    setRuns(undefined)
    setRunsError(undefined)
    setRunsLoading(true)
    getSystemTaskRuns(task.job, 100, controller.signal)
      .then(setRuns)
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setRunsError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setRunsLoading(false)
      })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (!selected) return
    return loadRuns(selected)
  }, [loadRuns, selected])

  const openEdit = (task: SystemTaskDefinition) => {
    if (!view) return
    setEditing(task)
    form.setFieldsValue({
      schedule: String(view.settings[task.scheduleField]),
      enabled: task.enabledField ? Boolean(view.settings[task.enabledField]) : true,
    })
  }

  const save = async () => {
    if (!editing) return
    const values = await form.validateFields()
    setSaving(true)
    try {
      // Read the latest file-backed snapshot before applying this narrow patch,
      // so another settings tab cannot be overwritten by a stale full payload.
      const latest = await getRuntimeSettings()
      const next: RuntimeSettings = {
        ...latest.settings,
        [editing.scheduleField]: values.schedule.trim(),
      }
      if (editing.enabledField) {
        Object.assign(next, { [editing.enabledField]: values.enabled })
      }
      const updated = await updateRuntimeSettings(next)
      setView(updated)
      setEditing(undefined)
      setError(undefined)
      setSuccess(`${editing.name}配置已保存`)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  const columns = useMemo<TableColumnsType<SystemTaskDefinition>>(() => [
    {
      title: '系统任务',
      dataIndex: 'name',
      width: 250,
      render: (_, task) => (
        <div>
          <Text strong>{task.name}</Text>
          <div><Text type="secondary">{task.description}</Text></div>
        </div>
      ),
    },
    { title: '分类', dataIndex: 'category', width: 120, render: (value: string) => <Tag>{value}</Tag> },
    {
      title: '状态',
      width: 100,
      render: (_, task) => {
        if (!view) return '—'
        const enabled = task.enabledField ? Boolean(view.settings[task.enabledField]) : true
        return <Tag color={enabled ? 'green' : 'default'}>{enabled ? (task.enabledField ? '启用' : '固定启用') : '停用'}</Tag>
      },
    },
    {
      title: '执行周期',
      width: 170,
      render: (_, task) => <Text code>{view ? String(view.settings[task.scheduleField]) : '—'}</Text>,
    },
    {
      title: '执行参数',
      width: 220,
      render: (_, task) => view ? <Text type="secondary">{task.parameters(view.settings)}</Text> : '—',
    },
    {
      title: '操作',
      width: 190,
      render: (_, task) => (
        <Space>
          <Button
            size="small"
            icon={<HistoryOutlined />}
            onClick={(event) => {
              event.stopPropagation()
              setSelected(task)
            }}
          >
            执行记录
          </Button>
          <Button
            size="small"
            icon={<EditOutlined />}
            onClick={(event) => {
              event.stopPropagation()
              openEdit(task)
            }}
          >
            配置
          </Button>
        </Space>
      ),
    },
  ], [view])

  if (loading && !view) {
    return <div className="system-task-loading"><Spin /></div>
  }

  return (
    <div className="system-tasks">
      <Flex className="system-tasks-header" justify="space-between" align="flex-start" gap={16}>
        <div>
          <Title level={4}>系统任务</Title>
          <Text type="secondary">集中配置 Jarvis 后台调度。配置写入本机 YAML 覆盖文件，执行记录直接读取服务日志。</Text>
        </div>
        <Button icon={<ReloadOutlined />} onClick={reload} loading={loading}>刷新</Button>
      </Flex>

      {view?.restart_required && (
        <Alert
          type="warning"
          showIcon
          title="配置已保存，重启主服务后生效"
          description="系统任务由进程启动时注册；请运行 ./scripts/rebuild-server.sh 应用新周期或开关。"
        />
      )}
      {success && <Alert type="success" showIcon title={success} closable onClose={() => setSuccess(undefined)} />}
      {error && <Alert type="error" showIcon title="系统任务配置失败" description={error} closable onClose={() => setError(undefined)} />}

      <Card className="table-card" variant="borderless">
        <Table<SystemTaskDefinition>
          rowKey="key"
          columns={columns}
          dataSource={systemTasks}
          loading={loading}
          pagination={false}
          scroll={{ x: 1050 }}
          onRow={(task) => ({ onClick: () => setSelected(task), className: 'clickable-row' })}
        />
      </Card>

      {view && (
        <Text type="secondary" className="system-tasks-file">
          配置文件：<Text code>{view.override_path}</Text>。日志受 launchd 日志轮转影响，不提供永久留存。
        </Text>
      )}

      <Modal
        title={`配置 · ${editing?.name || ''}`}
        open={Boolean(editing)}
        confirmLoading={saving}
        okText="保存配置"
        onOk={save}
        onCancel={() => setEditing(undefined)}
        destroyOnHidden
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="schedule"
            label="执行周期"
            rules={[{ required: true, whitespace: true, message: '请输入 cron 或 @every 周期' }]}
            extra="支持标准 cron，例如 0 19 * * *；也支持 @every 5m。保存后重启主服务生效。"
          >
            <Input placeholder="@every 5m" />
          </Form.Item>
          {editing?.enabledField ? (
            <Form.Item name="enabled" label="启用该任务" valuePropName="checked">
              <Switch />
            </Form.Item>
          ) : (
            <Alert type="info" showIcon title="这是基础系统任务，当前固定启用；可调整执行周期。" />
          )}
        </Form>
      </Modal>

      <Drawer
        title={selected ? `${selected.name} · 执行记录` : '执行记录'}
        open={Boolean(selected)}
        size={860}
        onClose={() => setSelected(undefined)}
        extra={<Button size="small" onClick={() => selected && loadRuns(selected)} loading={runsLoading}>刷新</Button>}
        destroyOnHidden
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {selected && <Text type="secondary">日志 job：<Text code>{selected.job}</Text>，最新记录在前。</Text>}
          {runs?.truncated && <Alert type="info" showIcon title="日志文件较大，仅展示当前日志尾部可读取到的记录。" />}
          {runs?.notes.map((note) => <Alert key={note} type="warning" showIcon title={note} />)}
          {runsError && <Alert type="error" showIcon title="执行记录加载失败" description={runsError} />}
          <Table<SystemTaskRun>
            rowKey={(run) => `${run.time}-${run.raw}`}
            size="small"
            loading={runsLoading}
            dataSource={runs?.items || []}
            pagination={{ pageSize: 20, hideOnSinglePage: true }}
            locale={{ emptyText: <Empty description="当前日志保留范围内没有执行记录" /> }}
            columns={[
              { title: '时间', dataIndex: 'time', width: 190, render: (value: string) => <Text className="mono">{value || '—'}</Text> },
              {
                title: '状态',
                dataIndex: 'status',
                width: 100,
                render: (value: string) => {
                  const meta = runStatus(value)
                  return <Tag color={meta.color}>{meta.label}</Tag>
                },
              },
              { title: '耗时', dataIndex: 'duration_ms', width: 100, render: (value: number | null) => <Text className="mono">{formatRunDuration(value)}</Text> },
              { title: '结果', render: (_, run) => <Text type={run.status === 'error' ? 'danger' : 'secondary'}>{resultSummary(run)}</Text> },
            ]}
            expandable={{
              expandedRowRender: (run) => <pre className="system-task-raw-log">{run.raw}</pre>,
              rowExpandable: () => true,
            }}
          />
          {runsLoading && !runs && <div className="system-task-loading"><Spin /></div>}
        </Space>
      </Drawer>
    </div>
  )
}
