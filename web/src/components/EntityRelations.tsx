import { useEffect, useState } from 'react'
import { Alert, Card, Empty, Space, Spin, Tag, Typography } from 'antd'
import { listEntityRelations } from '../api'
import type { RelationEntityType, RelationFact } from '../types'

const { Text, Paragraph } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function day(value: string): string {
  return value.slice(0, 10)
}

// 两端各自可空：只有起点表示仍然有效，只有终点表示起点未知。
function periodText(fact: RelationFact): string | null {
  if (fact.valid_from && fact.valid_until) return `${day(fact.valid_from)} ~ ${day(fact.valid_until)}`
  if (fact.valid_from) return `${day(fact.valid_from)} 起`
  if (fact.valid_until) return `截至 ${day(fact.valid_until)}`
  return null
}

function hasEnded(fact: RelationFact): boolean {
  return fact.valid_until !== null && Date.parse(fact.valid_until) <= Date.now()
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
          {items.map((fact) => {
            const period = periodText(fact)
            return (
              <div key={fact.id}>
                <Paragraph style={{ marginBottom: 2, whiteSpace: 'pre-wrap' }}>{fact.description}</Paragraph>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {fact.entity_a.label} ↔ {fact.entity_b.label}
                  {period && ` · ${period}`}
                </Text>
                {hasEnded(fact) && <Tag style={{ marginInlineStart: 6 }}>已结束</Tag>}
              </div>
            )
          })}
        </Space>
      )}
    </Card>
  )
}
