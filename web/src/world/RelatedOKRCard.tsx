import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Flex, Spin, Tag, Typography } from 'antd'
import { listRelations } from '../api'
import { getGenericOKRBoard } from '../okr/emily/api'
import type { EntityRelation } from '../types'
import { usePageContext } from '../pageContext'
import { relatedOKRsForWorldEntity, type WorldOKREntityType } from './relatedOKR'

const { Paragraph, Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function RelatedOKRCard({
  type,
  id,
}: {
  type: WorldOKREntityType
  id: number | string
}) {
  const { navigate } = usePageContext()
  const [quarter, setQuarter] = useState('')
  const [objectives, setObjectives] = useState<Awaited<ReturnType<typeof getGenericOKRBoard>>['objectives']>([])
  const [relations, setRelations] = useState<EntityRelation[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    Promise.all([
      getGenericOKRBoard('', controller.signal),
      listRelations({ nodeType: type, nodeId: String(id) }, controller.signal),
    ])
      .then(([board, neighbors]) => {
        setQuarter(board.quarter)
        setObjectives(board.objectives)
        setRelations(neighbors.items)
      })
      .catch((cause: unknown) => {
        if (!controller.signal.aborted) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [id, type])

  const rows = useMemo(() => relatedOKRsForWorldEntity(
    relations, objectives, { type, id },
  ), [id, objectives, relations, type])

  return (
    <Card
      size="small"
      title={<Flex align="center" gap={8}><span>关联 OKR</span>{quarter && <Tag color="blue">{quarter}</Tag>}<Tag>{rows.length}</Tag></Flex>}
      extra={<Button type="link" size="small" onClick={() => navigate('plugins', { plugin: 'okr', plugin_tab: 'relations', quarter })}>查看 OKR 投影</Button>}
    >
      {loading ? <div style={{ padding: 20, textAlign: 'center' }}><Spin /></div> : error ? (
        <Alert type="error" showIcon title="关联 OKR 读取失败" description={error} />
      ) : rows.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当前季度还没有已确认的 OKR 映射或负责人关系" />
      ) : (
        <Flex vertical gap={12}>
          {rows.map((row) => (
            <div key={row.key} className="world-related-okr-row">
              <Flex align="center" gap={6} wrap>
                <Tag color={row.level === 'O' ? 'purple' : row.level === 'KR' ? 'blue' : 'cyan'}>{row.level}</Tag>
                <Tag color={row.confirmed ? 'green' : 'default'}>{row.relation}</Tag>
                <Text strong>{row.title}</Text>
              </Flex>
              {row.context && <Text type="secondary" className="world-related-okr-context">{row.context}</Text>}
              {row.basis && <Paragraph className="world-related-okr-basis">{row.basis}</Paragraph>}
              <Text type="secondary" copyable={{ text: `${row.type}:${row.id}` }}>{row.type}:{row.id}</Text>
            </div>
          ))}
        </Flex>
      )}
    </Card>
  )
}
