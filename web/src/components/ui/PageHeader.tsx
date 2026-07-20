import type { ReactNode } from 'react'
import { Search } from 'lucide-react'

interface PageHeaderProps {
  title: string
  subtitle?: string
  actions?: ReactNode
  onSearch?: () => void
}

export default function PageHeader({ title, subtitle, actions, onSearch }: PageHeaderProps) {
  return (
    <div className="page-header">
      <div className="page-header-main">
        <h1 className="page-title">{title}</h1>
        {subtitle && <p className="page-subtitle">{subtitle}</p>}
      </div>
      <div className="page-header-actions">
        {onSearch && (
          <button className="btn btn-secondary" onClick={onSearch}>
            <Search size={16} />
            <span>搜索</span>
            <kbd className="kbd">⌘K</kbd>
          </button>
        )}
        {actions}
      </div>
    </div>
  )
}
