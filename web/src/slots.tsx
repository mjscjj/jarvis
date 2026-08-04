import { Alert, Empty, Space, Typography } from 'antd'

const { Paragraph, Text } = Typography

// TodoContextPanel renders M3's general clue fields: the dedup target, the
// assistant-gathered context, and uncertainties M5 should handle while running
// the Task. It replaces the old per-action_type slot table.
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
    <Space orientation="vertical" size="small" style={{ width: '100%' }}>
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
          title="执行时待查证 / 补全"
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
