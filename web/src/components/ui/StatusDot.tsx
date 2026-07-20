interface StatusDotProps {
  color: string
  size?: number
  label?: string
}

export default function StatusDot({ color, size = 8, label }: StatusDotProps) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      <span
        className="status-dot"
        style={{ width: size, height: size, background: color }}
      />
      {label && <span style={{ fontSize: 12, color: 'var(--color-text-secondary)' }}>{label}</span>}
    </span>
  )
}
