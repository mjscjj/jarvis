import { Alert, Empty, Space, Typography } from 'antd'

const { Paragraph, Text } = Typography

// TodoContextPanel renders M3's general clue fields: the dedup target, the
// assistant-gathered context, and any open questions that still need the
// principal. It replaces the old per-action_type slot table.
export function TodoContextPanel({
  target,
  context,
  openQuestions,
}: {
  target?: string | null
  context?: string | null
  openQuestions?: string[] | null
}) {
  const hasContext = !!(context && context.trim())
  const questions = (openQuestions ?? []).filter((q) => q && q.trim())
  if (!target && !hasContext && questions.length === 0) {
    return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无背景信息" />
  }
  return (
    <Space direction="vertical" size="small" style={{ width: '100%' }}>
      {target && (
        <div>
          <Text type="secondary">主题 / 去重身份</Text>
          <Paragraph style={{ marginBottom: 0 }}>{target}</Paragraph>
        </div>
      )}
      {hasContext && (
        <div>
          <Text type="secondary">已补全的背景</Text>
          <Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>{context}</Paragraph>
        </div>
      )}
      {questions.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message="仍需你拍板 / 补充"
          description={
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {questions.map((q, i) => (
                <li key={i}>{q}</li>
              ))}
            </ul>
          }
        />
      )}
    </Space>
  )
}
