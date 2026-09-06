import { Tabs } from 'antd'
import type { ReactNode } from 'react'
import PageHeader from './PageHeader'

interface MergedPageTab {
  key: string
  label: ReactNode
}

interface MergedPageHeaderProps {
  title: string
  subtitle: string
  activeKey: string
  tabs: MergedPageTab[]
  onChange: (key: string) => void
  children?: ReactNode
}

export default function MergedPageHeader({
  title,
  subtitle,
  activeKey,
  tabs,
  onChange,
  children,
}: MergedPageHeaderProps) {
  return (
    <div className="merged-page-heading">
      <PageHeader title={title} subtitle={subtitle}>{children}</PageHeader>
      <Tabs
        className="merged-page-tabs"
        activeKey={activeKey}
        onChange={onChange}
        items={tabs}
      />
    </div>
  )
}
