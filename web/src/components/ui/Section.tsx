import type { ReactNode } from 'react'

interface SectionProps {
  title: string
  subtitle?: string
  actions?: ReactNode
  children: ReactNode
  className?: string
}

export default function Section({ title, subtitle, actions, children, className = '' }: SectionProps) {
  return (
    <section className={`section ${className}`}>
      <div className="section-header">
        <div>
          <div className="section-title">{title}</div>
          {subtitle && <div className="section-subtitle">{subtitle}</div>}
        </div>
        {actions && <div className="section-actions">{actions}</div>}
      </div>
      <div className="section-content">{children}</div>
    </section>
  )
}
