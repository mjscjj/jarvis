import { useEffect } from 'react'
import type { ReactNode } from 'react'
import { X } from 'lucide-react'

interface DetailPanelProps {
  open: boolean
  onClose: () => void
  title?: ReactNode
  subtitle?: ReactNode
  children: ReactNode
  width?: number
  footer?: ReactNode
}

export default function DetailPanel({ open, onClose, title, subtitle, children, width = 480, footer }: DetailPanelProps) {
  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && open) onClose()
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [open, onClose])

  if (!open) return null

  return (
    <>
      <div className="detail-panel-overlay" onClick={onClose} />
      <div className="detail-panel" style={{ width }}>
        <div className="detail-panel-header">
          <div className="detail-panel-title">
            {title && <h2>{title}</h2>}
            {subtitle && <p>{subtitle}</p>}
          </div>
          <button className="btn btn-ghost btn-sm" onClick={onClose} aria-label="关闭">
            <X size={18} />
          </button>
        </div>
        <div className="detail-panel-body">{children}</div>
        {footer && <div className="detail-panel-footer">{footer}</div>}
      </div>
    </>
  )
}
