import { Tag } from 'antd'

interface StatusBadgeProps {
  status: string
  label: string
  color?: string
}

export default function StatusBadge({ status, label, color }: StatusBadgeProps) {
  return <Tag color={color ?? 'default'} style={{ fontWeight: 500, borderRadius: 6 }}>{label}</Tag>
}
