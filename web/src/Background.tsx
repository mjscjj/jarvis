import { useCallback, useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Drawer,
  Empty,
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
  Timeline,
  Tooltip,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  appendProjectEvent,
  createPerson,
  createProject,
  createResource,
  createWorkRule,
  createTextStorage,
  deletePerson,
  deleteProject,
  deleteResource,
  deleteWorkRule,
  deleteTextStorage,
  getProfile,
  getSkillContent,
  listGroups,
  listPersons,
  listProjectEvents,
  listProjects,
  listResources,
  listSkills,
  listWorkRules,
  listTextStorage,
  resolvePerson,
  scanSkills,
  updateGroupBackground,
  updatePerson,
  updateProfile,
  updateProject,
  updateResource,
  updateSkill,
  updateWorkRule,
  updateTextStorage,
} from './api'
import SharedMemory from './SharedMemory'
import RuntimeSettings from './RuntimeSettings'
import EntityRelations from './components/EntityRelations'
import type {
  AgentSkill,
  AgentSkillInput,
  Group,
  GroupBackgroundInput,
  Person,
  PersonInput,
  PersonRole,
  ProfileInput,
  ProfileView,
  Project,
  ProjectEvent,
  ProjectInput,
  ProjectRole,
  ProjectStatus,
  ResolveCandidate,
  Resource,
  ResourceInput,
  ResourceType,
  SkillStage,
  WorkRule,
  WorkRuleInput,
  WorkRuleStage,
  TextStorage,
  TextStorageInput,
} from './types'

const { Text } = Typography

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
const personRoleColors: Record<PersonRole, string> = {
  leader: 'volcano', key: 'gold', colleague: 'blue', other: 'default',
}

const workRuleStageLabels: Record<WorkRuleStage, string> = {
  extract: 'M3 抽取 Todo', decide: 'M4 决策', execute: 'M5 执行',
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
  const [events, setEvents] = useState<ProjectEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState<string>()
  const [eventOpen, setEventOpen] = useState(false)
  const [eventDescription, setEventDescription] = useState('')
  const [eventSubmitting, setEventSubmitting] = useState(false)
  const [eventRefresh, setEventRefresh] = useState(0)

  const reload = useCallback(() => {
    setLoading(true)
    listProjects()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  useEffect(() => {
    if (!detail) { setEvents([]); setEventsError(undefined); return }
    const controller = new AbortController()
    setEventsLoading(true)
    setEventsError(undefined)
    listProjectEvents(detail.id, controller.signal)
      .then((result) => setEvents(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setEventsError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setEventsLoading(false) })
    return () => controller.abort()
  }, [detail, eventRefresh])

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
      await appendProjectEvent(detail.id, eventDescription.trim())
      setEventDescription('')
      setEventOpen(false)
      setEventRefresh((value) => value + 1)
      setEventsError(undefined)
    } catch (cause: unknown) {
      setEventsError(errorText(cause))
    } finally {
      setEventSubmitting(false)
    }
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
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建项目</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon message="项目操作失败" description={error} closable onClose={() => setError(undefined)} />}
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
        <Form.Item name="description" label="描述(可选)"><Input.TextArea rows={2} /></Form.Item>
        <Form.Item name="notes" label="备注(可选)"><Input.TextArea rows={2} /></Form.Item>
      </Form>
    </Modal>
    <Drawer title={detail?.name || '项目详情'} open={Boolean(detail)} size={720} onClose={() => setDetail(undefined)}>
      {detail && <Space orientation="vertical" size={20} style={{ width: '100%' }}>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="状态"><Tag>{projectStatusLabels[detail.status]}</Tag></Descriptions.Item>
          <Descriptions.Item label="我的角色">{projectRoleLabels[detail.role]}</Descriptions.Item>
          <Descriptions.Item label="优先级">{detail.priority}</Descriptions.Item>
          <Descriptions.Item label="项目代号">{detail.code || '—'}</Descriptions.Item>
          <Descriptions.Item label="项目描述" span={2}>{detail.description || '—'}</Descriptions.Item>
          <Descriptions.Item label="备注" span={2}>{detail.notes || '—'}</Descriptions.Item>
        </Descriptions>
        <Card size="small" title="项目进展" variant="borderless" extra={<Button size="small" type="primary" onClick={() => setEventOpen(true)}>记录进展</Button>}>
          {eventsError && <Alert type="error" showIcon title="项目进展加载失败" description={eventsError} style={{ marginBottom: 12 }} />}
          {eventsLoading ? (
            <div style={{ padding: 16, textAlign: 'center' }}><Spin size="small" /></div>
          ) : events.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无项目进展" />
          ) : (
            <Timeline items={events.map((event) => ({
              content: <div><div style={{ whiteSpace: 'pre-wrap' }}>{event.description}</div><Text type="secondary" style={{ fontSize: 12 }}>{new Date(event.occurred_at).toLocaleString()}</Text></div>,
            }))} />
          )}
        </Card>
        <EntityRelations entityType="project" entityId={detail.id} />
      </Space>}
    </Drawer>
    <Modal title="记录项目进展" open={eventOpen} confirmLoading={eventSubmitting} onOk={recordEvent} onCancel={() => setEventOpen(false)} okText="记录">
      <Input.TextArea rows={6} value={eventDescription} onChange={(event) => setEventDescription(event.target.value)} placeholder="写清楚发生了什么、当前结果和下一步。" />
    </Modal>
  </>
}

// --- Persons ---

const roleDefaultWeight: Record<PersonRole, number> = { leader: 1.0, key: 0.7, colleague: 0.4, other: 0.1 }

type PersonRoleFilter = 'all' | PersonRole

// personToInput projects a stored Person back into the update payload so an
// inline edit patches exactly one field without dropping the rest.
function personToInput(person: Person): PersonInput {
  return {
    open_id: person.open_id, name: person.name, role: person.role, priority_weight: person.priority_weight,
    department: person.department, title: person.title, relation: person.relation,
    comm_style: person.comm_style, p2p_chat_id: person.p2p_chat_id, notes: person.notes, is_active: person.is_active,
  }
}

function PersonsPanel() {
  const [items, setItems] = useState<Person[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Person | null>(null)
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [roleFilter, setRoleFilter] = useState<PersonRoleFilter>('all')
  const [savingId, setSavingId] = useState<number>()
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

  // patchPerson performs an inline single-field update straight from the list.
  const patchPerson = async (person: Person, patch: Partial<PersonInput>) => {
    setSavingId(person.id)
    try {
      await updatePerson(person.id, { ...personToInput(person), ...patch })
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingId(undefined)
    }
  }

  const visibleItems = roleFilter === 'all' ? items : items.filter((p) => p.role === roleFilter)

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
    {
      title: '姓名', dataIndex: 'name',
      render: (_, p) => <Tooltip title={`open_id: ${p.open_id}`}><Text strong>{p.name}</Text></Tooltip>,
    },
    {
      title: '角色', dataIndex: 'role', width: 140,
      // 行内直接改角色，同时把权重联动为该角色默认值（规范：列表页优先行内编辑）。
      render: (r: PersonRole, p) => (
        <Select<PersonRole> size="small" variant="borderless" value={r} disabled={savingId === p.id} style={{ width: 120 }}
          onChange={(role) => patchPerson(p, { role, priority_weight: roleDefaultWeight[role] })}
          options={Object.entries(personRoleLabels).map(([value, label]) => ({ value, label }))} />
      ),
    },
    { title: '权重', dataIndex: 'priority_weight', width: 80 },
    { title: '部门/职位', width: 200, render: (_, p) => [p.department, p.title].filter(Boolean).join(' · ') || '—' },
    { title: '沟通风格', dataIndex: 'comm_style', ellipsis: true, render: (v: string | null) => v || '—' },
    {
      title: '启用', dataIndex: 'is_active', width: 70,
      render: (v: boolean, p) => <Switch size="small" checked={v} loading={savingId === p.id} onChange={(next) => patchPerson(p, { is_active: next })} />,
    },
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
    {error && <Alert type="error" showIcon message="人物操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Person> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} scroll={{ x: 900 }} /></Card>
    <Modal title={editing ? '编辑人物' : '新建人物'} open={open} confirmLoading={submitting} onOk={submit} onCancel={() => setOpen(false)} okText="保存" destroyOnHidden width={720}>
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
      {editing && <EntityRelations entityType="person" entityId={editing.id} />}
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
    { title: '会话', dataIndex: 'name', render: (_, g) => <Text strong>{g.name || g.chat_id}</Text> },
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
          allowClear placeholder="搜索群名 / 群主 / 项目 / chat_id" style={{ width: 260 }}
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
        message={`「已监控」中没有匹配，已在全部会话中搜索「${keyword}」，命中 ${total} 个`}
      />
    )}
    {error && <Alert type="error" showIcon message="会话背景操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Table<Group>
        rowKey="id" columns={columns} dataSource={items} loading={loading} scroll={{ x: 1000 }}
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

// --- Profile (decision-maker "me") ---

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
          background: result.background, preferences: result.preferences,
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
    {error && <Alert type="error" showIcon message="保存失败" description={error} closable onClose={() => setError(undefined)} style={{ marginBottom: 12 }} />}
    {ok && <Alert type="success" showIcon message="已保存，抽取时会把「我的背景」喂给模型" closable onClose={() => setOk(false)} style={{ marginBottom: 12 }} />}
    {profile && !profile.saved && <Alert type="info" showIcon message="首次填写：决策主体（我）背景尚未设置，完善后可显著提升 leader 软措辞交办的识别" style={{ marginBottom: 12 }} />}
    <Card variant="borderless" loading={loading} style={{ maxWidth: 720 }}>
      <Form form={form} layout="vertical">
        <Form.Item label="open_id（由配置固定）">
          <Input value={profile?.open_id} disabled />
        </Form.Item>
        <Form.Item name="name" label="姓名（当前用户是谁）" rules={[{ required: true, message: '请填写姓名' }]}>
          <Input placeholder="如：储节节" />
        </Form.Item>
        <Flex gap={12}>
          <Form.Item name="department" label="部门" style={{ flex: 1 }}><Input placeholder="选填" /></Form.Item>
          <Form.Item name="title" label="职位" style={{ flex: 1 }}><Input placeholder="选填" /></Form.Item>
        </Flex>
        <Form.Item name="background" label="背景 / 负责方向" tooltip="我是谁、负责什么方向，会作为抽取上下文喂给模型">
          <Input.TextArea rows={3} placeholder="如：研发工程师，负责公会 Agent 基建（runtime / skill 治理 / 自建活动 AI 助手）" />
        </Form.Item>
        <Form.Item name="preferences" label="喜好 / 工作偏好" tooltip="沟通与工作偏好，帮助模型贴合你的习惯">
          <Input.TextArea rows={2} placeholder="如：偏好先给结论再展开；紧急事项直接同步" />
        </Form.Item>
        <Form.Item label="直属 leader" tooltip="显式告诉模型「我的 leader 是谁」，对识别 leader 软措辞交办最关键">
          {leaderOpenID ? (
            <Flex gap={8} align="center">
              <Tag color="gold">{leaderName || leaderOpenID}</Tag>
              <Text type="secondary" style={{ fontSize: 12 }}>{leaderOpenID}</Text>
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
        <Button type="primary" loading={saving} onClick={submit}>保存</Button>
      </Form>
    </Card>
  </>
}

// --- Resources ---

// resourceToInput projects a stored Resource back into the update payload so an
// inline edit patches exactly one field without dropping the rest.
function resourceToInput(resource: Resource): ResourceInput {
  return {
    title: resource.title, resource_type: resource.resource_type, url: resource.url,
    description: resource.description, person_id: resource.person_id, project_id: resource.project_id,
    link_principal: resource.link_principal, is_active: resource.is_active,
  }
}

function ResourcePanel() {
  const [items, setItems] = useState<Resource[]>([])
  const [persons, setPersons] = useState<Person[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [savingId, setSavingId] = useState<number>()
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<ResourceInput>()

  const reload = useCallback(() => {
    setLoading(true)
    Promise.all([listResources(), listPersons(), listProjects()])
      .then(([resourceResult, personResult, projectResult]) => {
        setItems(resourceResult.items)
        setPersons(personResult.items)
        setProjects(projectResult.items)
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  // patchResource performs an inline single-field update straight from the list.
  const patchResource = async (resource: Resource, patch: Partial<ResourceInput>) => {
    setSavingId(resource.id)
    try {
      await updateResource(resource.id, { ...resourceToInput(resource), ...patch })
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingId(undefined)
    }
  }

  const openCreate = () => {
    form.setFieldsValue({ title: '', resource_type: 'link', url: null, description: null, person_id: null, project_id: null, link_principal: false, is_active: true })
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
    try { await deleteResource(resource.id); reload() } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const personOptions = [{ value: 0, label: '—' }, ...persons.map((p) => ({ value: p.id, label: p.name }))]
  const projectOptions = [{ value: 0, label: '—' }, ...projects.map((p) => ({ value: p.id, label: p.name }))]

  const columns: TableColumnsType<Resource> = [
    {
      title: '名称', dataIndex: 'title', render: (_, r) => (
        <Input size="small" defaultValue={r.title} disabled={savingId === r.id}
          onBlur={(e) => { const v = e.target.value.trim(); if (v && v !== r.title) patchResource(r, { title: v }) }} />
      ),
    },
    {
      title: '类型', dataIndex: 'resource_type', width: 110, render: (t: ResourceType, r) => (
        <Select size="small" value={t} style={{ width: '100%' }} disabled={savingId === r.id}
          options={Object.entries(resourceTypeLabels).map(([value, label]) => ({ value, label }))}
          onChange={(value) => patchResource(r, { resource_type: value as ResourceType })} />
      ),
    },
    {
      title: '链接/地址', dataIndex: 'url', render: (_, r) => (
        <Input size="small" defaultValue={r.url ?? ''} placeholder="可选" disabled={savingId === r.id}
          onBlur={(e) => { const v = e.target.value.trim(); if (v !== (r.url ?? '')) patchResource(r, { url: v || null }) }} />
      ),
    },
    {
      title: '关联人', dataIndex: 'person_id', width: 130, render: (_, r) => (
        <Select size="small" value={r.person_id ?? 0} style={{ width: '100%' }} disabled={savingId === r.id}
          options={personOptions} showSearch optionFilterProp="label"
          onChange={(value) => patchResource(r, { person_id: value === 0 ? null : value })} />
      ),
    },
    {
      title: '关联项目', dataIndex: 'project_id', width: 150, render: (_, r) => (
        <Select size="small" value={r.project_id ?? 0} style={{ width: '100%' }} disabled={savingId === r.id}
          options={projectOptions} showSearch optionFilterProp="label"
          onChange={(value) => patchResource(r, { project_id: value === 0 ? null : value })} />
      ),
    },
    {
      title: '关联我', dataIndex: 'link_principal', width: 70, align: 'center', render: (v: boolean, r) => (
        <Switch size="small" checked={v} loading={savingId === r.id}
          onChange={(checked) => patchResource(r, { link_principal: checked })} />
      ),
    },
    {
      title: '启用', dataIndex: 'is_active', width: 70, align: 'center', render: (v: boolean, r) => (
        <Switch size="small" checked={v} loading={savingId === r.id}
          onChange={(checked) => patchResource(r, { is_active: checked })} />
      ),
    },
    {
      title: '操作', width: 80, render: (_, r) => (
        <Popconfirm title="删除该资源？" onConfirm={() => remove(r)} okText="删除" cancelText="取消">
          <Button size="small" danger>删除</Button>
        </Popconfirm>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个资源</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建资源</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon message="资源操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<Resource> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} /></Card>
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
        <Form.Item name="description" label="说明/备注(可选)"><Input.TextArea rows={2} /></Form.Item>
      </Form>
    </Modal>
  </>
}

// --- Work rules ---

function WorkRulesPanel() {
  const [items, setItems] = useState<WorkRule[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<WorkRule | null>(null)
  const [open, setOpen] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<WorkRuleInput>()
  const ruleType = Form.useWatch('rule_type', form)

  const reload = useCallback(() => {
    setLoading(true)
    listWorkRules()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const openCreate = () => {
    setEditing(null)
    form.setFieldsValue({ name: '', content: '', rule_type: 'all', stages: [], priority: 100, is_enabled: true })
    setOpen(true)
  }
  const openEdit = (rule: WorkRule) => {
    setEditing(rule)
    form.setFieldsValue({
      name: rule.name, content: rule.content, rule_type: rule.rule_type,
      stages: rule.stages, priority: rule.priority, is_enabled: rule.is_enabled,
    })
    setOpen(true)
  }
  const submit = async () => {
    const values = await form.validateFields()
    const input = { ...values, stages: values.rule_type === 'all' ? [] : values.stages }
    setSubmitting(true)
    try {
      if (editing) await updateWorkRule(editing.id, input)
      else await createWorkRule(input)
      setOpen(false)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const remove = async (rule: WorkRule) => {
    try { await deleteWorkRule(rule.id); reload() } catch (cause: unknown) { setError(errorText(cause)) }
  }
  const toggle = async (rule: WorkRule, checked: boolean) => {
    try {
      await updateWorkRule(rule.id, {
        name: rule.name, content: rule.content, rule_type: rule.rule_type,
        stages: rule.stages, priority: rule.priority, is_enabled: checked,
      })
      reload()
    } catch (cause: unknown) { setError(errorText(cause)) }
  }

  const columns: TableColumnsType<WorkRule> = [
    { title: '规则', dataIndex: 'name', width: 180, render: (name: string) => <Text strong>{name}</Text> },
    { title: '内容', dataIndex: 'content', ellipsis: true },
    {
      title: '生效阶段', width: 270, render: (_, rule) => rule.rule_type === 'all'
        ? <Tag color="blue">M3 / M4 / M5 全阶段</Tag>
        : <Flex gap={4} wrap>{rule.stages.map((stage) => <Tag key={stage}>{workRuleStageLabels[stage]}</Tag>)}</Flex>,
    },
    { title: '优先级', dataIndex: 'priority', width: 80 },
    {
      title: '启用', dataIndex: 'is_enabled', width: 70, align: 'center',
      render: (enabled: boolean, rule) => <Switch size="small" checked={enabled} onChange={(checked) => toggle(rule, checked)} />,
    },
    {
      title: '操作', width: 150, render: (_, rule) => (
        <Flex gap={8}>
          <Button size="small" onClick={() => openEdit(rule)}>编辑</Button>
          <Popconfirm title="删除该工作规则？" onConfirm={() => remove(rule)} okText="删除" cancelText="取消">
            <Button size="small" danger>删除</Button>
          </Popconfirm>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 条规则</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={openCreate}>新建规则</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon title="工作规则操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<WorkRule> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} /></Card>
    <Modal title={editing ? '编辑工作规则' : '新建工作规则'} open={open} confirmLoading={submitting} onOk={submit} onCancel={() => setOpen(false)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="name" label="规则名称" rules={[{ required: true, message: '请输入规则名称' }]}><Input placeholder="如：飞书消息发送方式" /></Form.Item>
        <Form.Item name="content" label="规则内容" rules={[{ required: true, message: '请输入规则内容' }]}><Input.TextArea rows={5} placeholder="用自然语言说明 Agent 应怎样工作" /></Form.Item>
        <Flex gap={16}>
          <Form.Item name="rule_type" label="适用类型" rules={[{ required: true }]} style={{ flex: 1 }}>
            <Segmented block options={[{ label: '全部阶段', value: 'all' }, { label: '指定阶段', value: 'selected' }]} />
          </Form.Item>
          <Form.Item name="priority" label="优先级（越小越靠前）" rules={[{ required: true }]} style={{ width: 180 }}><InputNumber min={1} style={{ width: '100%' }} /></Form.Item>
        </Flex>
        {ruleType === 'selected' && (
          <Form.Item name="stages" label="生效阶段" rules={[{ required: true, type: 'array', min: 1, message: '至少选择一个阶段' }]}>
            <Select mode="multiple" options={Object.entries(workRuleStageLabels).map(([value, label]) => ({ value, label }))} />
          </Form.Item>
        )}
        <Form.Item name="is_enabled" label="立即启用" valuePropName="checked"><Switch /></Form.Item>
      </Form>
    </Modal>
  </>
}

// --- Approval rule (stored in generic text storage) ---

const approvalRuleStorageKey = 'm5_approval_rule'

function ApprovalRulesPanel() {
  const [record, setRecord] = useState<TextStorage | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [ok, setOk] = useState(false)
  const [form] = Form.useForm<TextStorageInput>()

  const reload = useCallback(() => {
    setLoading(true)
    listTextStorage()
      .then((result) => {
        const found = result.items.find((item) => item.storage_key === approvalRuleStorageKey) ?? null
        setRecord(found)
        form.setFieldsValue({
          storage_key: approvalRuleStorageKey,
          name: '审批规则',
          content: found?.content ?? '',
        })
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [form])
  useEffect(reload, [reload])

  const save = async () => {
    const values = await form.validateFields()
    const input: TextStorageInput = {
      storage_key: approvalRuleStorageKey,
      name: '审批规则',
      content: values.content,
    }
    setSaving(true)
    try {
      const updated = record
        ? await updateTextStorage(record.id, input)
        : await createTextStorage(input)
      setRecord(updated)
      setOk(true)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    if (!record) return
    try {
      await deleteTextStorage(record.id)
      setRecord(null)
      form.setFieldValue('content', '')
      setOk(false)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  return <>
    {error && <Alert type="error" showIcon message="审批规则操作失败" description={error} closable onClose={() => setError(undefined)} style={{ marginBottom: 12 }} />}
    {ok && <Alert type="success" showIcon message="审批规则已保存，后续 M5 批准执行会实时读取" closable onClose={() => setOk(false)} style={{ marginBottom: 12 }} />}
    {!record && !loading && <Alert type="warning" showIcon message="审批规则不存在，M5 批准后的落地执行会失败；请填写并保存。" style={{ marginBottom: 12 }} />}
    <Card loading={loading} variant="borderless">
      <Form form={form} layout="vertical" initialValues={{ storage_key: approvalRuleStorageKey, name: '审批规则', content: '' }}>
        <Form.Item name="content" label="M5 批准后执行规则" rules={[{ required: true, whitespace: true, message: '请输入审批规则' }]}
          extra="这段文本会作为可信规则注入 M5 的批准后落地提示词；修改后对后续执行实时生效。">
          <Input.TextArea rows={16} placeholder="填写批准后执行必须遵守的规则" style={{ fontFamily: 'monospace' }} />
        </Form.Item>
        <Flex gap={8}>
          <Button type="primary" onClick={save} loading={saving}>{record ? '保存修改' : '创建审批规则'}</Button>
          <Button onClick={reload} loading={loading}>刷新</Button>
          {record && (
            <Popconfirm title="删除审批规则？" description="删除后，M5 批准后的落地执行会直接失败。" onConfirm={remove} okText="删除" cancelText="取消">
              <Button danger>删除</Button>
            </Popconfirm>
          )}
        </Flex>
      </Form>
    </Card>
  </>
}

// --- M3/M4/M5 system prompts (stored in generic text storage) ---

const systemPromptDefinitions = [
  {
    key: 'm3_system_prompt',
    name: 'M3 抽取',
    storageName: 'M3 系统提示词',
    description: '定义行动线索抽取者的角色、判断原则和输出要求。',
  },
  {
    key: 'm4_system_prompt',
    name: 'M4 决策',
    storageName: 'M4 系统提示词',
    description: '定义行动决策者的角色、处置原则和阶段安全边界。',
  },
  {
    key: 'm5_system_prompt',
    name: 'M5 执行',
    storageName: 'M5 系统提示词',
    description: 'direct、propose、apply 和 Session 恢复共用；具体阶段、审批产物及输出 Schema 由运行时动态追加。',
  },
] as const

type SystemPromptKey = typeof systemPromptDefinitions[number]['key']

function SystemPromptsPanel() {
  const [records, setRecords] = useState<Partial<Record<SystemPromptKey, TextStorage>>>({})
  const [drafts, setDrafts] = useState<Partial<Record<SystemPromptKey, string>>>({})
  const [activeKey, setActiveKey] = useState<SystemPromptKey>(systemPromptDefinitions[0].key)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [ok, setOk] = useState(false)

  const reload = useCallback(() => {
    setLoading(true)
    listTextStorage()
      .then((result) => {
        const nextRecords: Partial<Record<SystemPromptKey, TextStorage>> = {}
        const nextDrafts: Partial<Record<SystemPromptKey, string>> = {}
        for (const definition of systemPromptDefinitions) {
          const found = result.items.find((item) => item.storage_key === definition.key)
          if (found) {
            nextRecords[definition.key] = found
            nextDrafts[definition.key] = found.content
          } else {
            nextDrafts[definition.key] = ''
          }
        }
        setRecords(nextRecords)
        setDrafts(nextDrafts)
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const definition = systemPromptDefinitions.find((item) => item.key === activeKey)!
  const record = records[activeKey]
  const content = drafts[activeKey] ?? ''

  const save = async () => {
    if (!content.trim()) {
      setError(`${definition.name}提示词不能为空`)
      return
    }
    const input: TextStorageInput = {
      storage_key: definition.key,
      name: definition.storageName,
      content,
    }
    setSaving(true)
    try {
      const updated = record
        ? await updateTextStorage(record.id, input)
        : await createTextStorage(input)
      setRecords((current) => ({ ...current, [activeKey]: updated }))
      setDrafts((current) => ({ ...current, [activeKey]: updated.content }))
      setOk(true)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  return <>
    {error && <Alert type="error" showIcon message="系统提示词操作失败" description={error} closable onClose={() => setError(undefined)} style={{ marginBottom: 12 }} />}
    {ok && <Alert type="success" showIcon message={`${definition.name}提示词已保存，后续对应执行实时读取`} closable onClose={() => setOk(false)} style={{ marginBottom: 12 }} />}
    <Alert
      type="info"
      showIcon
      message="这些内容复用通用 text_storage，不单独建表"
      description="M3、M4、M5 会实时读取对应系统提示词。工具说明由工具层维护，Skills 由 Skills 页维护；当前阶段、任务上下文、审批产物和 JSON 输出协议由代码动态组装。"
      style={{ marginBottom: 12 }}
    />
    <Card loading={loading} variant="borderless">
      <Tabs
        tabPosition="left"
        activeKey={activeKey}
        onChange={(key) => { setActiveKey(key as SystemPromptKey); setOk(false); setError(undefined) }}
        items={systemPromptDefinitions.map((item) => ({
          key: item.key,
          label: item.name,
          children: (
            <>
              {!records[item.key] && (
                <Alert type="warning" showIcon message={`${item.name}提示词不存在，对应阶段会 fail-fast；请填写并保存。`} style={{ marginBottom: 12 }} />
              )}
              <Text strong>{item.storageName}</Text>
              <div><Text type="secondary">{item.description}</Text></div>
              <div style={{ margin: '8px 0 12px' }}><Text code>{item.key}</Text></div>
              <Input.TextArea
                value={drafts[item.key] ?? ''}
                onChange={(event) => setDrafts((current) => ({ ...current, [item.key]: event.target.value }))}
                autoSize={{ minRows: 16, maxRows: 30 }}
                placeholder={`填写${item.name}系统提示词`}
                style={{ fontFamily: 'monospace' }}
              />
              <Flex gap={8} style={{ marginTop: 12 }}>
                <Button type="primary" onClick={save} loading={saving}>{records[item.key] ? '保存修改' : '创建提示词'}</Button>
                <Button onClick={reload} loading={loading}>刷新</Button>
              </Flex>
            </>
          ),
        }))}
      />
    </Card>
  </>
}

// --- Skills ---

function SkillsPanel() {
  const [items, setItems] = useState<AgentSkill[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<AgentSkill | null>(null)
  const [content, setContent] = useState<{ name: string; text: string } | null>(null)
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
      await updateSkill(editing.id, values)
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
      await updateSkill(item.id, { stages: item.stages, is_enabled: checked })
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }
  const showContent = async (item: AgentSkill) => {
    try {
      const result = await getSkillContent(item.name)
      setContent({ name: result.name, text: result.content })
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
    <Card className="table-card" variant="borderless"><Table<AgentSkill> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} /></Card>
    <Modal title={`设置 Skill 范围 · ${editing?.name || ''}`} open={Boolean(editing)} confirmLoading={submitting} onOk={submit} onCancel={() => setEditing(null)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="stages" label="生效阶段" rules={[{ required: true, type: 'array', min: 1, message: '至少选择一个阶段' }]}>
          <Select mode="multiple" options={Object.entries(workRuleStageLabels).map(([value, label]) => ({ value: value as SkillStage, label }))} />
        </Form.Item>
        <Form.Item name="is_enabled" label="启用" valuePropName="checked"><Switch /></Form.Item>
      </Form>
    </Modal>
    <Modal title={`Skill · ${content?.name || ''}`} open={Boolean(content)} onCancel={() => setContent(null)} footer={null} width={760}>
      <Input.TextArea value={content?.text} readOnly autoSize={{ minRows: 12, maxRows: 24 }} style={{ fontFamily: 'monospace' }} />
    </Modal>
  </>
}

export default function Background() {
  return (
    <Tabs
      items={[
        { key: 'profile', label: '我（决策主体）', children: <ProfilePanel /> },
        { key: 'projects', label: '项目', children: <ProjectsPanel /> },
        { key: 'persons', label: '人物', children: <PersonsPanel /> },
        { key: 'groups', label: '会话背景', children: <GroupsPanel /> },
        { key: 'resources', label: '资源', children: <ResourcePanel /> },
      ]}
    />
  )
}

export function Settings() {
  return (
    <Tabs
      items={[
        { key: 'runtime-settings', label: '运行配置', children: <RuntimeSettings /> },
        { key: 'work-rules', label: '工作规则', children: <WorkRulesPanel /> },
        { key: 'system-prompts', label: '系统提示词', children: <SystemPromptsPanel /> },
        { key: 'approval-rules', label: '审批规则管理', children: <ApprovalRulesPanel /> },
        { key: 'skills', label: 'Skills', children: <SkillsPanel /> },
        { key: 'shared-memory', label: '共享记忆', children: <SharedMemory /> },
      ]}
    />
  )
}
