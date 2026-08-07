import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Descriptions,
  Drawer,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  TimePicker,
  Tooltip,
  Typography,
  message,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { createScheduledTask, deleteScheduledTask, listScheduledTasks, triggerScheduledTask, updateScheduledTask } from './api'
import PageHeader from './components/PageHeader'
import { usePageContext } from './pageContext'
import type { ScheduledTask, ScheduledTaskInput, ScheduledTaskScheduleType, ScheduledTaskStatus } from './types'
import './styles/clues-automation.css'

const { Paragraph, Text } = Typography

const statusMeta: Record<ScheduledTaskStatus, { label: string; color: string }> = {
  binding: { label: '正在绑定会话', color: 'orange' },
  active: { label: '等待调度', color: 'green' },
  running: { label: '触发中', color: 'blue' },
  completed: { label: '已结束', color: 'default' },
}

type ScheduleView = 'automations' | 'wakeups'

const viewOptions = [
  { value: 'automations', label: '我的自动化' },
  { value: 'wakeups', label: '系统任务' },
] satisfies Array<{ value: ScheduleView; label: string }>

interface FormValue {
  title: string
  instruction: string
  context_snapshot: string
  schedule_type: ScheduledTaskScheduleType
  daily_time?: Dayjs
  interval_minutes?: number
  run_at?: Dayjs
  enabled: boolean
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function toInput(value: FormValue): ScheduledTaskInput {
  let context: unknown
  try {
    context = JSON.parse(value.context_snapshot || '{}')
  } catch (cause) {
    throw new Error(`上下文不是合法 JSON：${errorText(cause)}`)
  }
  if (context === null || Array.isArray(context) || typeof context !== 'object') {
    throw new Error('上下文必须是 JSON 对象')
  }
  if (value.schedule_type === 'daily' && !value.daily_time) {
    throw new Error('请选择每天执行时间')
  }
  if (value.schedule_type === 'interval' && (!value.interval_minutes || value.interval_minutes <= 0)) {
    throw new Error('执行间隔必须大于 0 分钟')
  }
  if (value.schedule_type === 'once' && !value.run_at) {
    throw new Error('请选择执行时间')
  }
  return {
    title: value.title.trim(),
    action_type: 'agent_task',
    instruction: value.instruction.trim(),
    context_snapshot: context as Record<string, unknown>,
    schedule_type: value.schedule_type,
    daily_time: value.schedule_type === 'daily' ? value.daily_time!.format('HH:mm') : null,
    interval_minutes: value.schedule_type === 'interval' ? value.interval_minutes! : null,
    run_at: value.schedule_type === 'once' ? value.run_at!.toISOString() : null,
    enabled: value.enabled,
  }
}

function scheduleText(task: ScheduledTask): string {
  if (task.schedule_type === 'once') return `执行一次 · ${formatDateTime(task.run_at)}`
  if (task.schedule_type === 'daily') return `每天 ${task.daily_time}`
  return `每 ${task.interval_minutes} 分钟`
}

function formatDateTime(value: string | null): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

function dailyTimeValue(value: string): Dayjs {
  const [hour, minute] = value.split(':').map(Number)
  return dayjs().hour(hour).minute(minute).second(0).millisecond(0)
}

function lastRunText(task: ScheduledTask): string {
  if (task.last_error_detail) return task.last_error_detail
  if (task.last_task_id) return `Task #${task.last_task_id} · ${task.last_result || '已提交执行'}`
  return '尚未触发'
}

export default function ScheduledTasks() {
  const { context, setViewState } = usePageContext()
  const routeView: ScheduleView = context.view_state.view === 'wakeups' ? 'wakeups' : 'automations'
  const routeStatus = context.view_state.status || ''
  const [items, setItems] = useState<ScheduledTask[]>([])
  const [view, setView] = useState<ScheduleView>(routeView)
  const [status, setStatus] = useState(routeStatus)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ScheduledTask | null>(null)
  const [selected, setSelected] = useState<ScheduledTask | null>(null)
  const [form] = Form.useForm<FormValue>()
  const scheduleType = Form.useWatch('schedule_type', form)

  useEffect(() => {
    setView((current) => current === routeView ? current : routeView)
    setStatus((current) => current === routeStatus ? current : routeStatus)
  }, [routeStatus, routeView])

  const load = useCallback((signal?: AbortSignal) => {
    setLoading(true)
    listScheduledTasks(status, signal)
      .then((data) => setItems(data.items))
      .catch((cause) => {
        if (!signal?.aborted) message.error(`加载自动化失败：${errorText(cause)}`)
      })
      .finally(() => { if (!signal?.aborted) setLoading(false) })
  }, [status])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  useEffect(() => {
    if (!items.some((item) => item.status === 'running')) return
    const timer = window.setInterval(() => load(), 3000)
    return () => window.clearInterval(timer)
  }, [items, load])

  const displayedItems = useMemo(
    () => items.filter((item) => view === 'wakeups'
      ? item.dispatch_kind === 'resume_task'
      : item.dispatch_kind === 'create_task'),
    [items, view],
  )

  const counts = useMemo(() => ({
    automations: items.filter((item) => item.dispatch_kind === 'create_task').length,
    wakeups: items.filter((item) => item.dispatch_kind === 'resume_task').length,
  }), [items])

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({
      title: '', instruction: '', context_snapshot: '{}',
      schedule_type: 'daily', daily_time: dailyTimeValue('09:00'),
      interval_minutes: 10, run_at: dayjs().add(10, 'minute'), enabled: true,
    })
    setModalOpen(true)
  }

  const openEdit = (task: ScheduledTask) => {
    setEditing(task)
    form.setFieldsValue({
      title: task.title,
      instruction: task.instruction,
      context_snapshot: JSON.stringify(task.context_snapshot ?? {}, null, 2),
      schedule_type: task.schedule_type,
      daily_time: task.daily_time ? dailyTimeValue(task.daily_time) : undefined,
      interval_minutes: task.interval_minutes ?? undefined,
      run_at: task.run_at ? dayjs(task.run_at) : undefined,
      enabled: task.enabled,
    })
    setModalOpen(true)
  }

  const save = async () => {
    try {
      const value = await form.validateFields()
      const input = toInput(value)
      setSaving(true)
      if (editing) {
        await updateScheduledTask(editing.id, input)
        message.success('自动化已更新')
      } else {
        await createScheduledTask(input)
        message.success('自动化已创建')
      }
      setModalOpen(false)
      load()
    } catch (cause) {
      if (cause && typeof cause === 'object' && 'errorFields' in cause) return
      message.error(`保存失败：${errorText(cause)}`)
    } finally {
      setSaving(false)
    }
  }

  const trigger = async (task: ScheduledTask) => {
    try {
      await triggerScheduledTask(task.id)
      message.success(task.schedule_type === 'once'
        ? `已触发“${task.title}”`
        : `已触发“${task.title}”，原计划不变`)
      load()
    } catch (cause) {
      message.error(`触发失败：${errorText(cause)}`)
    }
  }

  const remove = async (task: ScheduledTask) => {
    try {
      await deleteScheduledTask(task.id)
      message.success('自动化已删除')
      setSelected((current) => current?.id === task.id ? null : current)
      load()
    } catch (cause) {
      message.error(`删除失败：${errorText(cause)}`)
    }
  }

  const automationColumns: TableColumnsType<ScheduledTask> = [
    {
      title: '自动化', dataIndex: 'title', width: 360,
      render: (value: string, task) => (
        <div className="automation-title-cell">
          <Space size={6} wrap>
            <Text strong>{value}</Text>
            {!task.enabled && <Tag>已停用</Tag>}
          </Space>
          <Paragraph type="secondary" ellipsis={{ rows: 1, tooltip: task.instruction }}>
            {task.instruction}
          </Paragraph>
        </div>
      ),
    },
    {
      title: '计划', width: 190,
      render: (_, task) => (
        <Space orientation="vertical" size={0}>
          <Text>{scheduleText(task)}</Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            {task.enabled && task.status !== 'completed' ? `下次 ${formatDateTime(task.next_run_at)}` : '暂无下次执行'}
          </Text>
        </Space>
      ),
    },
    {
      title: '运行', width: 180,
      render: (_, task) => (
        <Space orientation="vertical" size={2}>
          <Tag color={statusMeta[task.status].color}>{statusMeta[task.status].label}</Tag>
          <Text
            type={task.last_error_detail ? 'danger' : 'secondary'}
            ellipsis={{ tooltip: lastRunText(task) }}
            className="automation-last-run"
          >
            {lastRunText(task)}
          </Text>
        </Space>
      ),
    },
    {
      title: '操作', fixed: 'right', width: 176,
      render: (_, task) => (
        <Space size={4} onClick={(event) => event.stopPropagation()}>
          <Button
            size="small"
            icon={<ThunderboltOutlined />}
            disabled={task.status === 'running'}
            onClick={() => trigger(task)}
          >
            立即运行
          </Button>
          <Tooltip title="编辑">
            <Button
              size="small"
              aria-label="编辑自动化"
              icon={<EditOutlined />}
              disabled={task.status === 'running'}
              onClick={() => openEdit(task)}
            />
          </Tooltip>
          <Popconfirm title="删除这条自动化？" okText="删除" cancelText="取消" onConfirm={() => remove(task)}>
            <Button size="small" aria-label="删除自动化" danger icon={<DeleteOutlined />} disabled={task.status === 'running'} />
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const wakeupColumns: TableColumnsType<ScheduledTask> = [
    {
      title: '系统任务', dataIndex: 'title', width: 390,
      render: (value: string, task) => (
        <div className="automation-title-cell">
          <Space size={6} wrap>
            <Text strong>{value}</Text>
            <Tag color="purple">系统等待</Tag>
            {task.subject_id && <Tag>Task #{task.subject_id}</Tag>}
          </Space>
          <Paragraph type="secondary" ellipsis={{ rows: 2, tooltip: task.instruction }}>
            {task.instruction}
          </Paragraph>
        </div>
      ),
    },
    {
      title: '唤醒时间', dataIndex: 'next_run_at', width: 150,
      render: (value: string, task) => task.enabled && task.status !== 'completed' ? formatDateTime(value) : '—',
    },
    {
      title: '状态', dataIndex: 'status', width: 110,
      render: (value: ScheduledTaskStatus) => <Tag color={statusMeta[value].color}>{statusMeta[value].label}</Tag>,
    },
    {
      title: '最近结果', width: 240,
      render: (_, task) => (
        <Text
          type={task.last_error_detail ? 'danger' : 'secondary'}
          ellipsis={{ tooltip: lastRunText(task) }}
        >
          {lastRunText(task)}
        </Text>
      ),
    },
  ]

  return (
    <div>
      <PageHeader title="自动化" subtitle="你创建的计划与任务执行产生的系统任务分开管理。">
        {view === 'automations' && <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建自动化</Button>}
      </PageHeader>

      <Card className="automation-toolbar" variant="borderless">
        <Flex align="center" justify="space-between" gap={16} wrap>
          <Segmented<ScheduleView>
            value={view}
            options={viewOptions.map((option) => ({
              ...option,
              label: `${option.label} ${counts[option.value]}`,
            }))}
            onChange={(nextView) => {
              setView(nextView)
              setSelected(null)
              setViewState({ view: nextView, status })
            }}
          />
          <Space>
            <Text type="secondary">状态</Text>
            <Select
              value={status}
              onChange={(nextStatus) => {
                setStatus(nextStatus)
                setViewState({ view, status: nextStatus })
              }}
              style={{ width: 130 }}
              options={[
                { value: '', label: '全部状态' },
                { value: 'binding', label: '正在绑定' },
                { value: 'active', label: '等待调度' },
                { value: 'running', label: '触发中' },
                { value: 'completed', label: '已结束' },
              ]}
            />
          </Space>
        </Flex>
      </Card>

      {view === 'wakeups' && (
        <Alert
          className="wakeup-explanation"
          type="info"
          showIcon
          title="这些是任务执行中产生的等待点"
          description="到时后 Jarvis 会回到原 Task 继续执行。它们由系统管理，在这里只读展示，不作为普通自动化编辑。"
        />
      )}

      <Card className="table-card automation-table-card" variant="borderless">
        <Table<ScheduledTask>
          rowKey="id"
          size="small"
          loading={loading}
          columns={view === 'automations' ? automationColumns : wakeupColumns}
          dataSource={displayedItems}
          pagination={{ pageSize: 15, showSizeChanger: false }}
          scroll={{ x: view === 'automations' ? 906 : 890 }}
          locale={{ emptyText: view === 'automations' ? '还没有自动化' : '当前没有系统任务' }}
          onRow={(task) => ({
            onClick: () => setSelected(task),
            onKeyDown: (event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                setSelected(task)
              }
            },
            tabIndex: 0,
            role: 'button',
            className: 'clickable-row',
          })}
        />
      </Card>

      <Drawer
        title={selected?.title || '调度详情'}
        open={Boolean(selected)}
        size={640}
        onClose={() => setSelected(null)}
      >
        {selected && (
          <Space orientation="vertical" size={24} className="drawer-content">
            <Space wrap>
              <Tag color={selected.dispatch_kind === 'resume_task' ? 'purple' : 'green'}>
                {selected.dispatch_kind === 'resume_task' ? '系统任务' : '用户自动化'}
              </Tag>
              <Tag color={statusMeta[selected.status].color}>{statusMeta[selected.status].label}</Tag>
              {!selected.enabled && <Tag>已停用</Tag>}
            </Space>
            <section className="clue-detail-section">
              <Text type="secondary">到时后做什么</Text>
              <Paragraph className="automation-instruction">{selected.instruction}</Paragraph>
            </section>
            <Descriptions column={2} size="small">
              <Descriptions.Item label="执行计划">{scheduleText(selected)}</Descriptions.Item>
              <Descriptions.Item label="下次执行">{formatDateTime(selected.next_run_at)}</Descriptions.Item>
              <Descriptions.Item label="关联 Task">{selected.subject_id ? `#${selected.subject_id}` : '—'}</Descriptions.Item>
              <Descriptions.Item label="最近触发">{formatDateTime(selected.last_started_at)}</Descriptions.Item>
              <Descriptions.Item label="最近结果" span={2}>{lastRunText(selected)}</Descriptions.Item>
            </Descriptions>
            <details className="automation-technical-details">
              <summary>上下文背景</summary>
              <pre>{JSON.stringify(selected.context_snapshot ?? {}, null, 2)}</pre>
            </details>
            {selected.dispatch_kind === 'resume_task' && selected.dispatch_payload && (
              <details className="automation-technical-details">
                <summary>系统唤醒信息</summary>
                <pre>{JSON.stringify(selected.dispatch_payload, null, 2)}</pre>
              </details>
            )}
          </Space>
        )}
      </Drawer>

      <Modal
        title={editing ? '修改自动化' : '新建自动化'}
        open={modalOpen}
        onCancel={() => setModalOpen(false)}
        onOk={save}
        confirmLoading={saving}
        okText="保存"
        cancelText="取消"
        width={720}
      >
        <Form form={form} layout="vertical">
          <Form.Item name="title" label="标题" rules={[{ required: true, whitespace: true, message: '请输入标题' }]}>
            <Input placeholder="例如：每天检查 Agent Runtime 项目进展" />
          </Form.Item>
          <Form.Item name="schedule_type" label="执行周期" rules={[{ required: true }]}>
            <Select options={[
              { value: 'once', label: '指定时间执行一次' },
              { value: 'daily', label: '每天指定时间' },
              { value: 'interval', label: '每隔 N 分钟' },
            ]} />
          </Form.Item>
          {scheduleType === 'once' ? (
            <Form.Item name="run_at" label="执行时间" rules={[{ required: true, message: '请选择执行时间' }]}>
              <DatePicker showTime format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
            </Form.Item>
          ) : scheduleType === 'daily' ? (
            <Form.Item name="daily_time" label="每天执行时间（本机时区）" rules={[{ required: true, message: '请选择执行时间' }]}>
              <TimePicker format="HH:mm" minuteStep={1} style={{ width: '100%' }} />
            </Form.Item>
          ) : (
            <Form.Item name="interval_minutes" label="执行间隔（分钟）" rules={[{ required: true, message: '请输入执行间隔' }]}>
              <InputNumber min={1} precision={0} style={{ width: '100%' }} addonAfter="分钟" />
            </Form.Item>
          )}
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="instruction" label="到时后做什么" rules={[{ required: true, whitespace: true, message: '请输入任务指令' }]}>
            <Input.TextArea autoSize={{ minRows: 5, maxRows: 12 }} placeholder="描述每次到点后要交给 Jarvis 完成的事" />
          </Form.Item>
          <details className="automation-context-editor">
            <summary>高级设置 · 冻结上下文</summary>
            <Text type="secondary">系统会在每次触发时完整交给执行者。只有确实需要固定项目、人物、会话或历史判断时再修改。</Text>
            <Form.Item
              name="context_snapshot"
              label="上下文背景（JSON）"
              rules={[{ required: true, whitespace: true, message: '请输入 JSON 对象，至少填写 {}' }]}
            >
              <Input.TextArea autoSize={{ minRows: 6, maxRows: 16 }} className="mono" />
            </Form.Item>
          </details>
        </Form>
      </Modal>
    </div>
  )
}
