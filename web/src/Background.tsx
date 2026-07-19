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
  Segmented,
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
  resolvePerson,
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
  ResolveCandidate,
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

const roleDefaultWeight: Record<PersonRole, number> = { leader: 1.0, key: 0.7, colleague: 0.4, other: 0.1 }

function PersonsPanel() {
  const [items, setItems] = useState<Person[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Person | null>(null)
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<PersonInput>()
  const [boundOpenID, setBoundOpenID] = useState('')
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

  const resetResolve = () => { setQuery(''); setCandidates(null); setHasMore(false); setBoundOpenID('') }
  const openCreate = () => {
    setEditing(null)
    resetResolve()
    form.setFieldsValue({ open_id: '', name: '', role: 'colleague', priority_weight: 0.4, department: null, title: null, relation: null, comm_style: null, p2p_chat_id: null, notes: null, is_active: true })
    setOpen(true)
  }
  const openEdit = (person: Person) => {
    setEditing(person)
    resetResolve()
    setBoundOpenID(person.open_id)
    form.setFieldsValue({
      open_id: person.open_id, name: person.name, role: person.role, priority_weight: person.priority_weight,
      department: person.department, title: person.title, relation: person.relation,
      comm_style: person.comm_style, p2p_chat_id: person.p2p_chat_id, notes: person.notes, is_active: person.is_active,
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
    const role = (form.getFieldValue('role') as PersonRole) || 'colleague'
    form.setFieldsValue({
      open_id: candidate.open_id, name: candidate.name,
      department: candidate.department || null, p2p_chat_id: candidate.p2p_chat_id || null,
      priority_weight: form.getFieldValue('priority_weight') ?? roleDefaultWeight[role],
    })
    setCandidates(null)
  }
  const submit = async () => {
    const values = await form.validateFields()
    if (!editing && !boundOpenID) { setError('请先搜索并选择一个飞书用户'); return }
    setSubmitting(true)
    try {
      if (editing) await updatePerson(editing.id, values)
      else await createPerson({ ...values, open_id: boundOpenID })
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
    { title: '沟通风格', dataIndex: 'comm_style', ellipsis: true, render: (v: string | null) => v || '—' },
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
      {!editing && (
        <Card size="small" style={{ marginBottom: 16 }}>
          <Flex gap={8}>
            <Input value={query} onChange={(e) => setQuery(e.target.value)} onPressEnter={runResolve} placeholder="输入姓名或邮箱搜索飞书用户" allowClear />
            <Button type="primary" onClick={runResolve} loading={searching}>搜索</Button>
          </Flex>
          {hasMore && <Alert style={{ marginTop: 8 }} type="warning" showIcon message="结果过多，请补全姓名或改用邮箱缩小范围" />}
          {candidates && candidates.length === 0 && <Alert style={{ marginTop: 8 }} type="info" showIcon message="未找到匹配用户，换个关键词试试" />}
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
        <Form.Item name="open_id" label="飞书 open_id" extra={editing ? '绑定键不可变更' : '由上方搜索选择自动绑定'}>
          <Input disabled value={boundOpenID} placeholder="搜索并选择用户后自动填入" />
        </Form.Item>
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
        <Form.Item name="comm_style" label="沟通风格(可选)" extra="辅助 AI 识别 leader 的隐含交办，如：结论先行、指令常以「看下」隐含表达"><Input.TextArea rows={2} /></Form.Item>
        <Form.Item name="notes" label="备注(可选)"><Input.TextArea rows={2} /></Form.Item>
      </Form>
    </Modal>
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

  const reload = useCallback(() => {
    setLoading(true)
    listGroups({ page, pageSize: PAGE_SIZE, relatedOnly, keyword: keyword.trim() || undefined, chatMode, tier })
      .then((result) => { setItems(result.items); setTotal(result.total); setError(undefined) })
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
    { title: '会话', dataIndex: 'name', render: (_, g) => <Text strong>{g.name || g.chat_id}</Text> },
    { title: '类型', dataIndex: 'chat_mode', width: 80, render: (m: string) => chatModeLabels[m] || m },
    { title: '分层', dataIndex: 'tier', width: 70, render: (t: string) => <Tag color={tierColors[t] || 'default'}>{tierLabels[t] || t}</Tag> },
    { title: '关联项目', width: 150, render: (_, g) => g.project?.name || '—' },
    { title: '关键群', dataIndex: 'is_key_group', width: 80, render: (v: boolean) => v ? <Tag color="volcano">是</Tag> : '—' },
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
        <Flex gap={8}>
          {g.related_group ? (
            <Popconfirm title="移出监控？将停止采集该会话" onConfirm={() => toggleRelated(g, false)} okText="移出" cancelText="取消">
              <Button size="small" danger loading={togglingId === g.id}>移出监控</Button>
            </Popconfirm>
          ) : (
            <Button size="small" type="primary" loading={togglingId === g.id} onClick={() => toggleRelated(g, true)}>纳入监控</Button>
          )}
          <Button size="small" onClick={() => openEdit(g)}>编辑背景</Button>
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
          allowClear placeholder="搜索群名 / chat_id" style={{ width: 220 }}
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
    {error && <Alert type="error" showIcon message="会话背景操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Table<Group>
        rowKey="id" columns={columns} dataSource={items} loading={loading} scroll={{ x: 1000 }}
        pagination={{ current: page, pageSize: PAGE_SIZE, total, showSizeChanger: false, onChange: setPage }}
      />
    </Card>
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
