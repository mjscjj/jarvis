import type { CSSProperties, ReactNode } from 'react'

interface MetricCardProps {
  title: string
  value: number | string
  hint?: string
  icon?: ReactNode
  loading?: boolean
  valueStyle?: CSSProperties
  onClick?: () => void
}

export default function MetricCard({ title, value, hint, icon, loading, valueStyle, onClick }: MetricCardProps) {
  return (
    <div className="metric-card" onClick={onClick} style={onClick ? { cursor: 'pointer' } : undefined}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div className="metric-card-label">{title}</div>
          {loading ? (
            <div style={{ width: 60, height: 32, background: 'var(--color-bg-soft)', borderRadius: 6, marginTop: 4 }} />
          ) : (
            <div className="metric-card-value" style={valueStyle}>{value}</div>
          )}
          {hint && <div className="metric-card-hint">{hint}</div>}
        </div>
        {icon && <div style={{ color: 'var(--color-primary)', fontSize: 20 }}>{icon}</div>}
      </div>
    </div>
  )
}
