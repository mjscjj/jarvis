import type { ReactNode } from 'react'
import { Button, Tag, Typography } from 'antd'
import { ApiOutlined, CalendarOutlined, FileTextOutlined, LinkOutlined, MessageOutlined, PaperClipOutlined, PullRequestOutlined, SafetyOutlined, UndoOutlined } from '@ant-design/icons'
import type { Effect } from '../types'
import { formatTime, printableValue } from './taskValues'

const { Link, Text } = Typography

export interface EffectRecall {
  pending?: string
  run: (messageID: string) => void
}

// Unknown effect fields stay visible instead of being silently discarded.
const KNOWN_EFFECT_FIELDS = new Set(['kind', 'title', 'url', 'target', 'preview', 'recalled_at'])

function effectKindMeta(kind: string): { icon: ReactNode; label: string } {
  switch (kind) {
    case 'feishu_message':
    case 'message':
    case 'reply_message':
    case 'summary_post':
      return { icon: <MessageOutlined />, label: '飞书消息' }
    case 'feishu_doc':
    case 'doc':
    case 'doc_write':
    case 'document':
      return { icon: <FileTextOutlined />, label: '飞书文档' }
    case 'calendar_event':
    case 'meeting':
    case 'schedule_meeting':
      return { icon: <CalendarOutlined />, label: '日程 / 会议' }
    case 'merge_request':
    case 'mr':
    case 'pull_request':
      return { icon: <PullRequestOutlined />, label: 'Merge Request' }
    case 'permission_request':
    case 'permission':
      return { icon: <SafetyOutlined />, label: '权限申请' }
    case 'file':
    case 'attachment':
      return { icon: <PaperClipOutlined />, label: '文件' }
    default:
      return { icon: <ApiOutlined />, label: kind || '对外动作' }
  }
}

function EffectExtraFields({ effect }: { effect: Effect }) {
  const extras = Object.entries(effect).filter(([key]) => !KNOWN_EFFECT_FIELDS.has(key))
  if (extras.length === 0) return null
  return (
    <div className="task-effect-extra">
      {extras.map(([key, value]) => {
        const text = printableValue(value)
        const isLink = typeof value === 'string' && /^https?:\/\//.test(value.trim())
        return (
          <div className="task-effect-extra-row" key={key}>
            <Text type="secondary" className="task-effect-extra-key">{key}</Text>
            {isLink
              ? <Link href={(value as string).trim()} target="_blank" rel="noreferrer">{text}</Link>
              : <Text className="task-effect-extra-value">{text}</Text>}
          </div>
        )
      })}
    </div>
  )
}

function recallableMessageID(effect: Effect): string {
  const id = typeof effect.message_id === 'string' ? effect.message_id.trim() : ''
  return id.startsWith('om_') ? id : ''
}

export function EffectCard({ effect, recall }: { effect: Effect; recall: EffectRecall }) {
  const meta = effectKindMeta(effect.kind)
  const url = typeof effect.url === 'string' ? effect.url.trim() : ''
  const target = typeof effect.target === 'string' ? effect.target.trim() : ''
  const preview = typeof effect.preview === 'string' ? effect.preview.trim() : ''
  const title = typeof effect.title === 'string' && effect.title.trim()
    ? effect.title.trim()
    : meta.label
  const messageID = recallableMessageID(effect)
  const recalledAt = typeof effect.recalled_at === 'string' ? effect.recalled_at.trim() : ''
  return (
    <div className={`task-effect-card${recalledAt ? ' task-effect-recalled' : ''}`}>
      <div className="task-effect-head">
        <span className="task-effect-icon">{meta.icon}</span>
        <div className="task-effect-headings">
          <Text strong className="task-effect-title">{title}</Text>
          <Tag className="task-effect-kind">{meta.label}</Tag>
        </div>
        {recalledAt ? (
          <Tag icon={<UndoOutlined />}>已撤回 · {formatTime(recalledAt)}</Tag>
        ) : messageID ? (
          <Button
            size="small"
            danger
            icon={<UndoOutlined />}
            loading={recall.pending === messageID}
            onClick={() => recall.run(messageID)}
          >
            撤回
          </Button>
        ) : null}
      </div>
      {target && (
        <div className="task-effect-target">
          <Text type="secondary">对象</Text>
          <Text>{target}</Text>
        </div>
      )}
      {url && (
        <div className="task-effect-link">
          <LinkOutlined />
          <Link href={url} target="_blank" rel="noreferrer">{url}</Link>
        </div>
      )}
      {preview && <div className="task-effect-preview">{preview}</div>}
      <EffectExtraFields effect={effect} />
    </div>
  )
}

export function EffectsCard({ effects, recall }: { effects: Effect[]; recall: EffectRecall }) {
  if (effects.length === 0) return null
  return (
    <div className="task-primary-card task-effects-card">
      <div className="task-section-kicker">对外产出（{effects.length}）</div>
      <div className="task-effect-list">
        {effects.map((effect, index) => <EffectCard key={index} effect={effect} recall={recall} />)}
      </div>
    </div>
  )
}
