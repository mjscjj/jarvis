import { Space, Tooltip } from 'antd'
import type { StatusCount } from '../types'

interface StatusStackBarProps {
  items: StatusCount[]
  meta: Record<string, { label: string; color: string }>
  height?: number
  showLegend?: boolean
}

const ANT_COLOR_MAP: Record<string, string> = {
  green: '#16a34a',
  blue: '#2563eb',
  orange: '#ea580c',
  red: '#dc2626',
  gold: '#d97706',
  volcano: '#ea580c',
  geekblue: '#2563eb',
  default: '#78716c',
  processing: '#d97706',
}

function resolveColor(antColor: string): string {
  return ANT_COLOR_MAP[antColor] ?? antColor
}

export default function StatusStackBar({ items, meta, height = 12, showLegend = true }: StatusStackBarProps) {
  const total = items.reduce((sum, item) => sum + item.count, 0)
  if (total === 0) return null

  const segments = items
    .filter((item) => item.count > 0)
    .map((item) => ({
      ...item,
      percent: (item.count / total) * 100,
      label: meta[item.status]?.label ?? item.status,
      color: resolveColor(meta[item.status]?.color ?? 'default'),
    }))

  return (
    <div>
      <div
        style={{
          display: 'flex',
          width: '100%',
          height,
          borderRadius: height / 2,
          overflow: 'hidden',
          background: '#e7e5e4',
        }}
      >
        {segments.map((seg) => (
          <Tooltip key={seg.status} title={`${seg.label} · ${seg.count} (${Math.round(seg.percent)}%)`}>
            <div
              style={{
                width: `${seg.percent}%`,
                background: seg.color,
                transition: 'width 0.3s ease',
              }}
            />
          </Tooltip>
        ))}
      </div>
      {showLegend && (
        <Space size={16} wrap style={{ marginTop: 8 }}>
          {segments.map((seg) => (
            <div key={seg.status} style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
              <div
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: '50%',
                  background: seg.color,
                }}
              />
              <span style={{ fontSize: 12, color: 'var(--color-text-secondary)' }}>
                {seg.label} {Math.round(seg.percent)}%
              </span>
            </div>
          ))}
        </Space>
      )}
    </div>
  )
}
