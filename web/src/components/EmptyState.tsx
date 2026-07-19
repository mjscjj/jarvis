import { Empty, Typography } from 'antd'

const { Text } = Typography

interface EmptyStateProps {
  description?: string
  hint?: string
}

export default function EmptyState({ description = '暂无数据', hint }: EmptyStateProps) {
  return (
    <Empty
      image={Empty.PRESENTED_IMAGE_SIMPLE}
      description={
        <div>
          <Text type="secondary">{description}</Text>
          {hint && <div><Text type="secondary" style={{ fontSize: 12 }}>{hint}</Text></div>}
        </div>
      }
    />
  )
}
