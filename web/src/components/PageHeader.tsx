import { Flex, Typography } from 'antd'
import type { ReactNode } from 'react'

const { Title, Text } = Typography

interface PageHeaderProps {
  title: string
  subtitle?: string
  children?: ReactNode
}

export default function PageHeader({ title, subtitle, children }: PageHeaderProps) {
  return (
    <Flex justify="space-between" align="center" wrap gap={16} className="page-header">
      <div>
        <Title level={1} className="page-title">{title}</Title>
        {subtitle && <Text type="secondary" style={{ fontSize: 13 }}>{subtitle}</Text>}
      </div>
      {children && <Flex gap={8}>{children}</Flex>}
    </Flex>
  )
}
