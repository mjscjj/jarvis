import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Flex, Form, Input, Modal, Popconfirm, Segmented, Select, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import { useAgentIdentity } from '../agentIdentity'
import { createResource, deleteResource, listPersons, listProjects, listResources, touchResource, updateResource } from '../api'
import FactTimeline from '../world/FactTimeline'
import SummaryPageEditor from '../world/SummaryPageEditor'
import { summaryIndexLine } from '../world/summary'
import type { Person, Project, Resource, ResourceInput, ResourceType } from '../types'
import errorText from './errorText'

const { Text } = Typography

const resourceTypeLabels: Record<ResourceType, string> = {
  doc: '文档', link: '链接', repo: '仓库', note: '笔记', other: '其他',
}

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

export default function ResourcePanel() {
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
