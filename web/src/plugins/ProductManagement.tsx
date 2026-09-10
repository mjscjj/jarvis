import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Drawer, Space, Table, Tabs, Tag, Typography, message } from 'antd'
import { getSkillContent, getTask, listScheduledTasks, listSkills, updateSkill } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import ScheduledTasks from '../ScheduledTasks'
import { usePageContext } from '../pageContext'
import type { AgentSkill, AgentSkillContent, Plugin, Task } from '../types'
import { taskStatusMeta } from '../status'

export default function ProductManagement({ plugin }: { plugin: Plugin }) {
  const { context, navigate, setViewState, setSelection } = usePageContext()
  const [skills, setSkills] = useState<AgentSkill[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [content, setContent] = useState<AgentSkillContent>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [busy, setBusy] = useState<string>()
  const [messageApi, messageContext] = message.useMessage()
  const tab = context.view_state.product_tab || 'skills'
  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const [catalog, schedules] = await Promise.all([
        listSkills(signal), listScheduledTasks('', signal, plugin.id),
      ])
      const ids = [...new Set(schedules.items.flatMap((item) => item.last_task_id ? [item.last_task_id] : []))]
      const results = await Promise.allSettled(ids.map((id) => getTask(id, signal)))
      if (signal?.aborted) return
      setSkills(catalog.items.filter((item) => plugin.skills.includes(item.name)))
      setTasks(results.flatMap((result) => result.status === 'fulfilled' ? [result.value] : []))
      setError(results.some((result) => result.status === 'rejected') ? '部分执行记录读取失败，请刷新重试。' : undefined)
    } catch (cause) {
      if (!signal?.aborted) setError(String(cause))
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [plugin.id, plugin.skills])

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
    try { setContent(await getSkillContent(name)) }
    catch (cause) { messageApi.error(String(cause)) }
    finally { setBusy(undefined) }
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
        <Typography.Title level={3}>产品管理</Typography.Title>
        <Typography.Text type="secondary">Agent 阅读与 Review 产品文档、维护 Skills；这里查看能力、配置定时任务和追踪结果。</Typography.Text>
      </div>
      <Button loading={loading} onClick={() => void load()}>刷新</Button>
    </Space>
    {error && <Alert type="error" showIcon title={error} />}
    <Tabs activeKey={tab} onChange={(product_tab) => setViewState({ ...context.view_state, product_tab })} items={[
      { key: 'skills', label: 'Skills', children: <>
        <Typography.Paragraph type="secondary">Skills 由 Agent 根据任务反馈维护。需要优化时，把具体问题和执行记录交给 Agent；下次读取使用最新正文。</Typography.Paragraph>
        <Table<AgentSkill> rowKey="name" dataSource={skills} loading={loading} pagination={false} columns={[
          { title: 'Skill', dataIndex: 'name' },
          { title: '用途', dataIndex: 'description' },
          { title: '状态', render: (_, item) => <Tag color={item.is_enabled && item.is_available ? 'green' : 'default'}>{item.is_enabled && item.is_available ? '可用' : '未启用'}</Tag> },
          { title: '操作', render: (_, item) => <Space>
            <Button loading={busy === item.name} onClick={() => void showSkill(item.name)}>查看正文</Button>
            <Button disabled={Boolean(busy)} onClick={() => void toggle(item)}>{item.is_enabled ? '停用' : '启用'}</Button>
          </Space> },
        ]} />
      </> },
      { key: 'schedules', label: '定时任务', children: <ScheduledTasks delegationsEnabled={false} scope={scope} /> },
      { key: 'results', label: '最近执行', children: <>
        <Typography.Paragraph type="secondary">每条定时任务最近一次关联 Task 的真实状态。完整执行过程与历史继续在任务中心查看。</Typography.Paragraph>
        <Button style={{ marginBottom: 12 }} onClick={() => navigate('tasks')}>打开任务中心</Button>
        <Table<Task> rowKey="id" dataSource={tasks} loading={loading} pagination={{ pageSize: 10 }} columns={[
          { title: '任务', dataIndex: 'title' },
          { title: '状态', render: (_, item) => taskStatusMeta[item.status]?.label || item.status },
          { title: '结果', dataIndex: 'summary' },
          { title: '详情', render: (_, item) => <Button onClick={() => setSelection({ kind: 'task', id: item.id, label: item.title })}>查看 Task #{item.id}</Button> },
        ]} />
      </> },
    ]} />
    <Drawer title={content?.name} open={Boolean(content)} size={800} onClose={() => setContent(undefined)}>
      {content && <><Typography.Paragraph type="secondary">{content.path}</Typography.Paragraph><MarkdownReport content={content.content} /></>}
    </Drawer>
  </section>
}
