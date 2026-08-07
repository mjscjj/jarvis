import { useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Descriptions, Empty, Space, Spin, Tabs, Tag, Typography } from 'antd'
import dayjs from 'dayjs'
import { getMeetingReviews } from '../api'
import MarkdownReport from '../components/MarkdownReport'
import { taskStatusMeta } from '../status'
import type { MeetingReviewItem, TaskStatus } from '../types'

const { Link, Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function statusTag(status: string, hasSummary: boolean) {
  if (hasSummary) return <Tag color="success">已有总结</Tag>
  if (status in taskStatusMeta) {
    const meta = taskStatusMeta[status as TaskStatus]
    return <Tag color={meta.color}>{meta.label}</Tag>
  }
  if (status) return <Tag>{status}</Tag>
  return <Tag>未进入处理</Tag>
}

function effectLink(effect: Record<string, unknown>, index: number) {
  const url = typeof effect.url === 'string' ? effect.url : ''
  const label = typeof effect.title === 'string'
    ? effect.title
    : typeof effect.kind === 'string' ? effect.kind : `产物 ${index + 1}`
  return url
    ? <Link key={`${url}-${index}`} href={url} target="_blank">{label}</Link>
    : <Text key={`${label}-${index}`} type="secondary">{label}</Text>
}

function meetingContent(item: MeetingReviewItem) {
  const details = [
    { key: 'time', label: '时间', children: item.start_at && item.end_at ? `${dayjs(item.start_at).format('HH:mm')}–${dayjs(item.end_at).format('HH:mm')}` : '未返回' },
    { key: 'host', label: '主持人', children: item.host || '未返回' },
    { key: 'participants', label: '参会人', children: item.participants || '未返回', span: 2 },
  ]
  return (
    <Card
      variant="borderless"
      title={<Space>{item.title}{statusTag(item.task_status, Boolean(item.summary))}</Space>}
      extra={(
        <Space>
          {item.meeting_url && <Button size="small" href={item.meeting_url} target="_blank">打开会议</Button>}
          {item.task_id && <Button size="small" href={`#/work/task/${item.task_id}`}>查看 Task</Button>}
        </Space>
      )}
    >
      <Descriptions size="small" column={2} items={details} />
      <div className="review-meeting-summary">
        {item.summary ? (
          <>
            <MarkdownReport content={item.summary} />
            <Space orientation="vertical" size={6}>
              {item.summary_generated_at && <Text type="secondary">生成于 {dayjs(item.summary_generated_at).format('YYYY-MM-DD HH:mm')}</Text>}
              {item.effects.length > 0 && <Space size={[12, 6]} wrap><Text type="secondary">相关产物：</Text>{item.effects.map(effectLink)}</Space>}
            </Space>
          </>
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无明确的会议总结产物" />
        )}
      </div>
    </Card>
  )
}

interface MeetingSummaryViewProps {
  date: string
  selectedMeetingID?: string
  onSelectMeeting: (meetingID: string) => void
}

export default function MeetingSummaryView({ date, selectedMeetingID, onSelectMeeting }: MeetingSummaryViewProps) {
  const [items, setItems] = useState<MeetingReviewItem[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    getMeetingReviews(date, controller.signal)
      .then((result) => setItems(result.items))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [date])

  const activeMeetingID = items.some((item) => item.meeting_id === selectedMeetingID)
    ? selectedMeetingID!
    : items[0]?.meeting_id
  const tabs = useMemo(() => items.map((item) => ({
    key: item.meeting_id,
    label: `${item.start_at ? dayjs(item.start_at).format('HH:mm') : '--:--'} ${item.title}`,
    children: item.meeting_id === activeMeetingID ? meetingContent(item) : null,
  })), [activeMeetingID, items])

  if (error) return <Alert type="error" showIcon title="会议总结加载失败" description={error} />
  if (loading) return <div className="review-loading"><Spin /></div>
  if (items.length === 0) {
    return <Card variant="borderless"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这一天没有已结束的会议" /></Card>
  }
  return (
    <Tabs
      className="review-secondary-tabs"
      activeKey={activeMeetingID}
      onChange={onSelectMeeting}
      items={tabs}
      tabBarGutter={8}
    />
  )
}
