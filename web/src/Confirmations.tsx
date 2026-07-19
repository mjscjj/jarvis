import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Descriptions, Drawer, Flex, Input, Modal, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { approveConfirmation, getConfirmation, listConfirmations, rejectConfirmation } from './api'
import type { ConfirmationDetail, Todo } from './types'

const { Paragraph, Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function Confirmations() {
  const [items, setItems] = useState<Todo[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [refreshKey, setRefreshKey] = useState(0)
  const [detail, setDetail] = useState<ConfirmationDetail>()
  const [detailLoading, setDetailLoading] = useState(false)
  const [modal, setModal] = useState<'approve' | 'reject'>()
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
    { title: '操作', width: 100, render: (_, todo) => <Button type="link" onClick={() => openDetail(todo)}>查看</Button> },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <div><Text strong>人工确认队列</Text><Text type="secondary"> · {total} 条</Text></div>
      <Button onClick={() => setRefreshKey((value) => value + 1)} loading={loading}>刷新</Button>
    </Flex>
    {error && <Alert type="error" showIcon message="操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless">
      <Table<Todo> rowKey="id" columns={columns} dataSource={items} loading={loading} pagination={false} />
    </Card>
    <Drawer title={detail?.todo.title || '确认详情'} open={Boolean(detail) || detailLoading} loading={detailLoading} width={680} onClose={() => setDetail(undefined)}>
      {detail && <Space direction="vertical" size={20} className="drawer-content">
        <Space><Tag color="gold">待决策</Tag><Tag>{detail.todo.action_type}</Tag></Space>
        <Paragraph>{detail.todo.description}</Paragraph>
        <Descriptions column={2} size="small">
          <Descriptions.Item label="交办人">{detail.assigner?.name || detail.todo.assigner_open_id || '未知'}</Descriptions.Item>
          <Descriptions.Item label="版本">v{detail.todo.version}</Descriptions.Item>
          <Descriptions.Item label="项目">{detail.todo.project?.name || '未关联'}</Descriptions.Item>
          <Descriptions.Item label="会话">{detail.todo.group?.name || detail.todo.group?.chat_id || '未知'}</Descriptions.Item>
        </Descriptions>
        {(detail.todo.confidence != null || detail.todo.risk != null || detail.proposed_plan) && <section>
          <Text type="secondary">codex 判断</Text>
          <Space size={12} style={{ display: 'flex', marginTop: 4 }}>
            {detail.todo.confidence != null && <Tag color="blue">信心 {(detail.todo.confidence * 100).toFixed(0)}%</Tag>}
            {detail.todo.risk != null && <Tag color={detail.todo.risk >= 0.6 ? 'red' : 'orange'}>风险 {(detail.todo.risk * 100).toFixed(0)}%</Tag>}
          </Space>
          {detail.proposed_plan && <>
            <Paragraph style={{ marginTop: 8, marginBottom: 4 }}><Text strong>建议方案：</Text>{detail.proposed_plan.summary}</Paragraph>
            {detail.proposed_plan.basis?.length > 0 && <Paragraph type="secondary" style={{ marginBottom: 0 }}>理由：{detail.proposed_plan.basis.join('；')}</Paragraph>}
          </>}
        </section>}
        <section><Text type="secondary">结构化参数</Text><pre>{JSON.stringify(detail.todo.slots, null, 2)}</pre></section>
        <section><Text type="secondary">证据消息</Text>{detail.source_messages.map((message) => <blockquote key={message.message_id}><Text strong>{message.sender_name || message.sender_open_id}</Text><br />{message.content}</blockquote>)}</section>
        {detail.todo.status === 'need_decision'
          ? <Flex gap={12}><Button type="primary" onClick={openApprove}>批准并生成 Task</Button><Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button></Flex>
          : <><Alert type="warning" showIcon message="该 Todo 需要补充信息；MVP 暂不提供补信息操作，可先拒绝后由新消息重新提取。" /><Button danger onClick={() => { setInput(''); setModal('reject') }}>拒绝</Button></>}
      </Space>}
    </Drawer>
    <Modal title={modal === 'approve' ? '确认执行方案' : '填写拒绝原因'} open={Boolean(modal)} confirmLoading={submitting} onOk={submit} onCancel={() => setModal(undefined)} okText={modal === 'approve' ? '批准' : '确认拒绝'}>
      <Input.TextArea rows={modal === 'approve' ? 12 : 4} value={input} onChange={(event) => setInput(event.target.value)} placeholder={modal === 'approve' ? '非空 JSON 方案' : '拒绝原因'} />
    </Modal>
  </>
}
