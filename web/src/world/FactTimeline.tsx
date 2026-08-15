import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Alert, Button, Card, Collapse, Empty, Flex, Space, Spin, Tag, Timeline, Typography } from 'antd'
import dayjs from 'dayjs'
import { getFactTimeline, listSubjectFacts } from '../api'
import type { Fact, FactSubjectDay, FactTimeline as FactTimelineData } from '../types'

const { Text } = Typography

function sourceLabel(source: string | null): string | null {
  if (!source) return null
  if (source === 'message') return '消息抽取'
  return source
}

function factContent(fact: Fact, showSubject = false, subjectLabel?: string) {
  return (
    <div className="fact-item">
      {showSubject && <Text strong className="fact-subject">{subjectLabel || `${fact.subject_type}/${fact.subject_id}`}</Text>}
      <div className="fact-description">{fact.description}</div>
      <Space size={6} wrap>
        <Text type="secondary" className="fact-time">{dayjs(fact.occurred_at).format('MM-DD HH:mm')}</Text>
        {sourceLabel(fact.source_kind) && <Tag>{sourceLabel(fact.source_kind)}</Tag>}
      </Space>
    </div>
  )
}

function subjectDayKey(date: string, subject: FactSubjectDay): string {
  return `${date}/${subject.subject_type}/${subject.subject_id}`
}

export default function FactTimeline({
  subject,
  title = '事实',
  extra,
  refreshToken = 0,
}: {
  subject?: { type: string; id: number }
  title?: string
  extra?: ReactNode
  refreshToken?: number
}) {
  const [timeline, setTimeline] = useState<FactTimelineData>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [revision, setRevision] = useState(0)
  const [details, setDetails] = useState<Record<string, Fact[]>>({})
  const [detailLoading, setDetailLoading] = useState<Record<string, boolean>>({})
  const timelineDays = subject ? 31 : 3

  const reload = useCallback(() => setRevision((value) => value + 1), [])

  useEffect(() => {
    const controller = new AbortController()
    const load = () => {
      setLoading(true)
      getFactTimeline(timelineDays, subject, controller.signal)
        .then((result) => { setTimeline(result); setError(undefined) })
        .catch((cause: unknown) => {
          if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(cause instanceof Error ? cause.message : String(cause))
        })
        .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    }
    load()
    const timer = window.setInterval(load, 30_000)
    return () => { controller.abort(); window.clearInterval(timer) }
  }, [refreshToken, revision, subject?.id, subject?.type])

  const loadDetails = async (date: string, item: FactSubjectDay) => {
    const key = subjectDayKey(date, item)
    if (details[key] || detailLoading[key]) return
    setDetailLoading((current) => ({ ...current, [key]: true }))
    try {
      const from = dayjs(`${date}T00:00:00`).toISOString()
      const until = dayjs(`${date}T00:00:00`).add(1, 'day').toISOString()
      const result = await listSubjectFacts(item.subject_type, item.subject_id, undefined, { from, until, excludeSourceKind: 'page_revision' })
      setDetails((current) => ({ ...current, [key]: result.items }))
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setDetailLoading((current) => ({ ...current, [key]: false }))
    }
  }

  const historyRows = timeline?.days.slice(1).flatMap((day) => day.subjects.map((item) => {
    const key = subjectDayKey(day.date, item)
    return (
      <div className="fact-history-row" key={key}>
        <Flex align="center" justify="space-between" gap={12} className="fact-history-label">
          <Space size={8} wrap>
            <Text strong>{dayjs(day.date).format('M月D日')}</Text>
            {!subject && <Text>{item.subject_label}</Text>}
            <Tag>{item.detail_count} 条事实</Tag>
            <Text type="secondary">最近 {dayjs(item.latest_occurred_at).format('HH:mm')}</Text>
          </Space>
        </Flex>
        <Collapse
          ghost
          className="fact-raw-collapse"
          items={[{
            key,
            label: <Text type="secondary">展开 {item.detail_count} 条事实</Text>,
            children: detailLoading[key] ? <Spin size="small" /> : (
              <Timeline items={(details[key] || []).map((fact) => ({ content: factContent(fact) }))} />
            ),
          }]}
          onChange={(keys) => {
            const active = Array.isArray(keys) ? keys : [keys]
            if (active.length > 0) void loadDetails(day.date, item)
          }}
        />
      </div>
    )
  })) || []

  return (
    <Card
      size="small"
      title={title}
      variant="borderless"
      className="fact-timeline-card"
      extra={<Space size={8}>{extra}<Button size="small" onClick={reload} loading={loading}>刷新</Button></Space>}
    >
      {error && <Alert type="error" showIcon title="事实加载失败" description={error} closable onClose={() => setError(undefined)} className="fact-alert" />}
      {loading && !timeline ? <div className="fact-loading"><Spin size="small" /></div> : (
        <Space orientation="vertical" size={18} style={{ width: '100%' }}>
          <section>
            <Flex align="baseline" gap={8} className="fact-day-heading">
              <Text strong>今天</Text>
              <Text type="secondary">实时明细 · {timeline?.days[0]?.detail_count || 0} 条</Text>
            </Flex>
            {(timeline?.days[0]?.details.length || 0) === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="今天暂无事实" />
            ) : (
              <Timeline items={timeline?.days[0]?.details.map((fact) => ({ content: factContent(fact, !subject, fact.subject_label) }))} />
            )}
          </section>
          <section>
            <Flex align="baseline" gap={8} className="fact-day-heading">
              <Text strong>历史</Text>
              <Text type="secondary">按主体分组，展开查看当天事实</Text>
            </Flex>
            {historyRows.length === 0 ? (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={subject ? '最近 30 天暂无事实' : '昨天、前天暂无事实'} />
            ) : (
              <Space orientation="vertical" size={10} style={{ width: '100%' }}>{historyRows}</Space>
            )}
          </section>
        </Space>
      )}
    </Card>
  )
}
