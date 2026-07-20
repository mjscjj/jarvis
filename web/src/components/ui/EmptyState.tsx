import { Inbox } from 'lucide-react'

interface EmptyStateProps {
  description?: string
  hint?: string
}

export default function EmptyState({ description = '暂无数据', hint }: EmptyStateProps) {
  return (
    <div className="empty-state">
      <Inbox size={32} style={{ color: 'var(--color-text-tertiary)', marginBottom: 8 }} />
      <div className="empty-state-title">{description}</div>
      {hint && <div className="empty-state-hint">{hint}</div>}
    </div>
  )
}
