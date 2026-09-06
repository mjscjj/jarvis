import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Collapse,
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
  Spin,
  Switch,
  Table,
  Tag,
  Tabs,
  Tooltip,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { useAgentIdentity } from './agentIdentity'
import {
  appendProjectFact,
  closeKeyMatter,
  createKeyMatter,
  createPerson,
  createProject,
  createResource,
  deletePerson,
  deleteProject,
  deleteResource,
  duplicateProject,
  exportProject,
  getProfile,
  getSkillContent,
  listGroups,
  listKeyMatters,
  listPersons,
  listProjects,
  listResources,
  listSkills,
  importProject,
  previewProjectImport,
  resolveProjectRepositories,
  resolvePerson,
  scanSkills,
  touchKeyMatter,
  touchResource,
  updateGroupBackground,
  updateKeyMatter,
  updatePerson,
  updateProfile,
  updateProject,
  updateResource,
  updateSkill,
} from './api'
import { keyMatterToInput, replaceKeyMatter } from './keyMatters'
import { personToUpdateInput } from './persons'
import SharedMemory from './SharedMemory'
import AppModules from './AppModules'
import RuntimeSettings from './RuntimeSettings'
import SystemTasks from './SystemTasks'
import PageHeader from './components/PageHeader'
import FactTimeline from './world/FactTimeline'
import FactsPanel from './world/FactsPanel'
import SummaryPageEditor from './world/SummaryPageEditor'
import { summaryIndexLine } from './world/summary'
import { usePageContext } from './pageContext'
import type {
  AgentSkill,
  AgentSkillInput,
  Group,
  GroupBackgroundInput,
  KeyMatter,
  KeyMatterInput,
  Person,
  PersonUpdateInput,
  PersonRole,
  ProfileInput,
  ProfileView,
  Project,
  ProjectBundle,
  ProjectInput,
  RepositoryBinding,
  ProjectRole,
  ProjectStatus,
  ResolveCandidate,
  Resource,
  ResourceInput,
  ResourceType,
  SkillStage,
  WorkRuleStage,
} from './types'
import './styles/review-memory.css'

const { Text } = Typography
const WorldMap = lazy(() => import('./world-map/WorldMap'))

const projectRoleLabels: Record<ProjectRole, string> = { owner: '负责人', participant: '参与者' }
const projectStatusLabels: Record<ProjectStatus, string> = {
  planning: '规划中', active: '进行中', paused: '暂停', archived: '归档', done: '完成',
}
const personRoleLabels: Record<PersonRole, string> = {
  leader: 'Leader', key: '关键干系人', colleague: '同事', other: '其他',
}
const resourceTypeLabels: Record<ResourceType, string> = {
  doc: '文档', link: '链接', repo: '仓库', note: '笔记', other: '其他',
}
const workRuleStageLabels: Record<WorkRuleStage, string> = {
  extract: 'M3 抽取 Todo', execute: 'M5 执行',
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

// --- Projects ---

function ProjectsPanel() {
  const [items, setItems] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Project | null>(null)
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<ProjectInput>()
  const [detail, setDetail] = useState<Project>()
  const [eventOpen, setEventOpen] = useState(false)
  const [eventDescription, setEventDescription] = useState('')
  const [eventSubmitting, setEventSubmitting] = useState(false)
  const [eventRefresh, setEventRefresh] = useState(0)
  const [copyOpen, setCopyOpen] = useState(false)
  const [copying, setCopying] = useState(false)
  const [copySource, setCopySource] = useState<Project>()
  const [copyForm] = Form.useForm<{ name: string; code: string | null }>()
  const [repositories, setRepositories] = useState<RepositoryBinding[]>([])
  const [resolvingRepositories, setResolvingRepositories] = useState(false)

  const reload = useCallback(() => {
    setLoading(true)
    listProjects()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])
  useEffect(() => setRepositories([]), [detail?.id])

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({ name: '', role: 'participant', status: 'active', priority: 3, code: null })
    setOpen(true)
  }
  const openEdit = (project: Project) => {
    setEditing(project)
    form.setFieldsValue({
      name: project.name, role: project.role, status: project.status, priority: project.priority,
      code: project.code,
    })
    setOpen(true)
  }
  const submit = async () => {
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      const saved = editing ? await updateProject(editing.id, values) : await createProject(values)
      if (detail?.id === saved.id) setDetail(saved)
      setOpen(false)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const remove = async (project: Project) => {
    try {
      await deleteProject(project.id)
      if (detail?.id === project.id) setDetail(undefined)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  const recordEvent = async () => {
    if (!detail || !eventDescription.trim()) return
    setEventSubmitting(true)
    try {
      await appendProjectFact(detail.id, eventDescription.trim())
      setEventDescription('')
      setEventOpen(false)
      setEventRefresh((value) => value + 1)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setEventSubmitting(false)
    }
  }

  const downloadProject = async (project: Project) => {
    try {
      const bundle = await exportProject(project.id)
      const href = URL.createObjectURL(new Blob([JSON.stringify(bundle, null, 2)], { type: 'application/json' }))
      const link = document.createElement('a')
      link.href = href
      link.download = `${project.code || project.name}-project.json`
      link.click()
      URL.revokeObjectURL(href)
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  const chooseProjectBundle = () => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = 'application/json,.json'
    input.onchange = async () => {
      const file = input.files?.[0]
      if (!file) return
      try {
        const bundle = JSON.parse(await file.text()) as ProjectBundle
        const preview = await previewProjectImport(bundle)
        if (!preview.valid) throw new Error(preview.warnings.join('；') || '项目包存在冲突')
        Modal.confirm({
          title: `导入项目“${bundle.project.name}”？`,
          content: `将创建项目并导入 ${bundle.resources.length} 条资源，本机仓库路径不会从分享包导入。`,
          okText: '导入',
          cancelText: '取消',
          onOk: async () => {
            try {
              const created = await importProject(bundle)
              setDetail(created)
              reload()
            } catch (cause: unknown) {
              setError(errorText(cause))
              throw cause
            }
          },
        })
      } catch (cause: unknown) {
        setError(errorText(cause))
      }
    }
    input.click()
  }

  const openCopy = (project: Project) => {
    setCopySource(project)
    copyForm.setFieldsValue({ name: `${project.name} 副本`, code: null })
    setCopyOpen(true)
  }

  const copyProject = async () => {
    if (!copySource) return
    const values = await copyForm.validateFields()
    setCopying(true)
    try {
      const created = await duplicateProject(copySource.id, values.name, values.code || null)
      setCopyOpen(false)
      setDetail(created)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setCopying(false)
    }
  }

  const resolveRepositories = async (project: Project) => {
    setResolvingRepositories(true)
    try {
      const result = await resolveProjectRepositories(project.id)
      setRepositories(result.items)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setResolvingRepositories(false)
    }
  }

  const columns: TableColumnsType<Project> = [
    { title: '项目', dataIndex: 'name', render: (_, p) => <Text strong>{p.name}</Text> },
    { title: '角色', dataIndex: 'role', width: 100, render: (r: ProjectRole) => projectRoleLabels[r] },
    { title: '状态', dataIndex: 'status', width: 100, render: (s: ProjectStatus) => <Tag>{projectStatusLabels[s]}</Tag> },
    { title: '优先级', dataIndex: 'priority', width: 90 },
    { title: '长期事实', dataIndex: 'summary', ellipsis: true, render: (v: string | null) => summaryIndexLine(v) || '—' },
    {
      title: '操作', width: 200, render: (_, p) => (
        <Flex gap={8}>
          <Button size="small" onClick={(event) => { event.stopPropagation(); setDetail(p) }}>详情</Button>
          <Button size="small" onClick={(event) => { event.stopPropagation(); openEdit(p) }}>编辑</Button>
          <Popconfirm title="归档该项目？" onConfirm={() => remove(p)} okText="归档" cancelText="取消">
            <Button size="small" danger onClick={(event) => event.stopPropagation()}>归档</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个项目</Text>
      <Flex gap={8}><Button onClick={chooseProjectBundle}>导入项目</Button><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建项目</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon title="项目操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Project> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} onRow={(project) => ({ onClick: () => setDetail(project), className: 'clickable-row' })} /></Card>
    <Modal title={editing ? '编辑项目' : '新建项目'} open={open} confirmLoading={submitting} onOk={submit} onCancel={() => setOpen(false)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="name" label="项目名" rules={[{ required: true, message: '请输入项目名' }]}><Input /></Form.Item>
        <Flex gap={16}>
          <Form.Item name="role" label="我的角色" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Select options={Object.entries(projectRoleLabels).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
          <Form.Item name="status" label="状态" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Select options={Object.entries(projectStatusLabels).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
          <Form.Item name="priority" label="优先级(1-5)" rules={[{ required: true }]} style={{ width: 130 }}>
            <InputNumber min={1} max={5} style={{ width: '100%' }} />
          </Form.Item>
        </Flex>
        <Form.Item name="code" label="项目代号(可选)"><Input allowClear /></Form.Item>
      </Form>
    </Modal>
    <Drawer title={detail?.name || '项目详情'} open={Boolean(detail)} size={720} onClose={() => setDetail(undefined)}>
      {detail && <Space orientation="vertical" size={20} style={{ width: '100%' }}>
        <Flex gap={8}>
          <Button onClick={() => void downloadProject(detail)}>分享项目</Button>
          <Button onClick={() => openCopy(detail)}>复制项目</Button>
          <Button loading={resolvingRepositories} onClick={() => void resolveRepositories(detail)}>扫描 Codebase 仓库</Button>
        </Flex>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="状态"><Tag>{projectStatusLabels[detail.status]}</Tag></Descriptions.Item>
          <Descriptions.Item label="我的角色">{projectRoleLabels[detail.role]}</Descriptions.Item>
          <Descriptions.Item label="优先级">{detail.priority}</Descriptions.Item>
          <Descriptions.Item label="项目代号">{detail.code || '—'}</Descriptions.Item>
          <Descriptions.Item label="最近实质进展" span={2}>{detail.last_progress_at ? dayjs(detail.last_progress_at).format('YYYY-MM-DD HH:mm') : '—'}</Descriptions.Item>
        </Descriptions>
        <SummaryPageEditor type="project" id={detail.id} />
        {repositories.length > 0 && (
          <Card size="small" title="Codebase 仓库">
            <Space orientation="vertical">
              {repositories.map((repository) => (
                <Text key={repository.resource_id}>
                  {repository.title} · {repository.status === 'matched' ? repository.local_path : repository.status === 'ambiguous' ? '匹配到多个本地仓库' : '未找到本地仓库'}
                </Text>
              ))}
            </Space>
          </Card>
        )}
        <FactTimeline
          subject={{ type: 'project', id: detail.id }}
          title="项目事实"
          refreshToken={eventRefresh}
          extra={<Button size="small" type="primary" onClick={() => setEventOpen(true)}>记录进展</Button>}
        />
      </Space>}
    </Drawer>
    <Modal title="复制项目" open={copyOpen} confirmLoading={copying} onOk={copyProject} onCancel={() => setCopyOpen(false)} okText="复制">
      <Form form={copyForm} layout="vertical">
        <Form.Item name="name" label="新项目名" rules={[{ required: true, message: '请输入项目名' }]}><Input /></Form.Item>
        <Form.Item name="code" label="新项目代号（可选）"><Input allowClear /></Form.Item>
      </Form>
    </Modal>
    <Modal title="记录项目进展" open={eventOpen} confirmLoading={eventSubmitting} onOk={recordEvent} onCancel={() => setEventOpen(false)} okText="记录">
      <Input.TextArea rows={6} value={eventDescription} onChange={(event) => setEventDescription(event.target.value)} placeholder="写清楚发生了什么、当前结果和下一步。" />
    </Modal>
  </>
}

// --- Key matters ---

type KeyMatterField = 'status' | 'due_at'

interface KeyMatterCreateFields {
  title: string
  status?: string
  project_id?: number
  due_at?: Dayjs
}

function KeyMattersPanel() {
  const [items, setItems] = useState<KeyMatter[]>([])
  const [total, setTotal] = useState(0)
  const [maxOpen, setMaxOpen] = useState(10)
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [selected, setSelected] = useState<KeyMatter>()
  const [editing, setEditing] = useState<{ id: number; field: KeyMatterField }>()
  const [draftText, setDraftText] = useState('')
  const [draftDueAt, setDraftDueAt] = useState<Dayjs | null>(null)
  const [saving, setSaving] = useState(false)
  const [touchingId, setTouchingId] = useState<number>()
  const [form] = Form.useForm<KeyMatterCreateFields>()

  const reload = useCallback(() => {
    setLoading(true)
    listKeyMatters()
      .then((result) => {
        setItems(result.items)
        setTotal(result.total)
        setMaxOpen(result.max_open)
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(reload, [reload])
  useEffect(() => {
    listProjects()
      .then((result) => setProjects(result.items))
      .catch((cause: unknown) => setError(errorText(cause)))
  }, [])

  const openCreate = () => {
    form.setFieldsValue({ title: '', status: '', project_id: undefined, due_at: undefined })
    setOpen(true)
  }

  const openDetail = (matter: KeyMatter) => {
    setSelected(matter)
    form.setFieldsValue({
      title: matter.title,
      status: matter.status,
      project_id: matter.project_id ?? undefined,
      due_at: matter.due_at ? dayjs(matter.due_at) : undefined,
    })
  }

  const submit = async () => {
    const values = await form.validateFields()
    const input: KeyMatterInput = {
      title: values.title,
      status: values.status ?? '',
      project_id: values.project_id ?? null,
      due_at: values.due_at?.toISOString() ?? null,
    }
    setSubmitting(true)
    try {
      await createKeyMatter(input)
      setOpen(false)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  const saveDetail = async () => {
    if (!selected) return
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      const saved = await updateKeyMatter(selected.id, {
        title: values.title,
        status: values.status ?? '',
        project_id: values.project_id ?? null,
        due_at: values.due_at?.toISOString() ?? null,
      })
      setItems((current) => replaceKeyMatter(current, saved))
      setSelected(undefined)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  const beginEdit = (matter: KeyMatter, field: KeyMatterField) => {
    setEditing({ id: matter.id, field })
    setDraftText(matter.status)
    setDraftDueAt(field === 'due_at' && matter.due_at ? dayjs(matter.due_at) : null)
  }

  const cancelEdit = () => setEditing(undefined)

  const saveEdit = async (matter: KeyMatter) => {
    if (!editing || editing.id !== matter.id) return
    const patch: Partial<KeyMatterInput> = editing.field === 'status'
      ? { status: draftText }
      : { due_at: draftDueAt?.toISOString() ?? null }
    setSaving(true)
    try {
      const saved = await updateKeyMatter(matter.id, keyMatterToInput(matter, patch))
      setItems((current) => replaceKeyMatter(current, saved))
      if (selected?.id === saved.id) setSelected(saved)
      setEditing(undefined)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  const close = async (matter: KeyMatter) => {
    try {
      await closeKeyMatter(matter.id)
      setItems((current) => current.filter((item) => item.id !== matter.id))
      setTotal((current) => Math.max(0, current - 1))
      if (selected?.id === matter.id) setSelected(undefined)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  const touch = async (matter: KeyMatter) => {
    setTouchingId(matter.id)
    try {
      const saved = await touchKeyMatter(matter.id)
      setItems((current) => replaceKeyMatter(current, saved).sort((left, right) => (
        dayjs(right.last_active_at).valueOf() - dayjs(left.last_active_at).valueOf()
      )))
      if (selected?.id === saved.id) setSelected(saved)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setTouchingId(undefined)
    }
  }

  const textEditor = (matter: KeyMatter) => {
    if (editing?.id !== matter.id || editing.field !== 'status') {
      return (
        <Button
          type="text"
          size="small"
          className={`key-matter-edit-trigger${matter.status ? '' : ' is-empty'}`}
          title={matter.status || '点击填写'}
          onClick={(event) => { event.stopPropagation(); beginEdit(matter, 'status') }}
        >
          <span>{matter.status || '点击填写'}</span>
        </Button>
      )
    }
    return (
      <Space orientation="vertical" size={4} onClick={(event) => event.stopPropagation()} style={{ width: '100%' }}>
        <Input size="small" value={draftText} onChange={(event) => setDraftText(event.target.value)} onPressEnter={() => saveEdit(matter)} autoFocus />
        <Space size={4}>
          <Button size="small" type="primary" loading={saving} onClick={() => saveEdit(matter)}>保存</Button>
          <Button size="small" disabled={saving} onClick={cancelEdit}>取消</Button>
        </Space>
      </Space>
    )
  }

  const dueAtEditor = (matter: KeyMatter) => {
    if (editing?.id !== matter.id || editing.field !== 'due_at') {
      const dueAt = matter.due_at ? dayjs(matter.due_at) : null
      const overdue = dueAt?.isBefore(dayjs()) ?? false
      return (
        <Button
          type="text"
          size="small"
          className={`key-matter-due-trigger${overdue ? ' is-overdue' : ''}${dueAt ? '' : ' is-empty'}`}
          title={dueAt ? `截止 ${dueAt.format('YYYY-MM-DD HH:mm')}` : '点击设置截止时间'}
          onClick={(event) => { event.stopPropagation(); beginEdit(matter, 'due_at') }}
        >
          {dueAt ? <>截止 <strong>{dueAt.format('MM-DD HH:mm')}</strong></> : '设置截止时间'}
        </Button>
      )
    }
    return (
      <Space orientation="vertical" size={4} className="key-matter-due-editor" onClick={(event) => event.stopPropagation()}>
        <DatePicker size="small" showTime value={draftDueAt} onChange={setDraftDueAt} format="YYYY-MM-DD HH:mm" autoFocus />
        <Space size={4}>
          <Button size="small" type="primary" loading={saving} onClick={() => saveEdit(matter)}>保存</Button>
          <Button size="small" disabled={saving} onClick={cancelEdit}>取消</Button>
        </Space>
      </Space>
    )
  }

  const columns: TableColumnsType<KeyMatter> = [
    {
      title: '关键事项',
      dataIndex: 'title',
      render: (_, matter) => {
        const summary = summaryIndexLine(matter.summary)
        return (
          <div className="key-matter-primary">
            <div className="key-matter-title-row">
              <Text strong className="key-matter-title" title={matter.title}>{matter.title}</Text>
              {matter.project && <Tag bordered={false} className="key-matter-project" title={matter.project.name}>{matter.project.name}</Tag>}
            </div>
            <Text
              type="secondary"
              className={`key-matter-summary${summary ? '' : ' is-empty'}`}
              title={summary || '暂无长期事实'}
            >
              {summary || '暂无长期事实'}
            </Text>
          </div>
        )
      },
    },
    { title: '当前状态', dataIndex: 'status', width: 220, render: (_, matter) => textEditor(matter) },
    {
      title: '时间节点',
      dataIndex: 'due_at',
      width: 172,
      render: (_, matter) => (
        <div className="key-matter-timeline">
          {dueAtEditor(matter)}
          <Text type="secondary" className="key-matter-active-time">
            活跃于 {dayjs(matter.last_active_at).format('MM-DD HH:mm')}
          </Text>
        </div>
      ),
    },
    {
      title: '操作', width: 144, render: (_, matter) => (
        <Flex gap={0} wrap={false} className="key-matter-actions">
          <Button type="link" size="small" loading={touchingId === matter.id} onClick={(event) => { event.stopPropagation(); touch(matter) }}>活跃</Button>
          <Button type="link" size="small" onClick={(event) => { event.stopPropagation(); openDetail(matter) }}>查看</Button>
          <Popconfirm title="闭环该关键事项？" onConfirm={() => close(matter)} okText="闭环" cancelText="取消">
            <Button type="link" size="small" danger onClick={(event) => event.stopPropagation()}>闭环</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">未闭环关键事项 {total}/{maxOpen}</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate} disabled={total >= maxOpen}>新建关键事项</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon title="关键事项操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card key-matter-list-card" variant="borderless">
      <Table<KeyMatter>
        className="key-matter-table"
        rowKey="id"
        columns={columns}
        dataSource={items}
        loading={loading}
        pagination={false}
        tableLayout="fixed"
        scroll={{ x: 820 }}
        onRow={(matter) => ({
          onClick: () => openDetail(matter),
          onKeyDown: (event) => { if (event.key === 'Enter') openDetail(matter) },
          className: 'clickable-row', tabIndex: 0,
        })}
      />
    </Card>
    <Drawer
      title={selected?.title || '关键事项详情'}
      open={Boolean(selected)}
      size={680}
      onClose={() => setSelected(undefined)}
      destroyOnHidden
      footer={<Flex justify="flex-end" gap={8}><Button onClick={() => setSelected(undefined)}>关闭</Button><Button type="primary" loading={submitting} onClick={saveDetail}>保存</Button></Flex>}
    >
      {selected && (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <Form form={form} layout="vertical">
            <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入关键事项标题' }]}><Input /></Form.Item>
            <Form.Item name="status" label="状态"><Input placeholder="自由文本，如：等法务回复" /></Form.Item>
            <Form.Item name="project_id" label="关联项目（可选）">
              <Select allowClear options={projects.map((project) => ({ value: project.id, label: project.name }))} />
            </Form.Item>
            <Form.Item name="due_at" label="截止时间（可选）"><DatePicker showTime format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} /></Form.Item>
          </Form>
          <Descriptions className="world-detail-descriptions" column={1} size="small" bordered>
            <Descriptions.Item label="最近实质进展">{selected.last_progress_at ? dayjs(selected.last_progress_at).format('YYYY-MM-DD HH:mm') : '—'}</Descriptions.Item>
            <Descriptions.Item label="最近活跃">{dayjs(selected.last_active_at).format('YYYY-MM-DD HH:mm')}</Descriptions.Item>
          </Descriptions>
          <SummaryPageEditor type="key_matter" id={selected.id} />
          <FactTimeline subject={{ type: 'key_matter', id: selected.id }} title="关键事项事实" />
        </Space>
      )}
    </Drawer>
    <Modal title="新建关键事项" open={open} confirmLoading={submitting} onOk={submit} onCancel={() => setOpen(false)} okText="创建" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入关键事项标题' }]}><Input /></Form.Item>
        <Form.Item name="status" label="状态"><Input placeholder="自由文本，如：等法务回复" /></Form.Item>
        <Form.Item name="project_id" label="关联项目（可选）">
          <Select allowClear options={projects.map((project) => ({ value: project.id, label: project.name }))} />
        </Form.Item>
        <Form.Item name="due_at" label="截止时间（可选）"><DatePicker showTime format="YYYY-MM-DD HH:mm" style={{ width: '100%' }} /></Form.Item>
      </Form>
    </Modal>
  </>
}

// --- Persons ---

const roleDefaultWeight: Record<PersonRole, number> = { leader: 1.0, key: 0.7, colleague: 0.4, other: 0.1 }

type PersonRoleFilter = 'all' | PersonRole

function PersonsPanel() {
  const [items, setItems] = useState<Person[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Person | null>(null)
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [roleFilter, setRoleFilter] = useState<PersonRoleFilter>('all')
  const [savingId, setSavingId] = useState<number>()
  const [form] = Form.useForm<PersonUpdateInput>()
  const [boundOpenID, setBoundOpenID] = useState('')
  const [boundP2PChatID, setBoundP2PChatID] = useState('')
  const [query, setQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const [candidates, setCandidates] = useState<ResolveCandidate[] | null>(null)
  const [hasMore, setHasMore] = useState(false)

  const reload = useCallback(() => {
    setLoading(true)
    listPersons()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  // patchPerson performs an inline single-field update straight from the list.
  const patchPerson = async (person: Person, patch: Partial<PersonUpdateInput>) => {
    setSavingId(person.id)
    try {
      const saved = await updatePerson(person.id, { ...personToUpdateInput(person), ...patch })
      setItems((current) => current.map((item) => item.id === saved.id ? saved : item))
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingId(undefined)
    }
  }

  const visibleItems = roleFilter === 'all' ? items : items.filter((p) => p.role === roleFilter)

  const resetResolve = () => {
    setQuery('')
    setCandidates(null)
    setHasMore(false)
    setBoundOpenID('')
    setBoundP2PChatID('')
  }
  const openCreate = () => {
    setEditing(null)
    resetResolve()
    form.setFieldsValue({ name: '', role: 'colleague', priority_weight: 0.4, department: null, title: null, is_active: true })
    setOpen(true)
  }
  const openEdit = (person: Person) => {
    setEditing(person)
    resetResolve()
    setBoundOpenID(person.open_id)
    setBoundP2PChatID(person.p2p_chat_id || '')
    form.setFieldsValue({
      name: person.name, role: person.role, priority_weight: person.priority_weight,
      department: person.department, title: person.title, is_active: person.is_active,
    })
    setOpen(true)
  }
  const runResolve = async () => {
    if (!query.trim()) return
    setSearching(true)
    try {
      const result = await resolvePerson(query.trim())
      setCandidates(result.candidates)
      setHasMore(result.has_more)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSearching(false)
    }
  }
  const pickCandidate = (candidate: ResolveCandidate) => {
    setBoundOpenID(candidate.open_id)
    setBoundP2PChatID(candidate.p2p_chat_id || '')
    const role = (form.getFieldValue('role') as PersonRole) || 'colleague'
    form.setFieldsValue({
      name: candidate.name, department: candidate.department || null,
      priority_weight: form.getFieldValue('priority_weight') ?? roleDefaultWeight[role],
    })
    setCandidates(null)
  }
  const submit = async () => {
    const values = await form.validateFields()
    if (!editing && !boundOpenID) { setError('请先搜索并选择一个飞书用户'); return }
    setSubmitting(true)
    try {
      if (editing) {
        const saved = await updatePerson(editing.id, values)
        setItems((current) => current.map((item) => item.id === saved.id ? saved : item))
      } else {
        await createPerson({ ...values, open_id: boundOpenID, p2p_chat_id: boundP2PChatID || null })
        reload()
      }
      setOpen(false)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const remove = async (person: Person) => {
    try {
      await deletePerson(person.id)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  const columns: TableColumnsType<Person> = [
    {
      title: '姓名', dataIndex: 'name',
      render: (_, p) => <Text strong>{p.name}</Text>,
    },
    {
      title: '角色', dataIndex: 'role', width: 140,
      // 行内直接改角色，同时把权重联动为该角色默认值（规范：列表页优先行内编辑）。
      render: (r: PersonRole, p) => (
        <Select<PersonRole> size="small" variant="borderless" value={r} disabled={savingId === p.id} style={{ width: 120 }}
          onClick={(event) => event.stopPropagation()}
          onChange={(role) => patchPerson(p, { role, priority_weight: roleDefaultWeight[role] })}
          options={Object.entries(personRoleLabels).map(([value, label]) => ({ value, label }))} />
      ),
    },
    { title: '权重', dataIndex: 'priority_weight', width: 80 },
    { title: '部门/职位', width: 200, render: (_, p) => [p.department, p.title].filter(Boolean).join(' · ') || '—' },
    { title: '长期事实', dataIndex: 'summary', ellipsis: true, render: (v: string | null) => summaryIndexLine(v) || '—' },
    {
      title: '启用', dataIndex: 'is_active', width: 70,
      render: (v: boolean, p) => <span onClick={(event) => event.stopPropagation()}><Switch size="small" checked={v} loading={savingId === p.id} onChange={(next) => patchPerson(p, { is_active: next })} /></span>,
    },
    {
      title: '操作', width: 150, render: (_, p) => (
        <Flex gap={8} onClick={(event) => event.stopPropagation()}>
          <Button size="small" onClick={() => openEdit(p)}>详情</Button>
          <Popconfirm title="删除该人物？" onConfirm={() => remove(p)} okText="删除" cancelText="取消">
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Flex gap={12} align="center">
        <Segmented<PersonRoleFilter>
          value={roleFilter}
          onChange={(value) => setRoleFilter(value)}
          options={[
            { value: 'all', label: '全部' },
            { value: 'leader', label: 'Leader' },
            { value: 'key', label: '关键干系人' },
            { value: 'colleague', label: '同事' },
          ]}
        />
        <Text type="secondary">{visibleItems.length} / {items.length} 人</Text>
      </Flex>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建人物</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon title="人物操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Person>
      rowKey="id" columns={columns} dataSource={visibleItems} loading={loading}
      pagination={{ pageSize: 20, showSizeChanger: true, pageSizeOptions: [20, 50, 100], hideOnSinglePage: visibleItems.length <= 20 }}
      scroll={{ x: 900 }}
      onRow={(person) => ({
        onClick: () => openEdit(person),
        onKeyDown: (event) => { if (event.key === 'Enter') openEdit(person) },
        className: 'clickable-row', tabIndex: 0,
      })}
    /></Card>
    <Drawer
      title={editing ? editing.name : '新建人物'}
      open={open}
      size={720}
      onClose={() => setOpen(false)}
      destroyOnHidden
      footer={<Flex justify="flex-end" gap={8}><Button onClick={() => setOpen(false)}>关闭</Button><Button type="primary" loading={submitting} onClick={submit}>保存</Button></Flex>}
    >
      {!editing && (
        <Card size="small" style={{ marginBottom: 16 }}>
          <Flex gap={8}>
            <Input value={query} onChange={(e) => setQuery(e.target.value)} onPressEnter={runResolve} placeholder="输入姓名或邮箱搜索飞书用户" allowClear />
            <Button type="primary" onClick={runResolve} loading={searching}>搜索</Button>
          </Flex>
          {hasMore && <Alert style={{ marginTop: 8 }} type="warning" showIcon title="结果过多，请补全姓名或改用邮箱缩小范围" />}
          {candidates && candidates.length === 0 && <Alert style={{ marginTop: 8 }} type="info" showIcon title="未找到匹配用户，换个关键词试试" />}
          {candidates && candidates.length > 0 && (
            <div style={{ marginTop: 8, maxHeight: 220, overflowY: 'auto' }}>
              {candidates.map((c) => (
                <Flex key={c.open_id} justify="space-between" align="center" style={{ padding: '6px 4px', borderBottom: '1px solid #f0f0f0' }}>
                  <div>
                    <Text strong>{c.name}</Text>{c.is_external && <Tag color="orange" style={{ marginLeft: 6 }}>外部</Tag>}
                    <div><Text type="secondary" style={{ fontSize: 12 }}>{[c.department, c.email].filter(Boolean).join(' · ') || c.open_id}</Text></div>
                  </div>
                  <Button size="small" type="link" onClick={() => pickCandidate(c)}>选择</Button>
                </Flex>
              ))}
            </div>
          )}
        </Card>
      )}
      <Form form={form} layout="vertical">
        <Flex gap={16}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, message: '请先搜索选择用户' }]} style={{ flex: 1 }}><Input disabled={!editing} /></Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]} style={{ width: 160 }}>
            <Select options={Object.entries(personRoleLabels).map(([value, label]) => ({ value, label }))} onChange={(role: PersonRole) => { if (!editing) form.setFieldValue('priority_weight', roleDefaultWeight[role]) }} />
          </Form.Item>
        </Flex>
        <Flex gap={16}>
          <Form.Item name="priority_weight" label="优先权重(0-1)" rules={[{ required: true }]} style={{ width: 160 }}>
            <InputNumber min={0} max={1} step={0.05} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="is_active" label="启用" valuePropName="checked" style={{ width: 100 }}><Switch /></Form.Item>
        </Flex>
        <Flex gap={16}>
          <Form.Item name="department" label="部门(可选)" style={{ flex: 1 }}><Input allowClear /></Form.Item>
          <Form.Item name="title" label="职位(可选)" style={{ flex: 1 }}><Input allowClear /></Form.Item>
        </Flex>
        <Collapse
          ghost
          className="memory-advanced"
          items={[{
            key: 'identity',
            label: '高级信息',
            children: (
              <div>
                <Text type="secondary">飞书用户标识</Text>
                <div><Text copyable={Boolean(boundOpenID)}>{boundOpenID || '搜索并选择用户后自动绑定'}</Text></div>
              </div>
            ),
          }]}
        />
      </Form>
      {editing && <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <SummaryPageEditor type="person" id={editing.id} />
        <FactTimeline subject={{ type: 'person', id: editing.id }} title="人物事实" />
      </Space>}
    </Drawer>
  </>
}

// --- Groups (background patch only) ---

const tierLabels: Record<string, string> = { hot: '热', warm: '温', cold: '冷' }
const tierColors: Record<string, string> = { hot: 'red', warm: 'orange', cold: 'default' }
const chatModeLabels: Record<string, string> = { group: '群聊', p2p: '单聊', topic: '话题' }
const scanStatusMeta: Record<string, { color: string; label: string }> = {
  ok: { color: 'green', label: '正常' },
  error: { color: 'red', label: '失败' },
}

function formatScanTime(value: string | null): string {
  if (!value) return '未扫描'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

// formatActiveTime renders last_active_at (ms epoch of the newest message) as a
// coarse relative label so the activity-desc ordering reads at a glance.
function formatActiveTime(ms: number | null): string {
  if (!ms) return '无活跃'
  const diff = Date.now() - ms
  if (diff < 0) return '刚刚'
  const minute = 60_000, hour = 60 * minute, day = 24 * hour
  if (diff < minute) return '刚刚'
  if (diff < hour) return `${Math.floor(diff / minute)} 分钟前`
  if (diff < day) return `${Math.floor(diff / hour)} 小时前`
  if (diff < 30 * day) return `${Math.floor(diff / day)} 天前`
  return new Date(ms).toLocaleDateString('zh-CN')
}

const PAGE_SIZE = 20

function GroupsPanel() {
  const [items, setItems] = useState<Group[]>([])
  const [total, setTotal] = useState(0)
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Group | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [togglingId, setTogglingId] = useState<number>()
  const [form] = Form.useForm<GroupBackgroundInput>()

  const [relatedOnly, setRelatedOnly] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [chatMode, setChatMode] = useState<string>()
  const [tier, setTier] = useState<string>()
  const [page, setPage] = useState(1)
  const [broadened, setBroadened] = useState(false)

  const reload = useCallback(() => {
    setLoading(true)
    listGroups({ page, pageSize: PAGE_SIZE, relatedOnly, keyword: keyword.trim() || undefined, chatMode, tier })
      .then((result) => { setItems(result.items); setTotal(result.total); setBroadened(result.broadened); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [page, relatedOnly, keyword, chatMode, tier])
  useEffect(reload, [reload])

  useEffect(() => {
    listProjects()
      .then((result) => setProjects(result.items))
      .catch((cause: unknown) => setError(errorText(cause)))
  }, [])

  const resetToFirstPage = () => setPage(1)

  const openEdit = (group: Group) => {
    setEditing(group)
    form.setFieldsValue({
      project_id: group.project_id, related_group: group.related_group, pinned: group.pinned,
      include_in_memory: group.include_in_memory, is_key_group: group.is_key_group,
    })
  }
  const submit = async () => {
    if (!editing) return
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      await updateGroupBackground(editing.id, { ...values, project_id: values.project_id ?? null })
      setEditing(null)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  // One-click monitor toggle. It patches only related_group while preserving the
  // group's other curated fields; flipping to true triggers an immediate scan
  // on the backend.
  const toggleRelated = async (group: Group, next: boolean) => {
    setTogglingId(group.id)
    try {
      await updateGroupBackground(group.id, {
        project_id: group.project_id,
        related_group: next,
        pinned: group.pinned,
        include_in_memory: group.include_in_memory,
        is_key_group: group.is_key_group,
      })
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setTogglingId(undefined)
    }
  }

  const columns: TableColumnsType<Group> = [
    { title: '会话', dataIndex: 'name', render: (_, g) => <Text strong>{g.name || '未命名会话'}</Text> },
    { title: '类型', dataIndex: 'chat_mode', width: 80, render: (m: string) => chatModeLabels[m] || m },
    { title: '分层', dataIndex: 'tier', width: 70, render: (t: string) => <Tag color={tierColors[t] || 'default'}>{tierLabels[t] || t}</Tag> },
    { title: '关联项目', width: 150, render: (_, g) => g.project?.name || '—' },
    { title: '关键群', dataIndex: 'is_key_group', width: 80, render: (v: boolean) => v ? <Tag color="volcano">是</Tag> : '—' },
    {
      title: '最近活跃', dataIndex: 'last_active_at', width: 110,
      render: (v: number | null) => (
        <Tooltip title={v ? new Date(v).toLocaleString('zh-CN', { hour12: false }) : '暂无消息活跃记录'}>
          <Text style={{ fontSize: 12 }} type={v ? undefined : 'secondary'}>{formatActiveTime(v)}</Text>
        </Tooltip>
      ),
    },
    {
      title: '最近扫描', width: 170, render: (_, g) => {
        if (!g.related_group) return <Text type="secondary">—</Text>
        const meta = g.last_scan_status ? scanStatusMeta[g.last_scan_status] : undefined
        return (
          <Flex vertical gap={2}>
            <Text style={{ fontSize: 12 }}>{formatScanTime(g.last_scan_at)}</Text>
            {meta && <Tag color={meta.color} style={{ marginInlineEnd: 0, width: 'fit-content' }}>{meta.label}</Tag>}
          </Flex>
        )
      },
    },
    { title: '消息数', dataIndex: 'message_count', width: 80, render: (v: number, g) => g.related_group ? v : <Text type="secondary">—</Text> },
    {
      title: '操作', width: 180, fixed: 'right', render: (_, g) => (
        <Flex gap={8} onClick={(event) => event.stopPropagation()}>
          {g.related_group ? (
            <Popconfirm title="移出监控？将停止采集该会话" onConfirm={() => toggleRelated(g, false)} okText="移出" cancelText="取消">
              <Button size="small" danger loading={togglingId === g.id}>移出监控</Button>
            </Popconfirm>
          ) : (
            <Button size="small" type="primary" loading={togglingId === g.id} onClick={() => toggleRelated(g, true)}>纳入监控</Button>
          )}
          <Button size="small" onClick={() => openEdit(g)}>详情</Button>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" gap={12} wrap className="section-heading">
      <Segmented
        value={relatedOnly ? 'related' : 'all'}
        onChange={(value) => { setRelatedOnly(value === 'related'); resetToFirstPage() }}
        options={[{ value: 'related', label: '已监控' }, { value: 'all', label: '全部会话' }]}
      />
      <Flex gap={8} wrap align="center">
        <Input.Search
          allowClear placeholder="搜索会话、群主或项目" style={{ width: 260 }}
          onSearch={(value) => { setKeyword(value); resetToFirstPage() }}
          onChange={(e) => { if (e.target.value === '') { setKeyword(''); resetToFirstPage() } }}
        />
        <Select
          allowClear placeholder="类型" style={{ width: 110 }} value={chatMode}
          onChange={(value) => { setChatMode(value); resetToFirstPage() }}
          options={Object.entries(chatModeLabels).map(([value, label]) => ({ value, label }))}
        />
        <Select
          allowClear placeholder="分层" style={{ width: 100 }} value={tier}
          onChange={(value) => { setTier(value); resetToFirstPage() }}
          options={Object.entries(tierLabels).map(([value, label]) => ({ value, label }))}
        />
        <Button onClick={reload} loading={loading}>刷新</Button>
      </Flex>
    </Flex>
    <Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
      {relatedOnly ? `已监控 ${total} 个会话（正在按调度增量采集）` : `全部 ${total} 个会话（由采集发现，纳入监控后才会采集消息）`}
    </Text>
    {broadened && (
      <Alert
        style={{ marginBottom: 8 }} type="info" showIcon
        title={`「已监控」中没有匹配，已在全部会话中搜索「${keyword}」，命中 ${total} 个`}
      />
    )}
    {error && <Alert type="error" showIcon title="会话背景操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Table<Group>
        rowKey="id" columns={columns} dataSource={items} loading={loading} scroll={{ x: 1000 }}
        onRow={(group) => ({
          onClick: () => openEdit(group),
          onKeyDown: (event) => { if (event.key === 'Enter') openEdit(group) },
          className: 'clickable-row', tabIndex: 0,
        })}
        locale={{
          emptyText: keyword
            ? <Flex vertical align="center" gap={8} style={{ padding: '24px 0' }}>
                <Text type="secondary">没有匹配「{keyword}」的会话</Text>
                {relatedOnly && <Button size="small" onClick={() => { setRelatedOnly(false); resetToFirstPage() }}>在全部会话中搜索</Button>}
              </Flex>
            : undefined,
        }}
        pagination={{ current: page, pageSize: PAGE_SIZE, total, showSizeChanger: false, onChange: setPage }}
      />
    </Card>
    <Drawer
      title={editing?.name || '未命名会话'}
      open={Boolean(editing)}
      size={720}
      onClose={() => setEditing(null)}
      destroyOnHidden
      footer={<Flex justify="flex-end" gap={8}><Button onClick={() => setEditing(null)}>关闭</Button><Button type="primary" loading={submitting} onClick={submit}>保存</Button></Flex>}
    >
      {editing && <Space orientation="vertical" size={20} style={{ width: '100%' }}>
        <Descriptions className="world-detail-descriptions" column={1} size="small" bordered>
          <Descriptions.Item label="类型">{chatModeLabels[editing.chat_mode] || editing.chat_mode}</Descriptions.Item>
          <Descriptions.Item label="分层"><Tag color={tierColors[editing.tier] || 'default'}>{tierLabels[editing.tier] || editing.tier}</Tag></Descriptions.Item>
          <Descriptions.Item label="最近活跃">{editing.last_active_at ? new Date(editing.last_active_at).toLocaleString('zh-CN', { hour12: false }) : '—'}</Descriptions.Item>
          <Descriptions.Item label="消息数">{editing.related_group ? editing.message_count : '—'}</Descriptions.Item>
          <Descriptions.Item label="最近扫描">{editing.related_group ? formatScanTime(editing.last_scan_at) : '—'}</Descriptions.Item>
          <Descriptions.Item label="扫描状态">{editing.last_scan_status ? scanStatusMeta[editing.last_scan_status]?.label || editing.last_scan_status : '—'}</Descriptions.Item>
          <Descriptions.Item label="会话说明">{editing.description || '—'}</Descriptions.Item>
        </Descriptions>
        <Form form={form} layout="vertical">
          <Form.Item name="project_id" label="关联项目">
            <Select allowClear placeholder="不关联" options={projects.map((p) => ({ value: p.id, label: p.name }))} />
          </Form.Item>
          <Flex gap={20} wrap>
            <Form.Item name="related_group" label="纳入监控" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="is_key_group" label="关键群" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="pinned" label="始终热扫" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="include_in_memory" label="纳入记忆" valuePropName="checked"><Switch /></Form.Item>
          </Flex>
        </Form>
        <SummaryPageEditor type="group" id={editing.id} />
        <FactTimeline subject={{ type: 'group', id: editing.id }} title="会话事实" />
      </Space>}
    </Drawer>
  </>
}

// --- Profile (principal "me") ---

function ProfilePanel() {
  const [profile, setProfile] = useState<ProfileView | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [ok, setOk] = useState(false)
  const [form] = Form.useForm<ProfileInput>()

  // Leader binding reuses the person search so leader_open_id is a real open_id.
  const [leaderOpenID, setLeaderOpenID] = useState('')
  const [leaderName, setLeaderName] = useState('')
  const [leaderQuery, setLeaderQuery] = useState('')
  const [leaderSearching, setLeaderSearching] = useState(false)
  const [leaderCandidates, setLeaderCandidates] = useState<ResolveCandidate[] | null>(null)

  const reload = useCallback(() => {
    setLoading(true)
    getProfile()
      .then((result) => {
        setProfile(result)
        setLeaderOpenID(result.leader_open_id || '')
        setLeaderName(result.leader_name || '')
        form.setFieldsValue({
          name: result.name, department: result.department, title: result.title,
        })
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [form])
  useEffect(reload, [reload])

  const runLeaderSearch = async () => {
    if (!leaderQuery.trim()) return
    setLeaderSearching(true)
    try {
      const result = await resolvePerson(leaderQuery.trim())
      setLeaderCandidates(result.candidates)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setLeaderSearching(false)
    }
  }
  const pickLeader = (candidate: ResolveCandidate) => {
    setLeaderOpenID(candidate.open_id)
    setLeaderName(candidate.name)
    setLeaderCandidates(null)
    setLeaderQuery('')
  }
  const clearLeader = () => { setLeaderOpenID(''); setLeaderName('') }

  const submit = async () => {
    const values = await form.validateFields()
    setSaving(true)
    setOk(false)
    try {
      const saved = await updateProfile({
        ...values,
        leader_open_id: leaderOpenID || null,
        leader_name: leaderName || null,
      })
      setProfile(saved)
      setOk(true)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  return <>
    {error && <Alert type="error" showIcon title="保存失败" description={error} closable onClose={() => setError(undefined)} style={{ marginBottom: 12 }} />}
    {ok && <Alert type="success" showIcon title="已保存，抽取时会把「我的背景」喂给模型" closable onClose={() => setOk(false)} style={{ marginBottom: 12 }} />}
    {profile && !profile.saved && <Alert type="info" showIcon title="首次填写：Principal（我）背景尚未设置，完善后可显著提升 leader 软措辞交办的识别" style={{ marginBottom: 12 }} />}
    <Card variant="borderless" loading={loading} className="memory-profile-card">
      <Form form={form} layout="vertical">
        <Form.Item name="name" label="姓名（当前用户是谁）" rules={[{ required: true, message: '请填写姓名' }]}>
          <Input placeholder="如：负责人姓名" />
        </Form.Item>
        <Flex gap={12}>
          <Form.Item name="department" label="部门" style={{ flex: 1 }}><Input placeholder="选填" /></Form.Item>
          <Form.Item name="title" label="职位" style={{ flex: 1 }}><Input placeholder="选填" /></Form.Item>
        </Flex>
        <Form.Item label="直属 leader" tooltip="显式告诉模型「我的 leader 是谁」，对识别 leader 软措辞交办最关键">
          {leaderOpenID ? (
            <Flex gap={8} align="center">
              <Tag color="gold">{leaderName || leaderOpenID}</Tag>
              <Button size="small" onClick={clearLeader}>清除</Button>
            </Flex>
          ) : (
            <Flex vertical gap={8}>
              <Flex gap={8}>
                <Input.Search
                  placeholder="搜索姓名 / 邮箱绑定 leader" value={leaderQuery}
                  onChange={(e) => setLeaderQuery(e.target.value)} onSearch={runLeaderSearch}
                  loading={leaderSearching} enterButton="搜索" style={{ maxWidth: 360 }}
                />
              </Flex>
              {leaderCandidates && (
                <Card size="small" variant="outlined">
                  {leaderCandidates.length === 0 ? <Text type="secondary">无匹配</Text> : leaderCandidates.map((c) => (
                    <Flex key={c.open_id} justify="space-between" align="center" style={{ padding: '4px 0' }}>
                      <Text>{c.name} <Text type="secondary" style={{ fontSize: 12 }}>{c.department}</Text></Text>
                      <Button size="small" type="link" onClick={() => pickLeader(c)}>选择</Button>
                    </Flex>
                  ))}
                </Card>
              )}
            </Flex>
          )}
        </Form.Item>
        <Collapse
          ghost
          className="memory-advanced"
          items={[{
            key: 'identity',
            label: '高级信息',
            children: (
              <Descriptions size="small" column={1}>
                <Descriptions.Item label="我的飞书用户标识"><Text copyable>{profile?.open_id || '—'}</Text></Descriptions.Item>
                <Descriptions.Item label="直属 leader 用户标识"><Text copyable>{leaderOpenID || '—'}</Text></Descriptions.Item>
              </Descriptions>
            ),
          }]}
        />
        <Button type="primary" loading={saving} onClick={submit}>保存</Button>
      </Form>
    </Card>
    {profile?.saved && profile.id > 0 && (
      <SummaryPageEditor type="principal" id={profile.id} />
    )}
    {profile?.saved && profile.id > 0 && (
      <FactTimeline subject={{ type: 'principal', id: profile.id }} title="我的事实" />
    )}
  </>
}

// --- Resources ---

// resourceToInput projects a stored Resource back into the update payload so an
// inline edit patches exactly one field without dropping the rest.
function resourceToInput(resource: Resource): ResourceInput {
  return {
    title: resource.title, resource_type: resource.resource_type, url: resource.url,
    person_id: resource.person_id, project_id: resource.project_id,
    link_principal: resource.link_principal, is_active: resource.is_active,
  }
}

type ResourceStatusFilter = 'all' | 'active' | 'inactive'
type ResourceTypeFilter = 'all' | ResourceType

function resourceAddressLabel(value: string): string {
  try {
    const url = new URL(value)
    return `${url.hostname}${url.pathname === '/' ? '' : url.pathname}${url.search}`
  } catch {
    return value
  }
}

function resourceActiveLabel(value: string): string {
  const activeAt = dayjs(value)
  const today = dayjs()
  if (activeAt.isSame(today, 'day')) return `今天 ${activeAt.format('HH:mm')}`
  if (activeAt.isSame(today.subtract(1, 'day'), 'day')) return `昨天 ${activeAt.format('HH:mm')}`
  return activeAt.format('YYYY-MM-DD')
}

function ResourcePanel() {
  const { name: agentName } = useAgentIdentity()
  const [items, setItems] = useState<Resource[]>([])
  const [total, setTotal] = useState(0)
  const [activeTotal, setActiveTotal] = useState(0)
  const [maxActive, setMaxActive] = useState(50)
  const [persons, setPersons] = useState<Person[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [savingId, setSavingId] = useState<number>()
  const [touchingId, setTouchingId] = useState<number>()
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [editingId, setEditingId] = useState<number>()
  const [draft, setDraft] = useState<ResourceInput>()
  const [query, setQuery] = useState('')
  const [statusFilter, setStatusFilter] = useState<ResourceStatusFilter>('all')
  const [typeFilter, setTypeFilter] = useState<ResourceTypeFilter>('all')
  const [selectedResource, setSelectedResource] = useState<Resource>()
  const [form] = Form.useForm<ResourceInput>()

  const reload = useCallback(() => {
    setLoading(true)
    Promise.all([listResources(), listPersons(), listProjects()])
      .then(([resourceResult, personResult, projectResult]) => {
        setItems(resourceResult.items)
        setTotal(resourceResult.total)
        setActiveTotal(resourceResult.active_total)
        setMaxActive(resourceResult.max_active)
        setPersons(personResult.items)
        setProjects(projectResult.items)
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const patchResource = async (resource: Resource, patch: Partial<ResourceInput>) => {
    setSavingId(resource.id)
    try {
      const saved = await updateResource(resource.id, { ...resourceToInput(resource), ...patch })
      setItems((current) => current.map((item) => item.id === saved.id ? saved : item))
      if (selectedResource?.id === saved.id) setSelectedResource(saved)
      if (resource.is_active !== saved.is_active) {
        setActiveTotal((current) => current + (saved.is_active ? 1 : -1))
      }
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingId(undefined)
    }
  }

  const beginEdit = (resource: Resource) => {
    setEditingId(resource.id)
    setDraft(resourceToInput(resource))
  }

  const cancelEdit = () => {
    setEditingId(undefined)
    setDraft(undefined)
  }

  const saveEdit = async (resource: Resource) => {
    const title = draft?.title.trim()
    if (!draft || !title) {
      setError('资源名称不能为空')
      return
    }
    setSavingId(resource.id)
    try {
      const saved = await updateResource(resource.id, { ...draft, title })
      setItems((current) => current.map((item) => item.id === saved.id ? saved : item))
      if (selectedResource?.id === saved.id) setSelectedResource(saved)
      if (resource.is_active !== saved.is_active) {
        setActiveTotal((current) => current + (saved.is_active ? 1 : -1))
      }
      cancelEdit()
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingId(undefined)
    }
  }

  const openCreate = () => {
    form.setFieldsValue({ title: '', resource_type: 'link', url: null, person_id: null, project_id: null, link_principal: false, is_active: true })
    setOpen(true)
  }
  const submit = async () => {
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      await createResource(values)
      setOpen(false)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const remove = async (resource: Resource) => {
    try {
      await deleteResource(resource.id)
      if (selectedResource?.id === resource.id) setSelectedResource(undefined)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }
  const touch = async (resource: Resource) => {
    setTouchingId(resource.id)
    try {
      const saved = await touchResource(resource.id)
      setItems((current) => current.map((item) => item.id === saved.id ? saved : item).sort((left, right) => (
        dayjs(right.last_active_at).valueOf() - dayjs(left.last_active_at).valueOf()
      )))
      if (selectedResource?.id === saved.id) setSelectedResource(saved)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setTouchingId(undefined)
    }
  }

  const personOptions = [{ value: 0, label: '—' }, ...persons.map((p) => ({ value: p.id, label: p.name }))]
  const projectOptions = [{ value: 0, label: '—' }, ...projects.map((p) => ({ value: p.id, label: p.name }))]

  const filteredItems = useMemo(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase()
    return items.filter((resource) => {
      if (statusFilter === 'active' && !resource.is_active) return false
      if (statusFilter === 'inactive' && resource.is_active) return false
      if (typeFilter !== 'all' && resource.resource_type !== typeFilter) return false
      if (!normalizedQuery) return true
      return [resource.title, resource.summary, resource.url, resource.person_name, resource.project_name]
        .some((value) => value?.toLocaleLowerCase().includes(normalizedQuery))
    })
  }, [items, query, statusFilter, typeFilter])

  const columns: TableColumnsType<Resource> = [
    {
      title: '资源', dataIndex: 'title', width: 270, render: (_, r) => editingId === r.id && draft ? (
        <div className="resource-edit-stack">
          <Input value={draft.title} placeholder="资源名称" disabled={savingId === r.id}
            onChange={(event) => setDraft((current) => current ? { ...current, title: event.target.value } : current)} />
          <Flex gap={8}>
            <Select value={draft.resource_type} style={{ width: 104 }} disabled={savingId === r.id}
              options={Object.entries(resourceTypeLabels).map(([value, label]) => ({ value, label }))}
              onChange={(value) => setDraft((current) => current ? { ...current, resource_type: value as ResourceType } : current)} />
            <Input value={draft.url ?? ''} placeholder="链接或仓库地址（可选）" disabled={savingId === r.id}
              onChange={(event) => setDraft((current) => current ? { ...current, url: event.target.value || null } : current)} />
          </Flex>
        </div>
      ) : (
        <div className="resource-primary">
          <Flex align="center" gap={8} wrap>
            <Text strong className="resource-title">{r.title}</Text>
            <Tag variant="filled" className={`resource-type resource-type-${r.resource_type}`}>{resourceTypeLabels[r.resource_type]}</Tag>
          </Flex>
          {summaryIndexLine(r.summary) && <Text type="secondary" className="resource-description">{summaryIndexLine(r.summary)}</Text>}
          {r.url ? (
            /^https?:\/\//i.test(r.url)
              ? <Tooltip title={r.url}><Typography.Link className="resource-address" href={r.url} target="_blank" rel="noreferrer">{resourceAddressLabel(r.url)}</Typography.Link></Tooltip>
              : <Tooltip title={r.url}><Text className="resource-address" copyable={{ text: r.url }} ellipsis>{resourceAddressLabel(r.url)}</Text></Tooltip>
          ) : <Text type="secondary" className="resource-address resource-address-empty">未填写地址</Text>}
        </div>
      ),
    },
    {
      title: '关联范围', width: 180, render: (_, r) => editingId === r.id && draft ? (
        <div className="resource-edit-stack">
          <Select value={draft.person_id ?? 0} style={{ width: '100%' }} disabled={savingId === r.id}
            options={personOptions} showSearch optionFilterProp="label" aria-label="关联人"
            onChange={(value) => setDraft((current) => current ? { ...current, person_id: value === 0 ? null : value } : current)} />
          <Select value={draft.project_id ?? 0} style={{ width: '100%' }} disabled={savingId === r.id}
            options={projectOptions} showSearch optionFilterProp="label" aria-label="关联项目"
            onChange={(value) => setDraft((current) => current ? { ...current, project_id: value === 0 ? null : value } : current)} />
          <Flex align="center" gap={8}><Switch size="small" checked={draft.link_principal} disabled={savingId === r.id}
            aria-label={`${r.title} 关联我`}
            onChange={(checked) => setDraft((current) => current ? { ...current, link_principal: checked } : current)} /><Text>关联我</Text></Flex>
        </div>
      ) : (
        <Flex gap={6} wrap className="resource-scope">
          {r.project_name && <Tag variant="filled">项目 · {r.project_name}</Tag>}
          {r.person_name && <Tag variant="filled">人物 · {r.person_name}</Tag>}
          {r.link_principal && <Tag variant="filled" color="green">我的资料</Tag>}
          {!r.project_name && !r.person_name && !r.link_principal && <Text type="secondary">未关联</Text>}
        </Flex>
      ),
    },
    {
      title: '状态', dataIndex: 'is_active', width: 130, render: (v: boolean, r) => (
        <div className="resource-status">
          <Flex align="center" gap={8}>
            <Switch size="small" checked={editingId === r.id && draft ? draft.is_active : v} loading={savingId === r.id}
              aria-label={`${r.title} ${(editingId === r.id && draft ? draft.is_active : v) ? '已启用' : '已停用'}`}
              onChange={(checked) => editingId === r.id && draft
                ? setDraft((current) => current ? { ...current, is_active: checked } : current)
                : patchResource(r, { is_active: checked })} />
            <Text>{(editingId === r.id && draft ? draft.is_active : v) ? '已启用' : '已停用'}</Text>
          </Flex>
          <Tooltip title={`最近活跃：${dayjs(r.last_active_at).format('YYYY-MM-DD HH:mm')}`}>
            <Text type="secondary" className="resource-active-time">{resourceActiveLabel(r.last_active_at)}</Text>
          </Tooltip>
        </div>
      ),
    },
    {
      title: '操作', width: 210, fixed: 'right', render: (_, r) => editingId === r.id ? (
        <Flex gap={6}>
          <Button size="small" type="primary" loading={savingId === r.id} onClick={() => saveEdit(r)}>保存</Button>
          <Button size="small" disabled={savingId === r.id} onClick={cancelEdit}>取消</Button>
        </Flex>
      ) : (
        <Flex gap={2} wrap="wrap" className="resource-actions">
          <Button size="small" type="text" disabled={editingId !== undefined} onClick={() => setSelectedResource(r)}>详情</Button>
          <Tooltip title="更新最近确认时间，并将该资源排到前面">
            <Button size="small" type="text" disabled={!r.is_active || editingId !== undefined} loading={touchingId === r.id} onClick={() => touch(r)}>确认有效</Button>
          </Tooltip>
          <Button size="small" type="text" disabled={editingId !== undefined} onClick={() => beginEdit(r)}>编辑</Button>
          <Popconfirm title="删除该资源？" description="删除后无法恢复。" onConfirm={() => remove(r)} okText="删除" cancelText="取消">
            <Button size="small" type="text" danger disabled={editingId !== undefined}>删除</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <section className="resource-overview">
      <div className="resource-overview-copy">
        <Text strong className="resource-overview-title">长期资源</Text>
        <Text type="secondary">{agentName} 会按人物、项目和你的个人背景调用这些资料。</Text>
      </div>
      <div className="resource-stats" aria-label="资源统计">
        <div><Text type="secondary">全部</Text><Text strong>{total}</Text></div>
        <div><Text type="secondary">已启用</Text><Text strong>{activeTotal}</Text></div>
        <div><Text type="secondary">可用额度</Text><Text strong>{Math.max(0, maxActive - activeTotal)}</Text></div>
      </div>
      <Flex gap={8} className="resource-overview-actions">
        <Button onClick={reload} loading={loading} disabled={editingId !== undefined}>刷新</Button>
        <Button type="primary" onClick={openCreate} disabled={activeTotal >= maxActive || editingId !== undefined}>新建资源</Button>
      </Flex>
    </section>
    {error && <Alert type="error" showIcon title="资源操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="resource-list-card" variant="borderless">
      <div className="resource-filters">
        <Input value={query} allowClear disabled={editingId !== undefined} placeholder="搜索名称、长期事实、地址、人物或项目" onChange={(event) => setQuery(event.target.value)} />
        <Segmented<ResourceStatusFilter> value={statusFilter} disabled={editingId !== undefined} onChange={setStatusFilter} options={[
          { label: `全部 ${items.length}`, value: 'all' },
          { label: `已启用 ${items.filter((item) => item.is_active).length}`, value: 'active' },
          { label: `已停用 ${items.filter((item) => !item.is_active).length}`, value: 'inactive' },
        ]} />
        <Select<ResourceTypeFilter> value={typeFilter} disabled={editingId !== undefined} className="resource-type-filter" onChange={setTypeFilter} options={[
          { value: 'all', label: '全部类型' },
          ...Object.entries(resourceTypeLabels).map(([value, label]) => ({ value: value as ResourceType, label })),
        ]} />
        <Text type="secondary" className="resource-filter-result">显示 {filteredItems.length} 条</Text>
      </div>
      <Table<Resource>
        rowKey="id"
        className="resource-table"
        columns={columns}
        dataSource={filteredItems}
        loading={loading}
        locale={{ emptyText: query || statusFilter !== 'all' || typeFilter !== 'all' ? '没有符合筛选条件的资源' : '还没有资源' }}
        pagination={{ pageSize: 20, hideOnSinglePage: filteredItems.length <= 20, showSizeChanger: false, disabled: editingId !== undefined, showTotal: (count) => `共 ${count} 条` }}
        scroll={{ x: 790 }}
        rowClassName={(resource) => resource.is_active ? '' : 'resource-row-inactive'}
      />
    </Card>
    <Drawer title={selectedResource?.title || '资源详情'} open={Boolean(selectedResource)} size={720} onClose={() => setSelectedResource(undefined)} destroyOnHidden>
      {selectedResource && <Space orientation="vertical" size={20} style={{ width: '100%' }}>
        <Descriptions className="world-detail-descriptions" column={1} size="small" bordered>
          <Descriptions.Item label="类型"><Tag>{resourceTypeLabels[selectedResource.resource_type]}</Tag></Descriptions.Item>
          <Descriptions.Item label="状态"><Tag color={selectedResource.is_active ? 'green' : 'default'}>{selectedResource.is_active ? '已启用' : '已停用'}</Tag></Descriptions.Item>
          <Descriptions.Item label="地址">
            {selectedResource.url ? (
              /^https?:\/\//i.test(selectedResource.url)
                ? <Typography.Link href={selectedResource.url} target="_blank" rel="noreferrer">{selectedResource.url}</Typography.Link>
                : <Text copyable={{ text: selectedResource.url }}>{selectedResource.url}</Text>
            ) : '—'}
          </Descriptions.Item>
          <Descriptions.Item label="关联范围">
            <Flex gap={6} wrap>
              {selectedResource.project_name && <Tag>项目 · {selectedResource.project_name}</Tag>}
              {selectedResource.person_name && <Tag>人物 · {selectedResource.person_name}</Tag>}
              {selectedResource.link_principal && <Tag color="green">我的资料</Tag>}
              {!selectedResource.project_name && !selectedResource.person_name && !selectedResource.link_principal && <Text type="secondary">未关联</Text>}
            </Flex>
          </Descriptions.Item>
          <Descriptions.Item label="最近确认">{dayjs(selectedResource.last_active_at).format('YYYY-MM-DD HH:mm')}</Descriptions.Item>
        </Descriptions>
        <SummaryPageEditor type="resource" id={selectedResource.id} />
        <FactTimeline subject={{ type: 'managed_resource', id: selectedResource.id }} title="资源事实" />
      </Space>}
    </Drawer>
    <Modal title="新建资源" open={open} confirmLoading={submitting} onOk={submit} onCancel={() => setOpen(false)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="title" label="名称" rules={[{ required: true, message: '请输入资源名称' }]}><Input /></Form.Item>
        <Flex gap={16}>
          <Form.Item name="resource_type" label="类型" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Select options={Object.entries(resourceTypeLabels).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
          <Form.Item name="link_principal" label="关联我" valuePropName="checked" style={{ width: 90 }}><Switch /></Form.Item>
        </Flex>
        <Form.Item name="url" label="链接/地址(可选)"><Input allowClear /></Form.Item>
        <Flex gap={16}>
          <Form.Item name="person_id" label="关联人(可选)" style={{ flex: 1 }}>
            <Select allowClear showSearch optionFilterProp="label" placeholder="—"
              options={persons.map((p) => ({ value: p.id, label: p.name }))} />
          </Form.Item>
          <Form.Item name="project_id" label="关联项目(可选)" style={{ flex: 1 }}>
            <Select allowClear showSearch optionFilterProp="label" placeholder="—"
              options={projects.map((p) => ({ value: p.id, label: p.name }))} />
          </Form.Item>
        </Flex>
      </Form>
    </Modal>
  </>
}

// --- Skills ---

function SkillsPanel() {
  const [items, setItems] = useState<AgentSkill[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<AgentSkill | null>(null)
  const [content, setContent] = useState<{ name: string; path: string; text: string } | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<AgentSkillInput>()

  const reload = useCallback(() => {
    setLoading(true)
    listSkills()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const sync = async () => {
    setLoading(true)
    try {
      const result = await scanSkills()
      setItems(result.items)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }
  const openEdit = (item: AgentSkill) => {
    setEditing(item)
    form.setFieldsValue({ stages: item.stages, is_enabled: item.is_enabled })
  }
  const submit = async () => {
    if (!editing) return
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      await updateSkill(editing.name, values)
      setEditing(null)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const toggle = async (item: AgentSkill, checked: boolean) => {
    try {
      await updateSkill(item.name, { stages: item.stages, is_enabled: checked })
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }
  const showContent = async (item: AgentSkill) => {
    try {
      const result = await getSkillContent(item.name)
      setContent({ name: result.name, path: result.path, text: result.content })
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  const columns: TableColumnsType<AgentSkill> = [
    { title: 'Skill', dataIndex: 'name', width: 220, render: (name: string) => <Text strong>{name}</Text> },
    { title: '说明', dataIndex: 'description', ellipsis: true },
    {
      title: '生效阶段', width: 270,
      render: (_, item) => <Flex gap={4} wrap>{item.stages.map((stage) => <Tag key={stage}>{workRuleStageLabels[stage]}</Tag>)}</Flex>,
    },
    {
      title: '启用', dataIndex: 'is_enabled', width: 70, align: 'center',
      render: (enabled: boolean, item) => <Switch size="small" checked={enabled} onChange={(checked) => toggle(item, checked)} />,
    },
    {
      title: '操作', width: 150, render: (_, item) => (
        <Flex gap={8}>
          <Button size="small" onClick={() => openEdit(item)}>范围</Button>
          <Button size="small" onClick={() => showContent(item)}>查看</Button>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个 Skill，正文来自 .agents/skills</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={sync} loading={loading}>扫描目录</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon title="Skills 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<AgentSkill> rowKey="name" columns={columns} dataSource={items} loading={loading} pagination={false} /></Card>
    <Modal title={`设置 Skill 范围 · ${editing?.name || ''}`} open={Boolean(editing)} confirmLoading={submitting} onOk={submit} onCancel={() => setEditing(null)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="stages" label="生效阶段" rules={[{ required: true, type: 'array', min: 1, message: '至少选择一个阶段' }]}>
          <Select mode="multiple" options={Object.entries(workRuleStageLabels).map(([value, label]) => ({ value: value as SkillStage, label }))} />
        </Form.Item>
        <Form.Item name="is_enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
      </Form>
    </Modal>
    <Modal title={`Skill · ${content?.name || ''}`} open={Boolean(content)} onCancel={() => setContent(null)} footer={null} width={760}>
      <div style={{ marginBottom: 8 }}><Text code>{content?.path}</Text></div>
      <Input.TextArea value={content?.text} readOnly autoSize={{ minRows: 12, maxRows: 24 }} style={{ fontFamily: 'monospace' }} />
    </Modal>
  </>
}

type MemoryView = 'world-map' | 'projects' | 'persons' | 'groups' | 'resources' | 'key-matters' | 'facts' | 'profile'

export default function Background() {
  const { name: agentName } = useAgentIdentity()
  const { context, setViewState } = usePageContext()
  const memoryView = (value: string | undefined): MemoryView => (
    value === 'world-map' || value === 'projects' || value === 'persons' || value === 'groups' || value === 'resources' || value === 'key-matters' || value === 'facts' || value === 'profile'
      ? value
      : 'profile'
  )
  const activeView = memoryView(context.view_state.view)

  const selectView = (view: MemoryView) => {
    setViewState({ view })
  }

  return (
    <div className="memory-page">
      <PageHeader title="世界" subtitle={`浏览 ${agentName} 用来理解你、项目和协作关系的长期背景`} />

      <Tabs
        activeKey={activeView}
        onChange={(key) => selectView(key as MemoryView)}
        destroyOnHidden
        items={[
          {
            key: 'world-map',
            label: '世界地图',
            children: (
              <Suspense fallback={<div style={{ padding: 72, textAlign: 'center' }}><Spin size="large" /></div>}>
                <WorldMap />
              </Suspense>
            ),
          },
          {
            key: 'profile',
            label: '我的资料',
            children: (
              <div className="memory-profile-view">
                <ProfilePanel />
              </div>
            ),
          },
          { key: 'projects', label: '项目', children: <ProjectsPanel /> },
          { key: 'persons', label: '人物', children: <PersonsPanel /> },
          { key: 'groups', label: '会话', children: <GroupsPanel /> },
          { key: 'resources', label: '资源', children: <ResourcePanel /> },
          { key: 'key-matters', label: '关键事项', children: <KeyMattersPanel /> },
          { key: 'facts', label: '全部事实', children: <FactsPanel /> },
        ]}
      />
    </div>
  )
}

export function Settings() {
  const { name: agentName } = useAgentIdentity()
  const { context, setViewState } = usePageContext()
  type SettingsView = 'runtime' | 'scheduling' | 'memory' | 'extensions'
  const settingsView = (value: string | undefined): SettingsView => (
    value === 'scheduling' || value === 'memory' || value === 'extensions' ? value : 'runtime'
  )
  const activeView = settingsView(context.view_state.view)

  return (
    <div className="settings-page">
      <PageHeader title="系统设置" subtitle={`配置 ${agentName} 的运行、调度、共享记忆和扩展能力`} />
      <Tabs
        activeKey={activeView}
        onChange={(view) => setViewState({ view })}
        items={[
          { key: 'runtime', label: '运行', children: <RuntimeSettings /> },
          { key: 'scheduling', label: '调度', children: <SystemTasks /> },
          { key: 'memory', label: '共享记忆', children: <SharedMemory /> },
          {
            key: 'extensions',
            label: '扩展',
            children: (
              <Space orientation="vertical" size={24} style={{ width: '100%' }}>
                <AppModules
                  moduleKeys={['biz-okr']}
                  title="业务应用"
                  description="Biz OKR 组合通用 OKR 插件提供业务打标、Plan、Review、周报与自动化。"
                />
                <SkillsPanel />
              </Space>
            ),
          },
        ]}
      />
    </div>
  )
}
