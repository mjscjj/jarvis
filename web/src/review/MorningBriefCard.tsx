import { Card, Empty, Space, Tag, Typography } from 'antd'
import { FileTextOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import type { MorningBrief } from '../types'
import MarkdownReport from '../components/MarkdownReport'

const { Text } = Typography

interface MorningBriefCardProps {
  item?: MorningBrief
}

export default function MorningBriefCard({ item }: MorningBriefCardProps) {
  return (
    <Card
      className="review-content-card review-digest-card"
      variant="borderless"
      title={(
        <div className="review-card-heading">
          <span className="review-card-icon"><FileTextOutlined /></span>
          <div>
            <Text className="review-card-eyebrow">DAILY BRIEF</Text>
            <div className="review-card-title"><span>我的晨报</span><Tag color={item ? 'success' : undefined}>{item ? '已生成' : '未生成'}</Tag></div>
          </div>
        </div>
      )}
    >
      {item ? (
        <>
          <div className="review-reading-canvas">
            <MarkdownReport className="daily-digest-markdown" content={item.content} />
          </div>
          <Space orientation="vertical" size={8} className="review-digest-footer">
            <Text type="secondary">生成于 {dayjs(item.generated_at).format('YYYY-MM-DD HH:mm')}</Text>
          </Space>
        </>
      ) : (
        <div className="review-state-panel"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这一天还没有晨报" /></div>
      )}
    </Card>
  )
}
