import { useState } from 'react'
import { Button, Popover } from 'antd'
import { LinkOutlined, QuestionCircleOutlined } from '@ant-design/icons'
import documents from '../helpDocuments.json'

export function DeveloperDocumentLinks() {
  return (
    <nav className="developer-document-links" aria-label="开发文档">
      {documents.map((document) => (
        <a key={document.id} href={document.url} target="_blank" rel="noopener noreferrer">
          <LinkOutlined />
          <span>
            <strong>{document.title}</strong>
            <small>{document.description}</small>
          </span>
        </a>
      ))}
    </nav>
  )
}

export function DeveloperHelpButton({ showLabel = false }: { showLabel?: boolean }) {
  const [open, setOpen] = useState(false)
  return (
    <Popover
      title="开发文档"
      content={<DeveloperDocumentLinks />}
      trigger="click"
      placement="topLeft"
      open={open}
      onOpenChange={setOpen}
    >
      <Button
        type="text"
        size="small"
        icon={<QuestionCircleOutlined />}
        aria-label="查看开发文档"
        aria-expanded={open}
        title="查看开发文档"
        onKeyDown={(event) => {
          if (event.key === 'Escape') setOpen(false)
        }}
      >
        {showLabel && '开发文档'}
      </Button>
    </Popover>
  )
}
