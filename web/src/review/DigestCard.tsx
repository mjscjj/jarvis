import { Alert, Button, Card, Empty, Space, Spin, Tag, Tooltip, Typography } from 'antd'
import { FileTextOutlined, ReloadOutlined, ThunderboltOutlined } from '@ant-design/icons'
import dayjs from 'dayjs'
import type { DailyDigest } from '../types'
import MarkdownReport from '../components/MarkdownReport'

const { Text } = Typography

const sourceLabels: Record<string, string> = {
  jarvis_internal: 'Jarvis 内部事实',
  feishu_work: '飞书工作证据',
  engineering_execution: '工程执行证据',
  jarvis_messages: '消息',
  jarvis_todos: 'Todo',
  jarvis_tasks: 'Task',
  lark_documents: '文档',
  lark_calendar: '日历',
  lark_meetings: '会议',
  lark_minutes: '妙记',
  code_mrs: 'MR',
  git_commits: 'Commit',
  jarvis_group_messages: '已采集群消息',
  lark_group_messages: '飞书群消息',
  code_commits: '关联 Commit',
  other_materials: '其他材料',
}

interface DigestCardProps {
  title: string
  item?: DailyDigest
  generating: boolean
  primaryAction?: boolean
  onGenerate: () => void
}

export default function DigestCard({ title, item, generating, primaryAction, onGenerate }: DigestCardProps) {
  const status = item?.status
  const statusTag = status === 'done'
    ? <Tag color="success">已生成</Tag>
    : status === 'failed'
      ? <Tag color="error">失败</Tag>
      : status === 'generating'
        ? <Tag color="processing">生成中</Tag>
        : <Tag>未生成</Tag>
  const buttonLabel = status === 'failed' ? '重试' : item ? '重新生成' : '生成'
  const coverage = Object.entries(item?.source_coverage ?? {})

  return (
    <Card
      className="review-content-card review-digest-card"
      variant="borderless"
      title={(
        <div className="review-card-heading">
          <span className="review-card-icon"><FileTextOutlined /></span>
          <div>
            <Text className="review-card-eyebrow">{primaryAction ? 'DAILY BRIEF' : 'GROUP BRIEF'}</Text>
            <div className="review-card-title"><span>{title}</span>{statusTag}</div>
          </div>
        </div>
      )}
      extra={(
        <Button
          type={primaryAction ? 'primary' : 'default'}
          icon={item ? <ReloadOutlined /> : <ThunderboltOutlined />}
          loading={generating}
          disabled={generating}
          onClick={onGenerate}
        >
          {buttonLabel}
        </Button>
      )}
    >
      {status === 'failed' ? (
        <Alert type="error" showIcon title="生成失败" description={item?.error_detail || '未记录错误详情'} />
      ) : item?.summary ? (
        <>
          <div className="review-reading-canvas">
            <MarkdownReport className="daily-digest-markdown" content={item.summary} />
          </div>
          <Space orientation="vertical" size={8} className="review-digest-footer">
            <Text type="secondary">
              {item.generated_at ? `生成于 ${dayjs(item.generated_at).format('YYYY-MM-DD HH:mm')}` : '尚未生成'}
              {item.cutoff_at ? ` · 数据截至 ${dayjs(item.cutoff_at).format('YYYY-MM-DD HH:mm')}` : ''}
              {` · ${item.source_count} 条证据 · ${item.trigger_type === 'schedule' ? '自动生成' : '手动生成'} · ${item.engine}`}
            </Text>
            {coverage.length > 0 && (
              <Space size={[4, 4]} wrap>
                <Text type="secondary">数据来源：</Text>
                {coverage.map(([source, sourceState]) => (
                  <Tooltip key={source} title={sourceState.note}>
                    <Tag color={
                      sourceState.status === 'error' || sourceState.status === 'unavailable'
                        ? 'error'
                        : sourceState.status === 'ok' || sourceState.status === 'complete'
                          ? 'success'
                          : sourceState.status === 'partial'
                            ? 'warning'
                            : 'default'
                    }>
                      {sourceLabels[source] ?? source}{' '}
                      {sourceState.status === 'error'
                        ? '失败'
                        : sourceState.status === 'unavailable'
                          ? '不可用'
                          : sourceState.status === 'partial'
                            ? `部分 ${sourceState.count}`
                            : sourceState.count}
                    </Tag>
                  </Tooltip>
                ))}
              </Space>
            )}
          </Space>
        </>
      ) : generating ? (
        <div className="review-state-panel"><Space><Spin size="small" /><Text type="secondary">正在整理这一天的工作，页面会自动刷新</Text></Space></div>
      ) : (
        <div className="review-state-panel"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这一天还没有总结" /></div>
      )}
    </Card>
  )
}
