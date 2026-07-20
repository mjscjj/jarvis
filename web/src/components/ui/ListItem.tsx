import type { ReactNode } from 'react'

interface ListItemProps {
  title: ReactNode
  description?: ReactNode
  meta?: ReactNode
  actions?: ReactNode
  onClick?: () => void
  selected?: boolean
  leading?: ReactNode
}

export default function ListItem({ title, description, meta, actions, onClick, selected, leading }: ListItemProps) {
  return (
    <div
      className={`list-item ${selected ? 'selected' : ''}`}
      onClick={onClick}
      role={onClick ? 'button' : undefined}
      tabIndex={onClick ? 0 : undefined}
    >
      {leading && <div className="list-item-leading">{leading}</div>}
      <div className="list-item-main">
        <div className="list-item-title">{title}</div>
        {description && <div className="list-item-description">{description}</div>}
        {meta && <div className="list-item-meta">{meta}</div>}
      </div>
      {actions && (
        <div className="list-item-actions" onClick={(e) => e.stopPropagation()}>
          {actions}
        </div>
      )}
    </div>
  )
}
