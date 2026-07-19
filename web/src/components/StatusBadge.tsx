interface StatusBadgeProps {
  label: string
  color?: string
}

// Linear 风状态标签：小圆点 + 文案，柔和描边浅底，不用 antd 实心 Tag。
// color 传语义色值（来自 status.ts）；未传则中性灰。
export default function StatusBadge({ label, color = '#6b7280' }: StatusBadgeProps) {
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 6,
        padding: '2px 10px',
        fontSize: 12.5,
        fontWeight: 500,
        lineHeight: 1.5,
        color,
        background: `color-mix(in srgb, ${color} 8%, transparent)`,
        border: `1px solid color-mix(in srgb, ${color} 22%, transparent)`,
        borderRadius: 999,
        whiteSpace: 'nowrap',
      }}
    >
      <span style={{ width: 6, height: 6, borderRadius: '50%', background: color }} />
      {label}
    </span>
  )
}
