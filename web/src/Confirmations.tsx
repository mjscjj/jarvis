import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Collapse, Descriptions, Drawer, Flex, Input, Modal, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { approveConfirmation, getConfirmation, listConfirmations, rejectConfirmation, supplementConfirmation } from './api'
import type { ConfirmationDetail, ContextSnapshot, Resolution, Todo } from './types'
import { TodoContextPanel } from './slots'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { todoStatusMeta } from './status'

const { Paragraph, Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

type ConfirmModal = 'approve' | 'reject' | 'supplement' | undefined
function modalTitle(modal: ConfirmModal): string {
  if (modal === 'approve') return '确认执行方案'
  if (modal === 'supplement') return '补充信息 / 指示'
  return '填写拒绝原因'
}
function modalOkText(modal: ConfirmModal): string {
  if (modal === 'approve') return '批准'
  if (modal === 'supplement') return '提交并重新判定'
  return '确认拒绝'
}
function modalPlaceholder(modal: ConfirmModal): string {
  if (modal === 'approve') return '非空 JSON 方案，可以是字符串、对象或数组'
  if (modal === 'supplement') return '补充信息或写下你的指示（如具体仓库、目标、范围、截止时间，或"优先按 X 方案""不要动 Z"等），供决策器重新判定'
  return '拒绝原因'
}

const RESOLUTION_METHOD_LABEL: Record<Resolution['method'], { text: string; color: string }> = {
  group_bound: { text: '群绑定项目', color: 'green' },
  project_hint: { text: '模型提示匹配', color: 'blue' },
  codex_cli: { text: 'codex 自查推算', color: 'geekblue' },
  unresolved: { text: '未推算出项目', color: 'default' },
}

// ResolutionCard shows "why this project/repo" so the reviewer can trust or
// correct the M3 inference before approving.
function ResolutionCard({ resolution }: { resolution: Resolution }) {
  const method = RESOLUTION_METHOD_LABEL[resolution.method] ?? { text: resolution.method, color: 'default' }
  return <section>
    <Text type="secondary">项目 / 仓库推算</Text>
    <Descriptions column={2} size="small" style={{ marginTop: 4 }}>
      <Descriptions.Item label="判定方式"><Tag color={method.color}>{method.text}</Tag></Descriptions.Item>
      <Descriptions.Item label="项目">{resolution.project_name || (resolution.project_id ? `#${resolution.project_id}` : '未推算')}</Descriptions.Item>
      <Descriptions.Item label="仓库线索">{resolution.repos_hint || '无'}</Descriptions.Item>
      <Descriptions.Item label="置信度">{resolution.confidence != null ? `${(resolution.confidence * 100).toFixed(0)}%` : '—'}</Descriptions.Item>
      {resolution.basis && <Descriptions.Item label="依据" span={2}>{resolution.basis}</Descriptions.Item>}
    </Descriptions>
  </section>
}

// SnapshotPanel renders the M3-frozen background that M4/M5 replay unchanged.
// It is collapsed by default (secondary reference), with the raw JSON available.
function SnapshotPanel({ snapshot }: { snapshot: ContextSnapshot }) {
  return <Collapse size="small" items={[{
    key: 'snapshot',
    label: `背景快照（M3 固化，M4/M5 复用） · ${snapshot.messages?.length ?? 0} 条证据`,
    children: <Space direction="vertical" size={8} style={{ display: 'flex' }}>
      <Descriptions column={1} size="small">
        {snapshot.principal && <Descriptions.Item label="决策人">{snapshot.principal.name}{snapshot.principal.title ? `（${snapshot.principal.title}）` : ''}{snapshot.principal.leader_name ? ` · 上级 ${snapshot.principal.leader_name}` : ''}</Descriptions.Item>}
        {snapshot.project && <Descriptions.Item label="项目">{snapshot.project.name}{snapshot.project.description ? ` — ${snapshot.project.description}` : ''}</Descriptions.Item>}
        {snapshot.group && <Descriptions.Item label="会话">{snapshot.group.name || snapshot.group.chat_id}{snapshot.group.description ? `（公告：${snapshot.group.description}）` : ''}</Descriptions.Item>}
        {snapshot.assigner && <Descriptions.Item label="交办人">{snapshot.assigner.name || snapshot.assigner.open_id}{snapshot.assigner.relation ? ` · ${snapshot.assigner.relation}` : ''}</Descriptions.Item>}
        <Descriptions.Item label="记忆命中">{snapshot.memories?.length ?? 0} 条</Descriptions.Item>
      </Descriptions>
      {snapshot.supplements && snapshot.supplements.length > 0 && <div>
        <Text type="secondary">人工补充</Text>
        {snapshot.supplements.map((s, i) => <blockquote key={i}>{s.note}<br /><Text type="secondary" style={{ fontSize: 12 }}>{new Date(s.at).toLocaleString()}</Text></blockquote>)}
      </div>}
      <Text type="secondary">原始 JSON</Text>
      <pre className="snapshot-json">{JSON.stringify(snapshot, null, 2)}</pre>
    </Space>,
  }]} />
}

function semanticSummary(value: unknown): string {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    const summary = (value as Record<string, unknown>).summary
    if (typeof summary === 'string') return summary
  }
  return typeof value === 'string' ? value : ''
}

function SemanticValue({ value }: { value: unknown }) {
  if (typeof value === 'string') return <Paragraph style={{ marginBottom: 0 }}>{value}</Paragraph>
  return <pre className="snapshot-json">{JSON.stringify(value, null, 2)}</pre>
}

function isNonEmptyJSONValue(value: unknown): boolean {
  if (value === null || value === undefined) return false
  if (typeof value === 'string') return value.trim().length > 0
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === 'object') return Object.keys(value).length > 0
  return true
}

export default function Confirmations() {
  const [items, setItems] = useState<Todo[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshKey, setRefreshKey] = useState(0)
  const [detail, setDetail] = useState<ConfirmationDetail>()
  const [detailLoading, setDetailLoading] = useState(false)
  const [modal, setModal] = useState<'approve' | 'reject' | 'supplement'>()
  const [input, setInput] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    listConfirmations(1, 100, controller.signal)
      .then((result) => { setItems(result.items); setTotal(result.total); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [refreshKey])

  const openDetail = useCallback((todo: Todo) => {
    setDetailLoading(true)
    getConfirmation(todo.id)
      .then(setDetail)
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setDetailLoading(false))
  }, [])

  const openApprove = () => {
    if (!detail) return
    setInput(JSON.stringify(detail.plan ?? detail.todo.description, null, 2))
    setModal('approve')
  }

  const submit = async () => {
    if (!detail || !modal) return
    setSubmitting(true)
    try {
      if (modal === 'approve') {
        const plan: unknown = JSON.parse(input)
        if (!isNonEmptyJSONValue(plan)) throw new Error('方案必须是非空、非 null 的 JSON 值')
        await approveConfirmation(detail.todo.id, detail.todo.version, plan)
      } else if (modal === 'supplement') {
        if (!input.trim()) throw new Error('补充说明不能为空')
        await supplementConfirmation(detail.todo.id, detail.todo.version, input.trim())
      } else {
        if (!input.trim()) throw new Error('拒绝原因不能为空')
        await rejectConfirmation(detail.todo.id, detail.todo.version, input.trim())
      }
      setModal(undefined)
      setDetail(undefined)
      setRefreshKey((value) => value + 1)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }

  const columns: TableColumnsType<Todo> = [
    { title: '待确认事项', dataIndex: 'title', render: (_, todo) => <Space direction="vertical" size={2}><Text strong>{todo.title}</Text><Text type="secondary">{todo.description}</Text></Space> },
    { title: '类型', dataIndex: 'action_type', width: 150, render: (value: string) => <Tag>{value}</Tag> },
    { title: '来源', width: 220, render: (_, todo) => todo.group?.name || todo.group?.chat_id || '未知会话' },
  ]

  return <>
    <PageHeader title="待确认" subtitle={`人工确认队列 · 共 ${total} 条，点行查看并决策`}>
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </PageHeader>
    {error && <Alert type="error" showIcon message="操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Table<Todo> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} onRow={(todo) => ({ onClick: () => openDetail(todo), className: 'clickable-row' })} />
    </Card>
    <Drawer title={detail?.todo.title || '确认详情'} open={Boolean(detail) || detailLoading} loading={detailLoading} width={680} onClose={() => setDetail(undefined)}>
      {detail && (() => {
        const isNeedInfo = detail.todo.status === 'need_info'
        return <Space direction="vertical" size={20} className="drawer-content">
        <Space><StatusBadge label={todoStatusMeta[isNeedInfo ? 'need_info' : 'need_decision'].label} color={todoStatusMeta[isNeedInfo ? 'need_info' : 'need_decision'].color} /><Tag>{detail.todo.action_type}</Tag></Space>

        {/* 第一段：决策问题 —— 一句话说清现在要你决定什么 */}
        {isNeedInfo
          ? <Alert type="warning" showIcon message="需要你补充信息，codex 才能继续"
              description={<SemanticValue value={detail.decision_payload} />} />
          : <Alert type="info" showIcon message="codex 建议如下方案，请你决策：批准 / 补充 / 拒绝"
              description={semanticSummary(detail.plan) || detail.todo.description} />}

        {/* 第二段：完整展示 M4 的开放方案与判断语义，不在前端复制模型 DTO。 */}
        <section>
          <Text type="secondary">codex 判断</Text>
          {detail.plan != null && <div style={{ marginTop: 8 }}><Text strong>执行方案</Text><SemanticValue value={detail.plan} /></div>}
          {detail.decision_payload != null && <div style={{ marginTop: 8 }}><Text strong>判断、证据与风险</Text><SemanticValue value={detail.decision_payload} /></div>}
        </section>

        {/* 第三段：证据与背景（次要，可展开） */}
        {detail.todo.resolution && <ResolutionCard resolution={detail.todo.resolution} />}
        <section><Text type="secondary">背景与待补充</Text><TodoContextPanel target={detail.todo.target} context={detail.todo.context} openQuestions={detail.todo.open_questions} /></section>
        <section><Text type="secondary">证据消息</Text>{detail.source_messages.map((message) => <blockquote key={message.message_id}><Text strong>{message.sender_name || message.sender_open_id}</Text><br />{message.content}</blockquote>)}</section>
        {detail.todo.context_snapshot && <SnapshotPanel snapshot={detail.todo.context_snapshot} />}

        {/* 行动区 */}
        {!isNeedInfo
          ? <Flex gap={12} wrap>
              <Button type="primary" onClick={openApprove}>批准</Button>
              <Button onClick={() => { setInput(''); setModal('supplement') }}>补充信息 / 指示，重新决策</Button>
              <Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button>
            </Flex>
          : <Flex gap={12} wrap>
              <Button type="primary" onClick={() => { setInput(''); setModal('supplement') }}>补充信息</Button>
              <Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button>
            </Flex>}
      </Space>
      })()}
    </Drawer>
    <Modal title={modalTitle(modal)} open={Boolean(modal)} confirmLoading={submitting} onOk={submit} onCancel={() => setModal(undefined)} okText={modalOkText(modal)}>
      <Input.TextArea rows={modal === 'approve' ? 12 : 4} value={input} onChange={(event) => setInput(event.target.value)} placeholder={modalPlaceholder(modal)} />
    </Modal>
  </>
}
