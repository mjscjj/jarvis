import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, DatePicker, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography, message } from 'antd'
import type { TableColumnsType } from 'antd'
import { DeleteOutlined, EditOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import dayjs, { type Dayjs } from 'dayjs'
import { createScheduledTask, deleteScheduledTask, listScheduledTasks, triggerScheduledTask, updateScheduledTask } from './api'
import PageHeader from './components/PageHeader'
import type { ScheduledTask, ScheduledTaskInput, ScheduledTaskStatus } from './types'

const { Paragraph, Text } = Typography

const statusMeta: Record<ScheduledTaskStatus, { label: string; color: string }> = {
  pending: { label: '待执行', color: 'gold' },
  running: { label: '执行中', color: 'blue' },
  done: { label: '已完成', color: 'green' },
  failed: { label: '失败', color: 'red' },
}

interface FormValue {
  title: string
  instruction: string
  context_snapshot: string
  scheduled_at: Dayjs
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
  return {
    title: value.title.trim(),
    instruction: value.instruction.trim(),
    context_snapshot: context as Record<string, unknown>,
    scheduled_at: value.scheduled_at.toISOString(),
  }
}

export default function ScheduledTasks() {
  const [items, setItems] = useState<ScheduledTask[]>([])
  const [status, setStatus] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<ScheduledTask | null>(null)
  const [form] = Form.useForm<FormValue>()

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
      title: '', instruction: '', context_snapshot: '{}', scheduled_at: dayjs().add(10, 'minute'),
    })
    setModalOpen(true)
  }

  const openEdit = (task: ScheduledTask) => {
    setEditing(task)
    form.setFieldsValue({
      title: task.title,
      instruction: task.instruction,
      context_snapshot: JSON.stringify(task.context_snapshot ?? {}, null, 2),
      scheduled_at: dayjs(task.scheduled_at),
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
      message.success(`已触发“${task.title}”`)
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
          <Text strong>{value}</Text>
          <Paragraph type="secondary" ellipsis={{ rows: 2, expandable: true }} style={{ margin: '4px 0 0', fontSize: 12, whiteSpace: 'pre-wrap' }}>
            {task.instruction}
          </Paragraph>
        </div>
      ),
    },
    {
      title: '计划时间', dataIndex: 'scheduled_at', width: 180,
      render: (value: string) => new Date(value).toLocaleString(),
    },
    {
      title: '状态', dataIndex: 'status', width: 100,
      render: (value: ScheduledTaskStatus) => <Tag color={statusMeta[value].color}>{statusMeta[value].label}</Tag>,
    },
    {
      title: '结果', width: 320,
      render: (_, task) => task.error_detail ? (
        <Paragraph type="danger" ellipsis={{ rows: 3, expandable: true }} style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{task.error_detail}</Paragraph>
      ) : (
        <Paragraph ellipsis={{ rows: 3, expandable: true }} style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{task.result || '—'}</Paragraph>
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
      <PageHeader title="定时任务" subtitle="一次性任务；服务每 5 分钟扫描到期记录，并发交给 Codex 执行。">
        <Select
          value={status}
          onChange={setStatus}
          style={{ width: 120 }}
          options={[
            { value: '', label: '全部状态' },
            { value: 'pending', label: '待执行' },
            { value: 'running', label: '执行中' },
            { value: 'done', label: '已完成' },
            { value: 'failed', label: '失败' },
          ]}
        />
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>新建定时任务</Button>
      </PageHeader>
      <Table<ScheduledTask>
        rowKey="id" loading={loading} columns={columns} dataSource={items}
        pagination={{ pageSize: 20, showSizeChanger: false }} scroll={{ x: 1100 }}
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
            <Input placeholder="例如：周五跟进个人工作台录屏" />
          </Form.Item>
          <Form.Item name="scheduled_at" label="执行时间" rules={[{ required: true, message: '请选择执行时间' }]}>
            <DatePicker showTime format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="instruction" label="执行指令" rules={[{ required: true, whitespace: true, message: '请输入执行指令' }]}>
            <Input.TextArea autoSize={{ minRows: 5, maxRows: 12 }} placeholder="到时间后希望 Codex 真正完成什么" />
          </Form.Item>
          <Form.Item
            name="context_snapshot"
            label="上下文背景（JSON）"
            extra="创建时冻结，执行时完整交给 Codex。可放项目、人物、会话、链接和历史判断。"
            rules={[{ required: true, whitespace: true, message: '请输入 JSON 对象，至少填写 {}' }]}
          >
            <Input.TextArea autoSize={{ minRows: 8, maxRows: 18 }} className="mono" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
