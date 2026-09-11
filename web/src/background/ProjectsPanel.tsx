import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Flex, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import {
  appendProjectFact,
  createProject,
  deleteProject,
  duplicateProject,
  exportProject,
  importProject,
  listProjects,
  previewProjectImport,
  resolveProjectRepositories,
  updateProject,
} from '../api'
import FactTimeline from '../world/FactTimeline'
import SummaryPageEditor from '../world/SummaryPageEditor'
import { summaryIndexLine } from '../world/summary'
import type { Project, ProjectBundle, ProjectInput, ProjectRole, ProjectStatus, RepositoryBinding } from '../types'
import errorText from './errorText'

const { Text } = Typography

const projectRoleLabels: Record<ProjectRole, string> = { owner: '负责人', participant: '参与者' }
const projectStatusLabels: Record<ProjectStatus, string> = {
  planning: '规划中', active: '进行中', paused: '暂停', archived: '归档', done: '完成',
}

export default function ProjectsPanel() {
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
