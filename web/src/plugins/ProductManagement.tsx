import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Drawer, Input, Modal, Space, Table, Tabs, Tag, Typography, message } from 'antd'
import { APIRequestError, getSkillSource, listSkills, listTasks, updateSkill, updateSkillSource } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import ScheduledTasks from '../ScheduledTasks'
import { usePageContext } from '../pageContext'
import type { AgentSkill, AgentSkillContent, Plugin, Task, TaskStatus } from '../types'
import { taskStatusMeta } from '../status'

const allTaskStatuses = Object.keys(taskStatusMeta) as TaskStatus[]

function formatTime(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false,
  }).format(new Date(value))
}

export default function ProductManagement({ plugin }: { plugin: Plugin }) {
  const { context, navigate, setViewState, setSelection } = usePageContext()
  const [skills, setSkills] = useState<AgentSkill[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [taskPage, setTaskPage] = useState(1)
  const [taskTotal, setTaskTotal] = useState(0)
  const [content, setContent] = useState<AgentSkillContent>()
  const [draft, setDraft] = useState('')
  const [editing, setEditing] = useState(false)
  const [saving, setSaving] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState<string>()
  const [messageApi, messageContext] = message.useMessage()
  const tab = context.view_state.product_tab || 'skills'
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const [catalog, history] = await Promise.all([
        listSkills(signal),
        listTasks(allTaskStatuses, taskPage, 10, signal, { plugin: plugin.id }),
      ])
      if (signal?.aborted) return
      setSkills(catalog.items.filter((item) => plugin.skills.includes(item.name)))
      setTasks(history.items)
      setTaskTotal(history.total)
      setError(undefined)
    } catch (cause) {
      if (!signal?.aborted) setError(String(cause))
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [plugin.id, plugin.skills, taskPage])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load, tab])

  const scope = useMemo(() => ({
    pluginID: plugin.id,
    skills: skills.map((item) => ({
      name: item.name, description: item.description,
      available: item.is_enabled && item.is_available && item.stages.includes('execute'),
    })),
  }), [plugin.id, skills])

  const showSkill = async (name: string) => {
    setBusy(name)
    try {
      const value = await getSkillSource(name)
      setContent(value)
      setDraft(value.content)
      setEditing(false)
    }
    catch (cause) { messageApi.error(String(cause)) }
    finally { setBusy(undefined) }
  }

  const saveSkill = async () => {
    if (!content) return
    setSaving(true)
    try {
      const value = await updateSkillSource(content.name, {
        content: draft,
        expected_revision: content.revision,
      })
      setContent(value)
      setDraft(value.content)
      setEditing(false)
      await load()
      messageApi.success('Skill Markdown 已保存，下次 Agent 读取时生效')
    } catch (cause) {
      if (cause instanceof APIRequestError && cause.status === 409) {
        messageApi.error('Skill 已被其他人或 Agent 修改；你的草稿仍保留，请复制后重新打开最新版本再合并')
      } else {
        messageApi.error(`保存失败：${String(cause)}`)
      }
    } finally {
      setSaving(false)
    }
  }

  const closeSkill = () => {
    setContent(undefined)
    setDraft('')
    setEditing(false)
  }

  const requestCloseSkill = () => {
    if (!editing || !content || draft === content.content) {
      closeSkill()
      return
    }
    Modal.confirm({
      title: '放弃未保存的修改？',
      content: '关闭后，这份 Markdown 草稿不会保留。',
      okText: '放弃修改',
      okButtonProps: { danger: true },
      cancelText: '继续编辑',
      onOk: closeSkill,
    })
  }

  const toggle = async (item: AgentSkill) => {
    setBusy(item.name)
    try {
      await updateSkill(item.name, { stages: item.stages, is_enabled: !item.is_enabled })
      await load()
    } catch (cause) { messageApi.error(String(cause)) }
    finally { setBusy(undefined) }
  }

  return <section>
    {messageContext}
    <Space style={{ width: '100%', justifyContent: 'space-between', marginBottom: 16 }}>
      <div>
        <Typography.Title level={3}>{plugin.name}</Typography.Title>
        <Typography.Text type="secondary">Agent 阅读与 Review 产品文档、维护 Skills；这里查看能力、配置定时任务和追踪结果。</Typography.Text>
      </div>
      <Button loading={loading} onClick={() => void load()}>刷新</Button>
    </Space>
    {error && <Alert type="error" showIcon title={error} />}
    <Tabs activeKey={tab} onChange={(product_tab) => setViewState({ ...context.view_state, product_tab })} items={[
      { key: 'skills', label: 'Skills', children: <>
        <Typography.Paragraph type="secondary">Skills 由 Agent 根据任务反馈持续维护，人也可以直接阅读和编辑同一份仓库 Markdown；保存后，Agent 下次读取使用最新正文。</Typography.Paragraph>
        <Table<AgentSkill> rowKey="name" dataSource={skills} loading={loading} pagination={false} columns={[
          { title: 'Skill', dataIndex: 'name' },
          { title: '用途', dataIndex: 'description' },
          { title: '状态', render: (_, item) => <Tag color={item.is_enabled && item.is_available ? 'green' : 'default'}>{item.is_enabled && item.is_available ? '可用' : '未启用'}</Tag> },
          { title: '操作', render: (_, item) => <Space>
            <Button loading={busy === item.name} onClick={() => void showSkill(item.name)}>查看 / 编辑</Button>
            <Button disabled={Boolean(busy)} onClick={() => void toggle(item)}>{item.is_enabled ? '停用' : '启用'}</Button>
          </Space> },
        ]} />
      </> },
      { key: 'schedules', label: '定时任务', children: <ScheduledTasks delegationsEnabled={false} scope={scope} /> },
      { key: 'results', label: '最近执行', children: <>
        <Typography.Paragraph type="secondary">产品流程计划创建的历史 Task 与真实状态。完整过程、报告链接和通知回执在任务详情中查看。</Typography.Paragraph>
        <Button style={{ marginBottom: 12 }} onClick={() => navigate('tasks')}>打开任务中心</Button>
        <Table<Task> rowKey="id" dataSource={tasks} loading={loading} pagination={{
          current: taskPage, pageSize: 10, total: taskTotal, showSizeChanger: false,
          onChange: (page) => setTaskPage(page),
        }} columns={[
          { title: '运行时间', dataIndex: 'created_at', width: 120, render: (value: string) => formatTime(value) },
          { title: '任务', dataIndex: 'title' },
          { title: '状态', render: (_, item) => taskStatusMeta[item.status]?.label || item.status },
          { title: '结果', dataIndex: 'summary' },
          { title: '详情', render: (_, item) => <Button onClick={() => setSelection({ kind: 'task', id: item.id, label: item.title })}>查看 Task #{item.id}</Button> },
        ]} />
      </> },
    ]} />
    <Drawer
      title={content?.name}
      open={Boolean(content)}
      size={800}
      onClose={requestCloseSkill}
      extra={content && (editing ? <Space>
        <Button disabled={saving} onClick={() => { setDraft(content.content); setEditing(false) }}>取消</Button>
        <Button type="primary" loading={saving} disabled={draft === content.content} onClick={() => void saveSkill()}>保存 Markdown</Button>
      </Space> : <Button onClick={() => setEditing(true)}>编辑 Markdown</Button>)}
    >
      {content && <>
        <Typography.Paragraph type="secondary">文件真源：{content.path}</Typography.Paragraph>
        {editing
          ? <Input.TextArea
              value={draft}
              onChange={(event) => setDraft(event.target.value)}
              autoSize={{ minRows: 24, maxRows: 48 }}
              style={{ fontFamily: 'var(--font-mono, monospace)', fontSize: 13 }}
              spellCheck={false}
            />
          : <MarkdownReport content={content.content} />}
      </>}
    </Drawer>
  </section>
}
