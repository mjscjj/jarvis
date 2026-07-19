import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Flex,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Switch,
  Table,
  Tag,
  Tabs,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  createPerson,
  createProject,
  deletePerson,
  deleteProject,
  listGroups,
  listPersons,
  listProjects,
  updateGroupBackground,
  updatePerson,
  updateProject,
} from './api'
import type {
  Group,
  GroupBackgroundInput,
  Person,
  PersonInput,
  PersonRole,
  Project,
  ProjectInput,
  ProjectRole,
  ProjectStatus,
} from './types'

const { Text } = Typography

const projectRoleLabels: Record<ProjectRole, string> = { owner: '负责人', participant: '参与者' }
const projectStatusLabels: Record<ProjectStatus, string> = {
  planning: '规划中', active: '进行中', paused: '暂停', archived: '归档', done: '完成',
}
const personRoleLabels: Record<PersonRole, string> = {
  leader: 'Leader', key: '关键干系人', colleague: '同事', other: '其他',
}
const personRoleColors: Record<PersonRole, string> = {
  leader: 'volcano', key: 'gold', colleague: 'blue', other: 'default',
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

  const reload = useCallback(() => {
    setLoading(true)
    listProjects()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({ name: '', role: 'participant', status: 'active', priority: 3, code: null, description: null, notes: null })
    setOpen(true)
  }
  const openEdit = (project: Project) => {
    setEditing(project)
    form.setFieldsValue({
      name: project.name, role: project.role, status: project.status, priority: project.priority,
      code: project.code, description: project.description, notes: project.notes,
    })
    setOpen(true)
  }
  const submit = async () => {
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      if (editing) await updateProject(editing.id, values)
      else await createProject(values)
      setOpen(false)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const remove = async (project: Project) => {
    try { await deleteProject(project.id); reload() } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const columns: TableColumnsType<Project> = [
    { title: '项目', dataIndex: 'name', render: (_, p) => <Text strong>{p.name}</Text> },
    { title: '角色', dataIndex: 'role', width: 100, render: (r: ProjectRole) => projectRoleLabels[r] },
    { title: '状态', dataIndex: 'status', width: 100, render: (s: ProjectStatus) => <Tag>{projectStatusLabels[s]}</Tag> },
    { title: '优先级', dataIndex: 'priority', width: 90 },
    { title: '描述', dataIndex: 'description', ellipsis: true, render: (v: string | null) => v || '—' },
    {
      title: '操作', width: 150, render: (_, p) => (
        <Flex gap={8}>
          <Button size="small" onClick={() => openEdit(p)}>编辑</Button>
          <Popconfirm title="删除该项目？" onConfirm={() => remove(p)} okText="删除" cancelText="取消">
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个项目</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建项目</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon message="项目操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Project> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} /></Card>
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
        <Form.Item name="description" label="描述(可选)"><Input.TextArea rows={2} /></Form.Item>
        <Form.Item name="notes" label="备注(可选)"><Input.TextArea rows={2} /></Form.Item>
      </Form>
    </Modal>
  </>
}

// --- Persons ---

function PersonsPanel() {
  const [items, setItems] = useState<Person[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Person | null>(null)
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<PersonInput>()

  const reload = useCallback(() => {
    setLoading(true)
    listPersons()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({ open_id: '', name: '', role: 'colleague', priority_weight: 0.5, department: null, title: null, relation: null, notes: null, is_active: true })
    setOpen(true)
  }
  const openEdit = (person: Person) => {
    setEditing(person)
    form.setFieldsValue({
      open_id: person.open_id, name: person.name, role: person.role, priority_weight: person.priority_weight,
      department: person.department, title: person.title, relation: person.relation, notes: person.notes, is_active: person.is_active,
    })
    setOpen(true)
  }
  const submit = async () => {
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      if (editing) await updatePerson(editing.id, values)
      else await createPerson(values)
      setOpen(false)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const remove = async (person: Person) => {
    try { await deletePerson(person.id); reload() } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const columns: TableColumnsType<Person> = [
    { title: '姓名', dataIndex: 'name', render: (_, p) => <Text strong>{p.name}</Text> },
    { title: '角色', dataIndex: 'role', width: 120, render: (r: PersonRole) => <Tag color={personRoleColors[r]}>{personRoleLabels[r]}</Tag> },
    { title: '权重', dataIndex: 'priority_weight', width: 80 },
    { title: '部门/职位', width: 200, render: (_, p) => [p.department, p.title].filter(Boolean).join(' · ') || '—' },
    { title: 'open_id', dataIndex: 'open_id', ellipsis: true },
    { title: '启用', dataIndex: 'is_active', width: 70, render: (v: boolean) => v ? <Tag color="green">是</Tag> : <Tag>否</Tag> },
    {
      title: '操作', width: 150, render: (_, p) => (
        <Flex gap={8}>
          <Button size="small" onClick={() => openEdit(p)}>编辑</Button>
          <Popconfirm title="删除该人物？" onConfirm={() => remove(p)} okText="删除" cancelText="取消">
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个人物</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建人物</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon message="人物操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Person> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 900 }} /></Card>
    <Modal title={editing ? '编辑人物' : '新建人物'} open={open} confirmLoading={submitting} onOk={submit} onCancel={() => setOpen(false)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Flex gap={16}>
          <Form.Item name="name" label="姓名" rules={[{ required: true, message: '请输入姓名' }]} style={{ flex: 1 }}><Input /></Form.Item>
          <Form.Item name="role" label="角色" rules={[{ required: true }]} style={{ width: 160 }}>
            <Select options={Object.entries(personRoleLabels).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
        </Flex>
        <Form.Item name="open_id" label="飞书 open_id" rules={[{ required: true, message: '请输入 open_id' }]} extra="可从飞书通讯录/消息中获取，形如 ou_xxx"><Input /></Form.Item>
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
        <Form.Item name="relation" label="与我的关系(可选)"><Input allowClear placeholder="如：直属领导 / 同组同事" /></Form.Item>
        <Form.Item name="notes" label="备注(可选)"><Input.TextArea rows={2} /></Form.Item>
      </Form>
    </Modal>
  </>
}

// --- Groups (background patch only) ---

function GroupsPanel() {
  const [items, setItems] = useState<Group[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Group | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<GroupBackgroundInput>()

  const reload = useCallback(() => {
    setLoading(true)
    Promise.all([listGroups(), listProjects()])
      .then(([groupResult, projectResult]) => { setItems(groupResult.items); setProjects(projectResult.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

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

  const columns: TableColumnsType<Group> = [
    { title: '会话', dataIndex: 'name', render: (_, g) => <Text strong>{g.name || g.chat_id}</Text> },
    { title: '类型', dataIndex: 'chat_mode', width: 90 },
    { title: '分层', dataIndex: 'tier', width: 80, render: (t: string) => <Tag>{t}</Tag> },
    { title: '关联项目', width: 160, render: (_, g) => g.project?.name || '—' },
    { title: '相关', dataIndex: 'related_group', width: 70, render: (v: boolean) => v ? <Tag color="green">是</Tag> : '—' },
    { title: '关键群', dataIndex: 'is_key_group', width: 80, render: (v: boolean) => v ? <Tag color="volcano">是</Tag> : '—' },
    { title: '记忆', dataIndex: 'include_in_memory', width: 70, render: (v: boolean) => v ? '✓' : '—' },
    { title: '操作', width: 100, render: (_, g) => <Button size="small" onClick={() => openEdit(g)}>编辑背景</Button> },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个会话（由采集发现，此处仅维护人工背景）</Text>
      <Button onClick={reload} loading={loading}>刷新</Button>
    </Flex>
    {error && <Alert type="error" showIcon message="会话背景操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Group> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 950 }} /></Card>
    <Modal title={`编辑会话背景 · ${editing?.name || editing?.chat_id || ''}`} open={Boolean(editing)} confirmLoading={submitting} onOk={submit} onCancel={() => setEditing(null)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="project_id" label="关联项目">
          <Select allowClear placeholder="不关联" options={projects.map((p) => ({ value: p.id, label: p.name }))} />
        </Form.Item>
        <Form.Item name="related_group" label="相关会话(纳入监控)" valuePropName="checked"><Switch /></Form.Item>
        <Form.Item name="is_key_group" label="关键群" valuePropName="checked"><Switch /></Form.Item>
        <Form.Item name="pinned" label="置顶(始终热扫)" valuePropName="checked"><Switch /></Form.Item>
        <Form.Item name="include_in_memory" label="纳入记忆" valuePropName="checked"><Switch /></Form.Item>
      </Form>
    </Modal>
  </>
}

export default function Background() {
  return (
    <Tabs
      items={[
        { key: 'projects', label: '项目', children: <ProjectsPanel /> },
        { key: 'persons', label: '人物', children: <PersonsPanel /> },
        { key: 'groups', label: '会话背景', children: <GroupsPanel /> },
      ]}
    />
  )
}
