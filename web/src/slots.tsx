import { Empty, Space, Typography } from 'antd'

const { Paragraph, Text } = Typography

function object(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : {}
}

// Both clue and task details display frozen facts directly, separate from model notes.
export function FrozenContextPanel({ content }: { content: unknown }) {
  const packet = object(content)
  const capture = object(packet.capture)
  const annotation = object(packet.annotation)
  const source = object(packet.source)
  const ids = new Set(Array.isArray(source.source_message_ids) ? source.source_message_ids : [])
  const messages = Array.isArray(capture.messages) ? capture.messages : []
  if (!Object.keys(packet).length) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无背景信息" />
  const renderBody = (body: unknown) => {
    const record = object(body)
    return typeof body === 'string' || typeof record.content === 'string'
      ? <><Text type="secondary">{typeof record.sender_name === 'string' ? record.sender_name : ''}</Text><Paragraph style={{ whiteSpace: 'pre-wrap' }}>{typeof body === 'string' ? body : String(record.content)}</Paragraph></>
      : <pre style={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{JSON.stringify(body, null, 2)}</pre>
  }
  const labels: Record<string, string> = { group: '来源会话', project: '项目', principal: '委托人', participants: '参与者', resources: '相关资料', assigner: '交办人', other_projects: '项目目录' }
  return <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
    {typeof annotation.brief === 'string' && <Paragraph>{annotation.brief}</Paragraph>}
    {annotation.scene != null && <section><Text strong>当时现场</Text>{renderBody(annotation.scene)}</section>}
    {messages.map((item, index) => {
      const record = object(item)
      const primary = ids.has(record.message_id)
      return <details key={String(record.message_id ?? index)} open={primary}>
        <summary>{primary ? '直接证据' : '周边消息'} · {String(record.sender_name || record.message_id || index + 1)}</summary>
        {renderBody(item)}
      </details>
    })}
    {Object.entries(capture).filter(([key, body]) => key !== 'messages' && body != null).map(([key, body]) =>
      <details key={key}><summary>{labels[key] || key}</summary>{renderBody(body)}</details>)}
    <details><summary>来源原文与判断</summary>{renderBody(packet.source)}</details>
    <details><summary>补充说明</summary>{renderBody(annotation)}</details>
  </Space>
}
