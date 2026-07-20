import { useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Flex, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { executeTask, finishTask, listTasks, rerunTask } from './api'
import type { Task, TaskStatus } from './types'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { taskStatusMeta as statusMeta } from './status'

const { Paragraph, Text } = Typography

// External actions reach outside this machine and cannot be auto-run; the
// backend still requires the click, but we warn before triggering.
const externalActions = new Set(['summary_post', 'reply_message', 'schedule_meeting', 'doc_write', 'manual_followup'])

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function Tasks() {
  const [statuses, setStatuses] = useState<TaskStatus[]>(['pending', 'executing', 'done', 'failed'])
  const [items, setItems] = useState<Task[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshKey, setRefreshKey] = useState(0)
  const [detail, setDetail] = useState<Task>()
  const [selected, setSelected] = useState<Task>()
  const [finishStatus, setFinishStatus] = useState<'done' | 'failed'>('done')
  const [summary, setSummary] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [executingId, setExecutingId] = useState<number>()

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

  const runExecute = async (task: Task) => {
    if (externalActions.has(task.action_type)) {
      const ok = window.confirm(`「${task.title}」是对外动作（${task.action_type}），执行会真实触达外部。确认由 codex 执行？`)
      if (!ok) return
    }
    setExecutingId(task.id)
    setError(undefined)
    try {
      await executeTask(task.id)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const runRerun = async (task: Task) => {
    const ok = window.confirm(`「${task.title}」已${statusMeta[task.status].label}，确认重新执行一次？`)
    if (!ok) return
    if (externalActions.has(task.action_type)) {
      const okExternal = window.confirm(`该任务是对外动作（${task.action_type}），重跑会再次真实触达外部。继续？`)
      if (!okExternal) return
    }
    setExecutingId(task.id)
    setError(undefined)
    try {
      await rerunTask(task.id)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setExecutingId(undefined)
    }
  }

  const columns: TableColumnsType<Task> = [
    { title: '任务', dataIndex: 'title', render: (_, task) => <Space direction="vertical" size={2}><Text strong>{task.title}</Text><Text type="secondary">Todo #{task.todo_id} · {task.action_type}</Text></Space> },
    { title: '状态', dataIndex: 'status', width: 110, render: (status: TaskStatus) => <StatusBadge label={statusMeta[status].label} color={statusMeta[status].color} /> },
    { title: '方案', width: 320, render: (_, task) => <pre className="inline-json">{JSON.stringify(task.plan, null, 2)}</pre> },
    { title: '结果', width: 260, render: (_, task) => task.execution_result ? <pre className="inline-json">{JSON.stringify(task.execution_result, null, 2)}</pre> : '—' },
    {
      title: '操作', width: 220, render: (_, task) => {
        if (task.status === 'pending') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button type="primary" size="small" loading={executingId === task.id} onClick={(e) => { e.stopPropagation(); runExecute(task) }}>执行</Button>
            <Button size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'done') }}>手动完成</Button>
            <Button danger size="small" onClick={(e) => { e.stopPropagation(); openFinish(task, 'failed') }}>失败</Button>
          </Space>
        }
        if (task.status === 'executing') return <StatusBadge label="codex 执行中…" color={statusMeta.executing.color} />
        if (task.status === 'done' || task.status === 'failed') {
          return <Space onClick={(e) => e.stopPropagation()}>
            <Button size="small" loading={executingId === task.id} onClick={(e) => { e.stopPropagation(); runRerun(task) }}>重跑</Button>
          </Space>
        }
        return '—'
      },
    },
  ]

  return <>
    <PageHeader title="任务执行" subtitle="已确认的可执行任务，点行查看方案与结果">
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </PageHeader>
    <Card className="filter-card" variant="borderless">
      <Flex gap={16} align="end" wrap>
        <label className="filter-field filter-status"><Text type="secondary">Task 状态</Text><Select mode="multiple" value={statuses} options={Object.entries(statusMeta).map(([value, meta]) => ({ value, label: meta.label }))} onChange={(values) => setStatuses(values.length ? values : ['pending', 'executing', 'done', 'failed'])} /></label>
      </Flex>
    </Card>
    {error && <Alert type="error" showIcon message="Task 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Task> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 1050 }} onRow={(task) => ({ onClick: () => setDetail(task), className: 'clickable-row' })} /></Card>
    <Drawer title={detail?.title || 'Task 详情'} open={Boolean(detail)} width={680} onClose={() => setDetail(undefined)}>
      {detail && <Space direction="vertical" size={20} className="drawer-content">
        <Space><StatusBadge label={statusMeta[detail.status].label} color={statusMeta[detail.status].color} /><Tag>{detail.action_type}</Tag></Space>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="Todo">#{detail.todo_id}</Descriptions.Item>
          <Descriptions.Item label="版本">v{detail.version}</Descriptions.Item>
          <Descriptions.Item label="确认人">{detail.confirmed_by || '—'}</Descriptions.Item>
          <Descriptions.Item label="确认时间">{detail.confirmed_at ? new Date(detail.confirmed_at).toLocaleString() : '—'}</Descriptions.Item>
          <Descriptions.Item label="自主模式">{detail.autonomy_mode || '—'}</Descriptions.Item>
          <Descriptions.Item label="项目">{detail.project_id != null ? `#${detail.project_id}` : '未关联'}</Descriptions.Item>
        </Descriptions>
        <section><Text type="secondary">执行方案</Text><pre>{JSON.stringify(detail.plan, null, 2)}</pre></section>
        <section><Text type="secondary">执行结果</Text>{detail.execution_result ? <pre>{JSON.stringify(detail.execution_result, null, 2)}</pre> : <Paragraph type="secondary" style={{ marginTop: 8 }}>尚未执行</Paragraph>}</section>
        <section><Text type="secondary">背景</Text><pre>{JSON.stringify(detail.background, null, 2)}</pre></section>
      </Space>}
    </Drawer>
    <Modal title={finishStatus === 'done' ? '记录完成结果' : '记录失败原因'} open={Boolean(selected)} confirmLoading={submitting} onOk={submit} onCancel={() => setSelected(undefined)} okText="提交">
      <Input.TextArea rows={5} value={summary} onChange={(event) => setSummary(event.target.value)} placeholder={finishStatus === 'done' ? '完成了什么、产物在哪里' : '失败原因和需要的后续处理'} />
    </Modal>
  </>
}
