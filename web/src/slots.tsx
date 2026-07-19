import { Descriptions, Empty } from 'antd'

const SLOT_LABELS: Record<string, string> = {
  repo_ref: '仓库',
  change_summary: '改动摘要',
  based_on: '依据',
  scope: '范围',
  acceptance: '验收标准',
  source_ref: '来源引用',
  target_chat_id: '目标会话',
  summary_scope: '总结范围',
  assignees: '负责人',
  question: '问题',
  lookup_sources: '查询来源',
  deliverable: '交付物',
  meeting_title: '会议主题',
  attendees: '参会人',
  proposed_time: '建议时间',
  duration_minutes: '时长(分钟)',
  agenda: '议程',
  meeting_room: '会议室',
  message_body: '消息内容',
  doc_title: '文档标题',
  followup_action: '跟进动作',
}

function isEmpty(value: unknown): boolean {
  if (value === null || value === undefined || value === '') return true
  if (Array.isArray(value)) return value.length === 0
  return false
}

function renderValue(value: unknown): string {
  if (Array.isArray(value)) return value.join('、')
  return String(value)
}

export function SlotDescriptions({ slots }: { slots: Record<string, unknown> }) {
  const entries = Object.entries(slots ?? {}).filter(([, value]) => !isEmpty(value))
  if (entries.length === 0) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="无结构化参数" />
  return (
    <Descriptions column={1} size="small" bordered>
      {entries.map(([key, value]) => (
        <Descriptions.Item key={key} label={SLOT_LABELS[key] ?? key}>
          {renderValue(value)}
        </Descriptions.Item>
      ))}
    </Descriptions>
  )
}
