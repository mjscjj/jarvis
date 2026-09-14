import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Flex, Form, Input, Popconfirm, Segmented, Select, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { listGroups, listProjects, updateGroupBackground, updateGroupCaptureExclusion } from '../api'
import { usePageContext } from '../pageContext'
import FactTimeline from '../world/FactTimeline'
import SummaryPageEditor from '../world/SummaryPageEditor'
import type { Group, GroupBackgroundInput, Project } from '../types'
import errorText from './errorText'

const { Text } = Typography

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
type CaptureView = 'monitored' | 'all' | 'excluded'

export default function GroupsPanel() {
  const { context, setViewState } = usePageContext()
  const [items, setItems] = useState<Group[]>([])
  const [total, setTotal] = useState(0)
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<Group | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [togglingId, setTogglingId] = useState<number>()
  const [batchSaving, setBatchSaving] = useState(false)
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [form] = Form.useForm<GroupBackgroundInput>()

  const [captureView, setCaptureView] = useState<CaptureView>(context.view_state.capture === 'excluded' ? 'excluded' : 'monitored')
  const [keyword, setKeyword] = useState('')
  const [chatMode, setChatMode] = useState<string>()
  const [tier, setTier] = useState<string>()
  const [page, setPage] = useState(1)
  const [broadened, setBroadened] = useState(false)

  const reload = useCallback(() => {
    setLoading(true)
    listGroups({
      page, pageSize: PAGE_SIZE, relatedOnly: captureView === 'monitored',
      captureState: captureView === 'excluded' ? 'excluded' : undefined,
      keyword: keyword.trim() || undefined, chatMode, tier,
    })
      .then((result) => { setItems(result.items); setTotal(result.total); setBroadened(result.broadened); setSelectedIds([]); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [page, captureView, keyword, chatMode, tier])
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

  const updateExclusion = async (groupIds: number[], excluded: boolean) => {
    setBatchSaving(true)
    try {
      await updateGroupCaptureExclusion(groupIds, excluded)
      setSelectedIds([])
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setBatchSaving(false)
    }
  }

  const columns: TableColumnsType<Group> = [
    {
      title: '会话', dataIndex: 'name', width: 280,
      render: (_, g) => <Text strong ellipsis={{ tooltip: g.name || '未命名会话' }} style={{ display: 'block' }}>{g.name || '未命名会话'}</Text>,
    },
    { title: '类型', dataIndex: 'chat_mode', width: 80, render: (m: string) => chatModeLabels[m] || m },
    { title: '分层', dataIndex: 'tier', width: 70, render: (t: string) => <Tag color={tierColors[t] || 'default'}>{tierLabels[t] || t}</Tag> },
    {
      title: '采集状态', width: 100, render: (_, g) => {
        if (g.capture_excluded) return <Tag color="red">已排除</Tag>
        if (g.related_group && g.pinned) return <Tag color="blue">手动固定</Tag>
        if (g.related_group) return <Tag color="green">自动监听</Tag>
        return <Tag>未监听</Tag>
      },
    },
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
      title: '操作', width: 260, fixed: 'right', render: (_, g) => (
        <Flex gap={8} onClick={(event) => event.stopPropagation()}>
          {g.capture_excluded ? (
            <Popconfirm title="取消排除？可监听的会话将从当前时刻恢复" onConfirm={() => updateExclusion([g.id], false)} okText="恢复" cancelText="取消">
              <Button size="small" loading={batchSaving}>取消排除</Button>
            </Popconfirm>
          ) : g.related_group ? (
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
        value={captureView}
        onChange={(value) => {
          const next = value as CaptureView
          setCaptureView(next)
          setViewState({ view: 'groups', capture: next === 'excluded' ? 'excluded' : undefined })
          resetToFirstPage()
        }}
        options={[{ value: 'monitored', label: '已监控' }, { value: 'all', label: '全部会话' }, { value: 'excluded', label: '已排除' }]}
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
      {captureView === 'monitored'
        ? `已监控 ${total} 个会话（正在按调度增量采集）`
        : captureView === 'excluded'
          ? `已排除 ${total} 个会话（不再后台采集新消息，历史数据保留）`
          : `全部 ${total} 个会话（由采集发现，纳入监控后才会采集消息）`}
    </Text>
    {selectedIds.length > 0 && (
      <Flex align="center" gap={8} style={{ marginBottom: 8 }}>
        <Text>已选择本页 {selectedIds.length} 个会话</Text>
        {captureView === 'excluded' ? (
          <Popconfirm title="取消排除？可监听的会话将从当前时刻恢复" onConfirm={() => updateExclusion(selectedIds, false)} okText="恢复" cancelText="取消">
            <Button size="small" loading={batchSaving}>批量取消排除</Button>
          </Popconfirm>
        ) : (
          <Popconfirm title="排除所选会话？将停止后台采集，历史数据仍会保留" onConfirm={() => updateExclusion(selectedIds, true)} okText="排除" cancelText="取消">
            <Button size="small" danger loading={batchSaving}>批量排除监听</Button>
          </Popconfirm>
        )}
      </Flex>
    )}
    {broadened && (
      <Alert
        style={{ marginBottom: 8 }} type="info" showIcon
        title={`「已监控」中没有匹配，已在全部会话中搜索「${keyword}」，命中 ${total} 个`}
      />
    )}
    {error && <Alert type="error" showIcon title="会话背景操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Table<Group>
        rowKey="id" columns={columns} dataSource={items} loading={loading} scroll={{ x: 1480 }}
        onRow={(group) => ({
          onClick: () => openEdit(group),
          onKeyDown: (event) => { if (event.key === 'Enter') openEdit(group) },
          className: 'clickable-row', tabIndex: 0,
        })}
        locale={{
          emptyText: keyword
            ? <Flex vertical align="center" gap={8} style={{ padding: '24px 0' }}>
                <Text type="secondary">没有匹配「{keyword}」的会话</Text>
                {captureView === 'monitored' && <Button size="small" onClick={() => { setCaptureView('all'); resetToFirstPage() }}>在全部会话中搜索</Button>}
              </Flex>
            : undefined,
        }}
        rowSelection={{
          selectedRowKeys: selectedIds,
          onChange: (keys) => setSelectedIds(keys.map(Number)),
          getCheckboxProps: (group) => ({
            disabled: (group.chat_mode !== 'group' && group.chat_mode !== 'topic' && group.chat_mode !== 'p2p') ||
              (captureView !== 'excluded' && group.capture_excluded),
          }),
          selections: true,
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
          <Descriptions.Item label="后台采集">{editing.capture_excluded ? <Tag color="red">已排除</Tag> : '允许'}</Descriptions.Item>
          <Descriptions.Item label="会话说明">{editing.description || '—'}</Descriptions.Item>
        </Descriptions>
        <Form form={form} layout="vertical">
          <Form.Item name="project_id" label="关联项目">
            <Select allowClear placeholder="不关联" options={projects.map((p) => ({ value: p.id, label: p.name }))} />
          </Form.Item>
          <Flex gap={20} wrap>
            <Form.Item name="related_group" label="纳入监控" valuePropName="checked"><Switch disabled={editing.capture_excluded} /></Form.Item>
            <Form.Item name="is_key_group" label="关键群" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="pinned" label="固定监听" tooltip="开启后不会因长期无消息或自动轮换退出监听" valuePropName="checked"><Switch /></Form.Item>
            <Form.Item name="include_in_memory" label="纳入记忆" valuePropName="checked"><Switch /></Form.Item>
          </Flex>
        </Form>
        <SummaryPageEditor type="group" id={editing.id} />
        <FactTimeline subject={{ type: 'group', id: editing.id }} title="会话事实" />
      </Space>}
    </Drawer>
  </>
}
