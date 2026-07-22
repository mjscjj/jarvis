import { useEffect, useState } from 'react'
import { Alert, Card, Empty, Space, Spin, Typography } from 'antd'
import { listEntityRelations } from '../api'
import type { RelationEntityType, RelationFact } from '../types'

const { Text, Paragraph } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function EntityRelations({ entityType, entityId }: { entityType: RelationEntityType; entityId: number }) {
  const [items, setItems] = useState<RelationFact[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    listEntityRelations(entityType, entityId, controller.signal)
      .then((result) => setItems(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [entityType, entityId])

  return (
    <Card size="small" title="关联关系" variant="borderless">
      {error && <Alert type="error" showIcon title="关系加载失败" description={error} />}
      {loading ? (
        <div style={{ padding: 16, textAlign: 'center' }}><Spin size="small" /></div>
      ) : items.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无关联关系" />
      ) : (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          {items.map((fact) => (
            <div key={fact.id}>
              <Paragraph style={{ marginBottom: 2, whiteSpace: 'pre-wrap' }}>{fact.description}</Paragraph>
              <Text type="secondary" style={{ fontSize: 12 }}>{fact.entity_a.label} ↔ {fact.entity_b.label}</Text>
            </div>
          ))}
        </Space>
      )}
    </Card>
  )
}
