import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Collapse, Descriptions, Drawer, Flex, Input, Modal, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { approveConfirmation, getConfirmation, listConfirmations, rejectConfirmation, supplementConfirmation } from './api'
import type { Clarification, ConfirmationDetail, ContextSnapshot, DecisionAuditView, DecisionFactor, Resolution, Todo } from './types'
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
  if (modal === 'supplement') return '补充信息'
  return '填写拒绝原因'
}
function modalOkText(modal: ConfirmModal): string {
  if (modal === 'approve') return '批准'
  if (modal === 'supplement') return '提交并重新判定'
  return '确认拒绝'
}
function modalPlaceholder(modal: ConfirmModal): string {
  if (modal === 'approve') return '非空 JSON 方案'
  if (modal === 'supplement') return '补充缺失的信息（如具体仓库、目标、范围、截止时间等），供决策器重新判定'
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

// latestCodexAudit picks the newest codex decision audit (with factor detail)
// so the workbench can show why codex scored the way it did.
function latestCodexAudit(detail: ConfirmationDetail): DecisionAuditView | undefined {
  const audits = (detail.audits || []).filter((a) => a.decision_engine === 'codex')
  return audits.length ? audits[audits.length - 1] : undefined
}

// FactorList renders codex's confidence/risk factors so the reviewer sees where
// codex is sure and where it is shaky — the basis for a human decision.
function FactorList({ title, factors, tone }: { title: string; factors: DecisionFactor[]; tone: 'confidence' | 'risk' }) {
  if (!factors.length) return null
  return <div style={{ flex: 1, minWidth: 240 }}>
    <Text type="secondary">{title}</Text>
    <ul style={{ margin: '4px 0 0', paddingLeft: 18 }}>
      {factors.map((f, i) => <li key={i} style={{ marginBottom: 4 }}>
        <Tag color={tone === 'risk' ? (f.score >= 0.6 ? 'red' : 'orange') : (f.score >= 0.6 ? 'green' : 'default')}>{(f.score * 100).toFixed(0)}%</Tag>
        <Text strong>{f.name}</Text><br /><Text type="secondary" style={{ fontSize: 12 }}>{f.basis}</Text>
      </li>)}
    </ul>
  </div>
}

// ClarificationList shows what codex needs the human to clarify/supply. It is the
// heart of "what do I need to do" for both need_info and need_review.
function ClarificationList({ clarifications, ordinal }: { clarifications: Clarification[]; ordinal: boolean }) {
  return <ol style={{ margin: '6px 0 0', paddingLeft: 20, listStyleType: ordinal ? 'decimal' : 'disc' }}>
    {clarifications.map((item, index) => <li key={index} style={{ marginBottom: 8 }}>
      <Text strong>{item.question}</Text>
      {item.hint && <><br /><Text type="secondary" style={{ fontSize: 12 }}>提示：{item.hint}</Text></>}
    </li>)}
  </ol>
}

// buildSupplementTemplate pre-fills the supplement box with codex's questions so
// the user answers each one instead of facing a blank textarea.
function buildSupplementTemplate(clarifications: Clarification[] | null): string {
  if (!clarifications || clarifications.length === 0) return ''
  return clarifications.map((c, i) => `${i + 1}. ${c.question}\n答：`).join('\n\n')
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
    setInput(JSON.stringify(detail.proposed_plan || { summary: detail.todo.description, steps: [detail.todo.title] }, null, 2))
    setModal('approve')
  }

  const submit = async () => {
    if (!detail || !modal) return
    setSubmitting(true)
    try {
      if (modal === 'approve') {
        const plan = JSON.parse(input) as Record<string, unknown>
        if (!plan || Array.isArray(plan) || Object.keys(plan).length === 0) throw new Error('方案必须是非空 JSON 对象')
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
        const audit = latestCodexAudit(detail)
        const clarifications = detail.clarifications || []
        return <Space direction="vertical" size={20} className="drawer-content">
        <Space><StatusBadge label={todoStatusMeta[isNeedInfo ? 'need_info' : 'need_decision'].label} color={todoStatusMeta[isNeedInfo ? 'need_info' : 'need_decision'].color} /><Tag>{detail.todo.action_type}</Tag></Space>

        {/* 第一段：决策问题 —— 一句话说清现在要你决定什么 */}
        {isNeedInfo
          ? <Alert type="warning" showIcon message="需要你补充信息，codex 才能继续"
              description={clarifications.length > 0
                ? <><Paragraph type="secondary" style={{ marginBottom: 4 }}>codex 需要你澄清以下几点：</Paragraph><ClarificationList clarifications={clarifications} ordinal /></>
                : '该 Todo 信息不足，但 codex 未给出具体澄清项（重跑一次 M4 可补全）。'} />
          : <Alert type="info" showIcon message="codex 建议如下方案，请你决策：批准执行 / 改方案 / 拒绝"
              description={detail.proposed_plan?.summary || detail.todo.description} />}

        {/* 第二段：codex 的想法 —— 它要干什么、有多大把握、担心什么 */}
        <section>
          <Text type="secondary">codex 判断</Text>
          <Space size={12} style={{ display: 'flex', marginTop: 4 }}>
            {detail.todo.confidence != null && <Tag color="blue">信心 {(detail.todo.confidence * 100).toFixed(0)}%</Tag>}
            {detail.todo.risk != null && <Tag color={detail.todo.risk >= 0.6 ? 'red' : 'orange'}>风险 {(detail.todo.risk * 100).toFixed(0)}%</Tag>}
          </Space>
          {detail.proposed_plan && <div style={{ marginTop: 8 }}>
            <Paragraph style={{ marginBottom: 4 }}><Text strong>它想做：</Text>{detail.proposed_plan.summary}</Paragraph>
            {detail.proposed_plan.steps?.length > 0 && <><Text type="secondary">执行步骤</Text>
              <ol style={{ margin: '4px 0 8px', paddingLeft: 20 }}>{detail.proposed_plan.steps.map((s, i) => <li key={i}>{s}</li>)}</ol></>}
            {detail.proposed_plan.parameters?.length > 0 && <Descriptions column={1} size="small" bordered style={{ marginBottom: 8 }}>
              {detail.proposed_plan.parameters.map((p, i) => <Descriptions.Item key={i} label={p.name}>{p.value}</Descriptions.Item>)}
            </Descriptions>}
            {detail.proposed_plan.basis?.length > 0 && <Paragraph type="secondary" style={{ marginBottom: 0 }}>依据：{detail.proposed_plan.basis.join('；')}</Paragraph>}
          </div>}
          {audit && (audit.confidence_factors?.length || audit.risk_factors?.length) ? <Collapse size="small" style={{ marginTop: 8 }} items={[{
            key: 'factors', label: '信心 / 风险因子明细',
            children: <Flex gap={16} wrap="wrap">
              <FactorList title="信心因子" factors={audit.confidence_factors || []} tone="confidence" />
              <FactorList title="风险因子" factors={audit.risk_factors || []} tone="risk" />
            </Flex>,
          }]} /> : null}
          {/* need_review 也把待澄清点显示出来，供决策参考 */}
          {!isNeedInfo && clarifications.length > 0 && <div style={{ marginTop: 8 }}>
            <Text type="secondary">codex 提出的待澄清点（供你决策参考）</Text>
            <ClarificationList clarifications={clarifications} ordinal={false} />
          </div>}
        </section>

        {/* 第三段：证据与背景（次要，可展开） */}
        {detail.todo.resolution && <ResolutionCard resolution={detail.todo.resolution} />}
        <section><Text type="secondary">背景与待补充</Text><TodoContextPanel target={detail.todo.target} context={detail.todo.context} openQuestions={detail.todo.open_questions} /></section>
        <section><Text type="secondary">证据消息</Text>{detail.source_messages.map((message) => <blockquote key={message.message_id}><Text strong>{message.sender_name || message.sender_open_id}</Text><br />{message.content}</blockquote>)}</section>
        {detail.todo.context_snapshot && <SnapshotPanel snapshot={detail.todo.context_snapshot} />}

        {/* 行动区 */}
        {!isNeedInfo
          ? <Flex gap={12}><Button type="primary" onClick={openApprove}>批准执行</Button><Button onClick={openApprove}>改方案再执行</Button><Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button></Flex>
          : <Flex gap={12}><Button type="primary" onClick={() => { setInput(buildSupplementTemplate(detail.clarifications)); setModal('supplement') }}>补充信息</Button><Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button></Flex>}
      </Space>
      })()}
    </Drawer>
    <Modal title={modalTitle(modal)} open={Boolean(modal)} confirmLoading={submitting} onOk={submit} onCancel={() => setModal(undefined)} okText={modalOkText(modal)}>
      <Input.TextArea rows={modal === 'approve' ? 12 : 4} value={input} onChange={(event) => setInput(event.target.value)} placeholder={modalPlaceholder(modal)} />
    </Modal>
  </>
}
