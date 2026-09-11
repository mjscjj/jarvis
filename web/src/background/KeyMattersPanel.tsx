import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, DatePicker, Descriptions, Drawer, Flex, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { closeKeyMatter, createKeyMatter, listKeyMatters, listProjects, touchKeyMatter, updateKeyMatter } from '../api'
import { keyMatterToInput, replaceKeyMatter } from '../keyMatters'
import FactTimeline from '../world/FactTimeline'
import SummaryPageEditor from '../world/SummaryPageEditor'
import { summaryIndexLine } from '../world/summary'
import type { KeyMatter, KeyMatterInput, Project } from '../types'
import errorText from './errorText'

const { Text } = Typography

type KeyMatterField = 'status' | 'due_at'

interface KeyMatterCreateFields {
  title: string
  status?: string
  project_id?: number
  due_at?: Dayjs
}

export default function KeyMattersPanel() {
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
