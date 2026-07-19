import { Card, Statistic, Typography } from 'antd'
import type { CSSProperties, ReactNode } from 'react'

const { Text } = Typography

interface MetricCardProps {
  title: string
  value: number | string
  suffix?: string
  hint?: string
  icon?: ReactNode
  loading?: boolean
  valueStyle?: CSSProperties
  onClick?: () => void
}

export default function MetricCard({ title, value, suffix, hint, icon, loading, valueStyle, onClick }: MetricCardProps) {
  return (
    <Card
      variant="borderless"
      loading={loading}
      style={{
        borderRadius: 14,
        border: '1px solid var(--color-border)',
        boxShadow: 'var(--shadow-sm)',
        cursor: onClick ? 'pointer' : undefined,
      }}
      bodyStyle={{ padding: '20px 20px 16px' }}
      onClick={onClick}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <Text type="secondary" style={{ fontSize: 13 }}>{title}</Text>
          <Statistic
            value={value}
            suffix={suffix}
            valueStyle={{ fontSize: 32, fontWeight: 700, marginTop: 4, ...valueStyle }}
          />
          {hint && <Text type="secondary" style={{ fontSize: 12, display: 'block', marginTop: 4 }}>{hint}</Text>}
        </div>
        {icon && <div style={{ color: 'var(--color-primary)', fontSize: 22 }}>{icon}</div>}
      </div>
    </Card>
  )
}
