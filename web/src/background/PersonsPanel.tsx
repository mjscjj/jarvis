import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Collapse, Drawer, Flex, Form, Input, InputNumber, Popconfirm, Segmented, Select, Space, Switch, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { createPerson, deletePerson, listPersons, resolvePerson, updatePerson } from '../api'
import { personToUpdateInput } from '../persons'
import FactTimeline from '../world/FactTimeline'
import SummaryPageEditor from '../world/SummaryPageEditor'
import { summaryIndexLine } from '../world/summary'
import type { Person, PersonRole, PersonUpdateInput, ResolveCandidate } from '../types'
import errorText from './errorText'

const { Text } = Typography

const personRoleLabels: Record<PersonRole, string> = {
  leader: 'Leader', key: '关键干系人', colleague: '同事', other: '其他',
}

const roleDefaultWeight: Record<PersonRole, number> = { leader: 1.0, key: 0.7, colleague: 0.4, other: 0.1 }

type PersonRoleFilter = 'all' | PersonRole

export default function PersonsPanel() {
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
