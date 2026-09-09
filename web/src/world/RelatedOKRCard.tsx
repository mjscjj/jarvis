import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Flex, Spin, Tag, Typography } from 'antd'
import { listAppModules, listRelations } from '../api'
import { getGenericOKRBoard } from '../okr/emily/api'
import type { EntityRelation } from '../types'
import { usePageContext } from '../pageContext'
import { relatedOKRsForWorldEntity, type WorldOKREntityType } from './relatedOKR'
import { isOKRPluginEnabled } from '../world-map/lens'

const { Paragraph, Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function RelatedOKRCard({
  type,
  id,
  compact = false,
}: {
  type: WorldOKREntityType
  id: number | string
  compact?: boolean
}) {
  const { navigate } = usePageContext()
  const [quarter, setQuarter] = useState('')
  const [objectives, setObjectives] = useState<Awaited<ReturnType<typeof getGenericOKRBoard>>['objectives']>([])
  const [relations, setRelations] = useState<EntityRelation[]>([])
  const [loading, setLoading] = useState(true)
  const [available, setAvailable] = useState<boolean>()
  const [error, setError] = useState<string>()

  useEffect(() => {
    let controller = new AbortController()
    const load = async () => {
      controller.abort()
      controller = new AbortController()
      setLoading(true)
      setAvailable(undefined)
      setError(undefined)
      try {
        const modules = await listAppModules(controller.signal)
        if (!isOKRPluginEnabled(modules.items)) {
          setAvailable(false)
          return
        }
        setAvailable(true)
        const [board, neighbors] = await Promise.all([
          getGenericOKRBoard('', controller.signal),
          listRelations({ nodeType: type, nodeId: String(id) }, controller.signal),
        ])
        if (controller.signal.aborted) return
        setQuarter(board.quarter)
        setObjectives(board.objectives)
        setRelations(neighbors.items)
      } catch (cause: unknown) {
        if (!controller.signal.aborted) setError(errorText(cause))
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void load()
    window.addEventListener('jarvis:app-modules-changed', load)
    return () => {
      controller.abort()
      window.removeEventListener('jarvis:app-modules-changed', load)
    }
  }, [id, type])

  const rows = useMemo(() => relatedOKRsForWorldEntity(
    relations, objectives, { type, id },
  ), [id, objectives, relations, type])

  if (available === false) return null

  const content = loading ? <div style={{ padding: 20, textAlign: 'center' }}><Spin /></div> : error ? (
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
      )

  if (compact) {
    const compactContent = loading ? <div className="world-related-okr-compact-loading"><Spin size="small" /></div> : error ? (
      <Text type="danger">关联 OKR 读取失败</Text>
    ) : rows.length === 0 ? (
      <Text type="secondary">暂无已确认的季度目标映射</Text>
    ) : (
      <div className="world-related-okr-compact-list">
        {rows.slice(0, 3).map((row) => (
          <div key={row.key} className="world-related-okr-compact-row">
            <Tag color={row.level === 'O' ? 'purple' : row.level === 'KR' ? 'blue' : 'cyan'}>{row.level}</Tag>
            <div><Text strong>{row.title}</Text>{row.context && <Text type="secondary">{row.context}</Text>}</div>
          </div>
        ))}
      </div>
    )
    return (
      <div className="world-related-okr-compact">
        <Flex justify="space-between" align="center" gap={8} className="world-related-okr-compact-head">
          <Flex align="center" gap={6}>{quarter && <Tag color="blue">{quarter}</Tag>}<Tag>{rows.length} 项</Tag></Flex>
          <Button type="link" size="small" onClick={() => navigate('plugins', { plugin: 'okr', plugin_tab: 'relations', quarter })}>查看 OKR</Button>
        </Flex>
        {compactContent}
      </div>
    )
  }

  return (
    <Card
      size="small"
      title={<Flex align="center" gap={8}><span>关联 OKR</span>{quarter && <Tag color="blue">{quarter}</Tag>}<Tag>{rows.length}</Tag></Flex>}
      extra={<Button type="link" size="small" onClick={() => navigate('plugins', { plugin: 'okr', plugin_tab: 'relations', quarter })}>查看 OKR 投影</Button>}
    >
      {content}
    </Card>
  )
}
