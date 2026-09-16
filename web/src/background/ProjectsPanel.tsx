import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Drawer, Flex, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Spin, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import {
  appendProjectFact,
  createProject,
  createWorldProgress,
  findWorldProgress,
  updateWorldProgress,
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
import RelatedOKRCard from '../world/RelatedOKRCard'
import ProjectOverview from '../world/ProjectOverview'
import { emptyProjectProgressDraft, hasProjectProgress, parseProjectProgress, serializeProjectProgress, type ProjectProgressDraft } from '../world/projectProgress'
import { summaryIndexLine } from '../world/summary'
import type { Project, ProjectBundle, ProjectInput, ProjectRole, ProjectStatus, RepositoryBinding, WorldProgress, WorldProgressSignal } from '../types'
import errorText from './errorText'

const { Text } = Typography

const projectRoleLabels: Record<ProjectRole, string> = { owner: '负责人', participant: '参与者' }
const projectStatusLabels: Record<ProjectStatus, string> = {
  planning: '规划中', active: '进行中', paused: '暂停', archived: '归档', done: '完成',
}

function isoWeekKey(value: Date): string {
  const date = new Date(Date.UTC(value.getFullYear(), value.getMonth(), value.getDate()))
  const weekday = date.getUTCDay() || 7
  date.setUTCDate(date.getUTCDate() + 4 - weekday)
  const yearStart = new Date(Date.UTC(date.getUTCFullYear(), 0, 1))
  const number = Math.ceil((((date.getTime() - yearStart.getTime()) / 86400000) + 1) / 7)
  return date.getUTCFullYear() + '-W' + String(number).padStart(2, '0')
}

function recentISOWeekKeys(count: number): string[] {
  const anchor = new Date()
  return Array.from({ length: count }, (_, index) => {
    const date = new Date(anchor)
    date.setDate(date.getDate() - index * 7)
    return isoWeekKey(date)
  }).filter((value, index, values) => values.indexOf(value) === index)
}

const projectProgressSignalLabels: Record<WorldProgressSignal, string> = {
  unknown: '未判断', green: '正常', yellow: '需关注', red: '有风险',
}

function ProjectProgressEditor({ projectId, autoFocus = false }: { projectId: number; autoFocus?: boolean }) {
  const periodOptions = useMemo(() => recentISOWeekKeys(8), [])
  const [periodKey, setPeriodKey] = useState(() => periodOptions[0])
  const [progress, setProgress] = useState<WorldProgress | null>(null)
  const [draft, setDraft] = useState<ProjectProgressDraft>({ ...emptyProjectProgressDraft })
  const [statusSignal, setStatusSignal] = useState<WorldProgressSignal>('unknown')
  const [loading, setLoading] = useState(true)
  const [loadSucceeded, setLoadSucceeded] = useState(false)
  const [reloadRevision, setReloadRevision] = useState(0)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setLoadSucceeded(false)
    setProgress(null)
    setDraft({ ...emptyProjectProgressDraft })
    setStatusSignal('unknown')
    setError(undefined)
    setSaved(false)
    findWorldProgress('project', String(projectId), periodKey, controller.signal)
      .then((result) => {
        if (controller.signal.aborted) return
        setProgress(result)
        setDraft(parseProjectProgress(result?.summary))
        setStatusSignal(result?.signal || 'unknown')
        setLoadSucceeded(true)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [periodKey, projectId, reloadRevision])

  const save = async () => {
    if (!hasProjectProgress(draft)) {
      setError('请填写本周进展')
      return
    }
    setSaving(true)
    setError(undefined)
    setSaved(false)
    const evidenceUntil = dayjs().endOf('day').toISOString()
    try {
      const savedProgress = progress
        ? await updateWorldProgress(progress.id, {
          expected_version: progress.version,
          signal: statusSignal,
          summary: serializeProjectProgress(draft),
          evidence: progress.evidence || {},
          evidence_until: evidenceUntil,
        })
        : await createWorldProgress({
          expected_version: 0,
          subject_type: 'project',
          subject_id: String(projectId),
          period_key: periodKey,
          signal: statusSignal,
          summary: serializeProjectProgress(draft),
          evidence: {},
          evidence_until: evidenceUntil,
        })
      setProgress(savedProgress)
      setDraft(parseProjectProgress(savedProgress.summary))
      setStatusSignal(savedProgress.signal)
      setSaved(true)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card
      size="small"
      className="project-progress-card"
      title={<div><Flex align="center" gap={8}><span>本周进展</span><Tag color="blue">{periodKey}</Tag></Flex><Text type="secondary" className="project-progress-subtitle">每周一份快照；风险在项目概览中独立维护</Text></div>}
      extra={(
        <Select
          size="small"
          value={periodKey}
          onChange={setPeriodKey}
          options={periodOptions.map((value) => ({ value, label: value }))}
        />
      )}
    >
      {error && <Alert type="error" showIcon title="进展读取或保存失败" description={error} closable onClose={() => setError(undefined)} />}
      {loading ? <div style={{ padding: 24, textAlign: 'center' }}><Spin /></div> : !loadSucceeded ? (
        <Flex justify="center" style={{ padding: 24 }}><Button onClick={() => setReloadRevision((value) => value + 1)}>重新加载</Button></Flex>
      ) : (
        <Space orientation="vertical" size={14} style={{ width: '100%' }}>
          <Flex align="center" justify="space-between" gap={12} wrap>
            <Text type="secondary">只写这一周发生的变化；稳定信息留在上方项目概览。</Text>
            <Select
              size="small"
              value={statusSignal}
              onChange={(value) => { setStatusSignal(value); setSaved(false) }}
              options={Object.entries(projectProgressSignalLabels).map(([value, label]) => ({ value, label }))}
            />
          </Flex>
          <div className="project-progress-editor-grid">
            <div className="project-progress-editor">
              <Flex justify="space-between" align="baseline" gap={8}><Text strong>本周重点</Text><Text type="secondary">最重要的 1–3 个结果</Text></Flex>
              <Input.TextArea
                value={draft.focus}
                onChange={(event) => { setDraft((current) => ({ ...current, focus: event.target.value })); setSaved(false) }}
                autoSize={{ minRows: 4, maxRows: 10 }}
                placeholder={'1. 本周必须完成什么\n2. 本周要验证什么'}
              />
            </div>
            <div className="project-progress-editor project-progress-editor-wide">
              <Flex justify="space-between" align="baseline" gap={8}><Text strong>当前进展</Text><Text type="secondary">本周新增结果和变化</Text></Flex>
              <Input.TextArea
                value={draft.progress}
                onChange={(event) => { setDraft((current) => ({ ...current, progress: event.target.value })); setSaved(false) }}
                autoSize={{ minRows: 5, maxRows: 14 }}
                autoFocus={autoFocus}
                placeholder="写结果、影响和可验证的证据，不重复长期事实"
              />
            </div>
            <div className="project-progress-editor project-progress-editor-wide">
              <Flex justify="space-between" align="baseline" gap={8}><Text strong>下周计划</Text><Text type="secondary">写可验收结果，不写笼统动作</Text></Flex>
              <Input.TextArea
                value={draft.next}
                onChange={(event) => { setDraft((current) => ({ ...current, next: event.target.value })); setSaved(false) }}
                autoSize={{ minRows: 4, maxRows: 12 }}
                placeholder={'1. 下周交付什么\n2. 如何判断已经完成'}
              />
            </div>
          </div>
          <Flex justify="space-between" align="center" gap={8} wrap>
            <Text type="secondary">{progress?.evidence_until ? `证据覆盖至 ${dayjs(progress.evidence_until).format('YYYY-MM-DD HH:mm')}` : '尚未保存本周快照'}</Text>
            <Flex align="center" gap={8}>
              {saved && <Text type="success">已保存</Text>}
              <Button type="primary" onClick={() => void save()} loading={saving}>保存进展</Button>
            </Flex>
          </Flex>
        </Space>
      )}
    </Card>
  )
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
  const [detailEntry, setDetailEntry] = useState<'overview' | 'progress'>('overview')
  const [detailTab, setDetailTab] = useState<'progress' | 'facts' | 'relations'>('progress')
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

  const openDetail = (project: Project, entry: 'overview' | 'progress' = 'overview') => {
    setDetailEntry(entry)
    setDetailTab('progress')
    setDetail(project)
  }

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
              openDetail(created)
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
      openDetail(created)
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
      title: '操作', width: 270, render: (_, p) => (
        <Flex gap={8}>
          <Button size="small" type="primary" onClick={(event) => { event.stopPropagation(); openDetail(p, 'progress') }}>进展</Button>
          <Button size="small" onClick={(event) => { event.stopPropagation(); openDetail(p) }}>详情</Button>
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
    <Card className="table-card" variant="borderless"><Table<Project> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} onRow={(project) => ({ onClick: () => openDetail(project), className: 'clickable-row' })} /></Card>
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
    <Drawer
      title={detail ? (
        <div className="project-detail-title">
          <Text strong>{detail.name}</Text>
          <Flex gap={8} align="center" wrap>
            <Tag color="green">{projectStatusLabels[detail.status]}</Tag>
            <Text type="secondary">{projectRoleLabels[detail.role]}</Text>
            <Text type="secondary">优先级 P{detail.priority}</Text>
            {detail.last_progress_at && <Text type="secondary">最近更新 {dayjs(detail.last_progress_at).format('M-D HH:mm')}</Text>}
          </Flex>
        </div>
      ) : '项目详情'}
      open={Boolean(detail)}
      size={900}
      onClose={() => { setDetail(undefined); setDetailEntry('overview'); setDetailTab('progress') }}
    >
      {detail && <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Flex gap={8} wrap>
          <Button onClick={() => void downloadProject(detail)}>分享项目</Button>
          <Button onClick={() => openCopy(detail)}>复制项目</Button>
          <Button loading={resolvingRepositories} onClick={() => void resolveRepositories(detail)}>扫描 Codebase 仓库</Button>
          <Button type="text" onClick={() => openEdit(detail)}>管理项目</Button>
        </Flex>
        <Tabs
          activeKey={detailTab}
          onChange={(value) => setDetailTab(value as typeof detailTab)}
          items={[
            {
              key: 'progress',
              label: '进展',
              children: (
                <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                  <ProjectOverview project={detail} />
                  <ProjectProgressEditor projectId={detail.id} autoFocus={detailEntry === 'progress'} />
                  <SummaryPageEditor type="project" id={detail.id} defaultCollapsed />
                </Space>
              ),
            },
            {
              key: 'facts',
              label: '事实流',
              children: (
                <FactTimeline
                  subject={{ type: 'project', id: detail.id }}
                  title="项目事实"
                  refreshToken={eventRefresh}
                  extra={<Button size="small" type="primary" onClick={() => setEventOpen(true)}>记录事实</Button>}
                />
              ),
            },
            {
              key: 'relations',
              label: '关联',
              children: (
                <Space orientation="vertical" size={16} style={{ width: '100%' }}>
                  <RelatedOKRCard type="project" id={detail.id} />
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
                </Space>
              ),
            },
          ]}
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
