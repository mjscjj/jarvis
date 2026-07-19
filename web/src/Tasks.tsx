import { useEffect, useState } from 'react'
import { Alert, Button, Card, Flex, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { finishTask, listTasks } from './api'
import type { Task, TaskStatus } from './types'

const { Text } = Typography

const statusMeta: Record<TaskStatus, { label: string; color: string }> = {
  pending: { label: '待执行', color: 'blue' },
  done: { label: '已完成', color: 'green' },
  failed: { label: '失败', color: 'red' },
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function Tasks() {
  const [statuses, setStatuses] = useState<TaskStatus[]>(['pending'])
  const [items, setItems] = useState<Task[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshKey, setRefreshKey] = useState(0)
  const [selected, setSelected] = useState<Task>()
  const [finishStatus, setFinishStatus] = useState<'done' | 'failed'>('done')
  const [summary, setSummary] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    listTasks(statuses, 1, 100, controller.signal)
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [statuses, refreshKey])

  const openFinish = (task: Task, status: 'done' | 'failed') => {
    setSelected(task)
    setFinishStatus(status)
    setSummary('')
  }

  const submit = async () => {
    if (!selected || !summary.trim()) { setError('执行结果不能为空'); return }
    setSubmitting(true)
    try {
      const result = finishStatus === 'done' ? { summary: summary.trim() } : { error: summary.trim() }
      await finishTask(selected.id, selected.version, finishStatus, result)
      setSelected(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  const columns: TableColumnsType<Task> = [
    { title: '任务', dataIndex: 'title', render: (_, task) => <Space direction="vertical" size={2}><Text strong>{task.title}</Text><Text type="secondary">Todo #{task.todo_id} · {task.action_type}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: (status: TaskStatus) => <Tag color={statusMeta[status].color}>{statusMeta[status].label}</Tag> },
    { title: '方案', width: 320, render: (_, task) => <pre className="inline-json">{JSON.stringify(task.plan, null, 2)}</pre> },
    { title: '结果', width: 220, render: (_, task) => task.execution_result ? <pre className="inline-json">{JSON.stringify(task.execution_result, null, 2)}</pre> : '—' },
    { title: '操作', width: 170, render: (_, task) => task.status === 'pending' ? <Space><Button type="primary" size="small" onClick={() => openFinish(task, 'done')}>完成</Button><Button danger size="small" onClick={() => openFinish(task, 'failed')}>失败</Button></Space> : '—' },
  ]

  return <>
    <Flex justify="space-between" align="end" className="section-heading">
      <label className="filter-field"><Text type="secondary">Task 状态</Text><Select mode="multiple" value={statuses} options={Object.entries(statusMeta).map(([value, meta]) => ({ value, label: meta.label }))} onChange={setStatuses} /></label>
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </Flex>
    {error && <Alert type="error" showIcon message="Task 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Task> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 1050 }} /></Card>
    <Modal title={finishStatus === 'done' ? '记录完成结果' : '记录失败原因'} open={Boolean(selected)} confirmLoading={submitting} onOk={submit} onCancel={() => setSelected(undefined)} okText="提交">
      <Input.TextArea rows={5} value={summary} onChange={(event) => setSummary(event.target.value)} placeholder={finishStatus === 'done' ? '完成了什么、产物在哪里' : '失败原因和需要的后续处理'} />
    </Modal>
  </>
}
