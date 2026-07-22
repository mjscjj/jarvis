import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, TimePicker, Typography, message } from 'antd'
import type { TableColumnsType } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { createScheduledTask, deleteScheduledTask, listScheduledTasks, triggerScheduledTask, updateScheduledTask } from './api'
import PageHeader from './components/PageHeader'
import type { ScheduledTask, ScheduledTaskInput, ScheduledTaskScheduleType, ScheduledTaskStatus } from './types'

const { Paragraph, Text } = Typography

const statusMeta: Record<ScheduledTaskStatus, { label: string; color: string }> = {
  active: { label: '等待调度', color: 'green' },
  running: { label: '执行中', color: 'blue' },
}

interface FormValue {
  title: string
  instruction: string
  context_snapshot: string
  schedule_type: ScheduledTaskScheduleType
  daily_time?: Dayjs
  interval_minutes?: number
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
  return {
    title: value.title.trim(),
    instruction: value.instruction.trim(),
    context_snapshot: context as Record<string, unknown>,
    schedule_type: value.schedule_type,
    daily_time: value.schedule_type === 'daily' ? value.daily_time!.format('HH:mm') : null,
    interval_minutes: value.schedule_type === 'interval' ? value.interval_minutes! : null,
    enabled: value.enabled,
  }
}

function scheduleText(task: ScheduledTask): string {
  if (task.schedule_type === 'daily') return `每天 ${task.daily_time}`
  return `每 ${task.interval_minutes} 分钟`
}

function dailyTimeValue(value: string): Dayjs {
  const [hour, minute] = value.split(':').map(Number)
  return dayjs().hour(hour).minute(minute).second(0).millisecond(0)
}

export default function ScheduledTasks() {
  const [items, setItems] = useState<ScheduledTask[]>([])
  const [status, setStatus] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ScheduledTask | null>(null)
  const [form] = Form.useForm<FormValue>()
  const scheduleType = Form.useWatch('schedule_type', form)

  const load = useCallback((signal?: AbortSignal) => {
    setLoading(true)
    listScheduledTasks(status, signal)
      .then((data) => setItems(data.items))
      .catch((cause) => {
        if (!signal?.aborted) message.error(`加载定时任务失败：${errorText(cause)}`)
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

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({
      title: '', instruction: '', context_snapshot: '{}',
      schedule_type: 'daily', daily_time: dailyTimeValue('09:00'),
      interval_minutes: 10, enabled: true,
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
        message.success('定时任务已更新')
      } else {
        await createScheduledTask(input)
        message.success('定时任务已创建')
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
      message.success(`已触发“${task.title}”，原定时计划不变`)
      load()
    } catch (cause) {
      message.error(`触发失败：${errorText(cause)}`)
    }
  }

  const remove = async (task: ScheduledTask) => {
    try {
      await deleteScheduledTask(task.id)
      message.success('定时任务已删除')
      load()
    } catch (cause) {
      message.error(`删除失败：${errorText(cause)}`)
    }
  }

  const columns = useMemo<TableColumnsType<ScheduledTask>>(() => [
    {
      title: '任务', dataIndex: 'title', width: 240,
      render: (value: string, task) => (
        <div>
          <Space size={6}><Text strong>{value}</Text>{!task.enabled && <Tag>已停用</Tag>}</Space>
          <Paragraph type="secondary" ellipsis={{ rows: 2, expandable: true }} style={{ margin: '4px 0 0', fontSize: 12, whiteSpace: 'pre-wrap' }}>
            {task.instruction}
          </Paragraph>
        </div>
      ),
    },
    {
      title: '执行计划', width: 150,
      render: (_, task) => scheduleText(task),
    },
    {
      title: '下次执行', dataIndex: 'next_run_at', width: 180,
      render: (value: string, task) => task.enabled ? new Date(value).toLocaleString() : '—',
    },
    {
      title: '状态', dataIndex: 'status', width: 110,
      render: (value: ScheduledTaskStatus) => <Tag color={statusMeta[value].color}>{statusMeta[value].label}</Tag>,
    },
    {
      title: '最近结果', width: 320,
      render: (_, task) => task.last_error_detail ? (
        <Paragraph type="danger" ellipsis={{ rows: 3, expandable: true }} style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{task.last_error_detail}</Paragraph>
      ) : (
        <Paragraph ellipsis={{ rows: 3, expandable: true }} style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{task.last_result || '尚未执行'}</Paragraph>
      ),
    },
    {
      title: '操作', fixed: 'right', width: 230,
      render: (_, task) => (
        <Space size={4}>
          <Button size="small" icon={<ThunderboltOutlined />} disabled={task.status === 'running'} onClick={() => trigger(task)}>手动触发</Button>
          <Button size="small" icon={<EditOutlined />} disabled={task.status === 'running'} onClick={() => openEdit(task)} />
          <Popconfirm title="删除这条定时任务？" okText="删除" cancelText="取消" onConfirm={() => remove(task)}>
            <Button size="small" danger icon={<DeleteOutlined />} disabled={task.status === 'running'} />
          </Popconfirm>
        </Space>
      ),
    },
  ], [load])

  return (
    <div>
      <PageHeader title="定时任务" subtitle="周期任务；支持每天指定时间或每 N 分钟执行，并发交给 Codex 完成。">
        <Select
          value={status}
          onChange={setStatus}
          style={{ width: 130 }}
          options={[
            { value: '', label: '全部状态' },
            { value: 'active', label: '等待调度' },
            { value: 'running', label: '执行中' },
          ]}
        />
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建定时任务</Button>
      </PageHeader>
      <Table<ScheduledTask>
        rowKey="id" loading={loading} columns={columns} dataSource={items}
        pagination={{ pageSize: 20, showSizeChanger: false }} scroll={{ x: 1230 }}
      />

      <Modal
        title={editing ? '修改定时任务' : '新建定时任务'}
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
            <Select options={[{ value: 'daily', label: '每天指定时间' }, { value: 'interval', label: '每隔 N 分钟' }]} />
          </Form.Item>
          {scheduleType === 'daily' ? (
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
          <Form.Item name="instruction" label="执行指令" rules={[{ required: true, whitespace: true, message: '请输入执行指令' }]}>
            <Input.TextArea autoSize={{ minRows: 5, maxRows: 12 }} placeholder="每次到时间后希望 Codex 真正完成什么" />
          </Form.Item>
          <Form.Item
            name="context_snapshot"
            label="上下文背景（JSON）"
            extra="创建时冻结，每次执行都完整交给 Codex。可放项目、人物、会话、链接和历史判断。"
            rules={[{ required: true, whitespace: true, message: '请输入 JSON 对象，至少填写 {}' }]}
          >
            <Input.TextArea autoSize={{ minRows: 8, maxRows: 18 }} className="mono" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
