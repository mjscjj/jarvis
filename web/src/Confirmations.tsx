import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, Collapse, Descriptions, Input, Modal, Space, Spin, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { CloseOutlined } from '@ant-design/icons'
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
  if (value === null || value === undefined) return <Text type="secondary">未提供</Text>
  if (typeof value === 'string') return <Paragraph className="confirmation-readable-text">{value}</Paragraph>
  if (typeof value !== 'object') return <Text>{String(value)}</Text>
  if (Array.isArray(value)) {
    return <ol className="confirmation-semantic-list">
      {value.map((item, index) => <li key={index}><SemanticValue value={item} /></li>)}
    </ol>
  }

  const entries = Object.entries(value as Record<string, unknown>)
  const summary = entries.find(([key, item]) => key === 'summary' && typeof item === 'string')
  const heading = entries.find(([key, item]) => (key === 'label' || key === 'goal') && typeof item === 'string')
  const rest = entries.filter(([key]) => key !== 'summary' && key !== 'kind' && key !== heading?.[0])
  return <div className="confirmation-semantic-object">
    {heading && <div className="confirmation-semantic-heading">{heading[1] as string}</div>}
    {summary && <Paragraph className="confirmation-semantic-summary">{summary[1] as string}</Paragraph>}
    {rest.map(([key, item]) => <section key={key} className="confirmation-semantic-field">
      <Text strong>{semanticKeyLabel(key)}</Text>
      <SemanticValue value={item} />
    </section>)}
  </div>
}

const SEMANTIC_KEY_LABELS: Record<string, string> = {
  steps: '执行步骤',
  actions: '具体动作',
  goal: '目标',
  blocks: '判断内容',
  label: '主题',
  content: '内容',
  evidence: '证据',
  risks: '风险',
  risk: '风险',
  review_points: '需要确认',
}

function semanticKeyLabel(key: string): string {
  return SEMANTIC_KEY_LABELS[key] || key.replaceAll('_', ' ')
}

function isNonEmptyJSONValue(value: unknown): boolean {
  if (value === null || value === undefined) return false
  if (typeof value === 'string') return value.trim().length > 0
  if (Array.isArray(value)) return value.length > 0
  if (typeof value === 'object') return Object.keys(value).length > 0
  return true
}

export default function Confirmations({ onDetailOpen }: { onDetailOpen?: () => void }) {
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
  const detailRequestRef = useRef<AbortController | undefined>(undefined)

  useEffect(() => () => detailRequestRef.current?.abort(), [])

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

  const openDetail = (todo: Todo) => {
    onDetailOpen?.()
    detailRequestRef.current?.abort()
    const controller = new AbortController()
    detailRequestRef.current = controller
    setDetail(undefined)
    setDetailLoading(true)
    getConfirmation(todo.id, controller.signal)
      .then(setDetail)
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (detailRequestRef.current === controller) setDetailLoading(false)
      })
  }

  const closeDetail = () => {
    detailRequestRef.current?.abort()
    detailRequestRef.current = undefined
    setDetail(undefined)
    setDetailLoading(false)
  }

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
    <Modal
      open={Boolean(detail) || detailLoading}
      footer={null}
      closable={false}
      centered
      width={1120}
      mask={{ closable: true }}
      onCancel={closeDetail}
      className="confirmation-detail-modal"
      destroyOnHidden
    >
      <div className="confirmation-detail-shell">
      {detailLoading && !detail
        ? <div className="confirmation-detail-loading"><Spin size="large" /></div>
        : detail && (() => {
        const isNeedInfo = detail.todo.status === 'need_info'
        const decisionSummary = semanticSummary(detail.decision_payload)
          || semanticSummary(detail.plan)
          || detail.todo.description
        return <>
          <header className="confirmation-detail-header">
            <div className="confirmation-detail-title">
              <Space size={8} wrap>
                <StatusBadge label={todoStatusMeta[isNeedInfo ? 'need_info' : 'need_decision'].label} color={todoStatusMeta[isNeedInfo ? 'need_info' : 'need_decision'].color} />
                <Tag>{detail.todo.action_type}</Tag>
                <Text type="secondary">Todo #{detail.todo.id}</Text>
              </Space>
              <Typography.Title level={3}>{detail.todo.title}</Typography.Title>
              <Text type="secondary">{detail.todo.project?.name || '未关联项目'} · {detail.todo.group?.name || detail.todo.group?.chat_id || '未知来源'}</Text>
            </div>
            <Button type="text" icon={<CloseOutlined />} aria-label="关闭" onClick={closeDetail} />
          </header>

          <div className="confirmation-detail-scroll">
            <Alert
              className="confirmation-decision-alert"
              type={isNeedInfo ? 'warning' : 'info'}
              showIcon
              message={isNeedInfo ? '需要你补充信息，Codex 才能继续' : '现在需要你拍板'}
              description={decisionSummary}
            />

            <Tabs
              defaultActiveKey="decision"
              items={[
                {
                  key: 'decision',
                  label: '方案与判断',
                  children: <div className="confirmation-decision-grid">
                    <section className="confirmation-section-card">
                      <div className="confirmation-section-title">拟执行方案</div>
                      {detail.plan != null
                        ? <SemanticValue value={detail.plan} />
                        : <Text type="secondary">Codex 尚未形成可执行方案。</Text>}
                    </section>
                    <section className="confirmation-section-card">
                      <div className="confirmation-section-title">为什么需要确认</div>
                      {detail.decision_payload != null
                        ? <SemanticValue value={detail.decision_payload} />
                        : <Text type="secondary">没有额外判断说明。</Text>}
                    </section>
                  </div>,
                },
                {
                  key: 'context',
                  label: '背景与证据',
                  children: <div className="confirmation-context-stack">
                    {detail.todo.resolution && <section className="confirmation-section-card"><ResolutionCard resolution={detail.todo.resolution} /></section>}
                    <section className="confirmation-section-card">
                      <div className="confirmation-section-title">任务背景与待补充</div>
                      <TodoContextPanel target={detail.todo.target} context={detail.todo.context} openQuestions={detail.todo.open_questions} />
                    </section>
                    <section className="confirmation-section-card">
                      <div className="confirmation-section-title">证据消息</div>
                      {detail.source_messages.map((message) => <blockquote key={message.message_id}><Text strong>{message.sender_name || message.sender_open_id}</Text><br />{message.content}</blockquote>)}
                    </section>
                    {detail.todo.context_snapshot && <SnapshotPanel snapshot={detail.todo.context_snapshot} />}
                  </div>,
                },
                {
                  key: 'raw',
                  label: '原始数据',
                  children: <div className="confirmation-raw-grid">
                    <section><Text strong>方案 JSON</Text><pre className="confirmation-raw-json">{JSON.stringify(detail.plan, null, 2)}</pre></section>
                    <section><Text strong>判断 JSON</Text><pre className="confirmation-raw-json">{JSON.stringify(detail.decision_payload, null, 2)}</pre></section>
                  </div>,
                },
              ]}
            />
          </div>

          <footer className="confirmation-detail-footer">
            <Text type="secondary">
              {isNeedInfo ? '补充后会回到 Codex 重新判定，不会直接执行。' : '批准后会固化为 Task，再进入执行链路。'}
            </Text>
            <Space wrap>
              {!isNeedInfo && <Button type="primary" onClick={openApprove}>批准方案</Button>}
              <Button onClick={() => { setInput(''); setModal('supplement') }}>{isNeedInfo ? '补充信息' : '补充 / 修改后重判'}</Button>
              <Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button>
            </Space>
          </footer>
        </>
      })()}
      </div>
    </Modal>
    <Modal zIndex={1100} title={modalTitle(modal)} open={Boolean(modal)} confirmLoading={submitting} onOk={submit} onCancel={() => setModal(undefined)} okText={modalOkText(modal)}>
      <Input.TextArea rows={modal === 'approve' ? 12 : 4} value={input} onChange={(event) => setInput(event.target.value)} placeholder={modalPlaceholder(modal)} />
    </Modal>
  </>
}
