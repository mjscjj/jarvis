import { Space, Typography } from 'antd'
import type { RunEnrichment } from '../types'
import { printableValue } from './taskValues'

const { Link, Paragraph, Text } = Typography

function enrichmentKindLabel(kind: string): string {
  switch (kind) {
    case 'context': return '正文'
    case 'doc_link': return '相关文档'
    case 'code_link': return '相关代码'
    case 'commit_digest': return 'Commit 摘要'
    default: return kind || '补充'
  }
}

function EnrichmentContent({ content, kind }: { content: unknown; kind: string }) {
  if (content === null) {
    return <pre className="task-enrichment-json">null</pre>
  }
  if (typeof content !== 'string') {
    return <pre className="task-enrichment-json">{printableValue(content)}</pre>
  }

  const isLink = kind === 'doc_link'
    || kind === 'code_link'
    || kind === 'link'
    || /^https?:\/\//.test(content.trim())
  if (!isLink) {
    return <Paragraph className="task-enrichment-detail">{content}</Paragraph>
  }

  const paths = content.split(/[；;\n]+/).map((path) => path.trim()).filter(Boolean)
  return (
    <Space orientation="vertical" size={2} className="task-enrichment-links">
      {paths.map((path, index) => /^https?:\/\//.test(path)
        ? <Link key={index} href={path} target="_blank" rel="noreferrer">{path}</Link>
        : <Text key={index} className="mono" copyable>{path}</Text>)}
    </Space>
  )
}

export function EnrichmentBlock({ item }: { item: RunEnrichment }) {
  const label = item.label?.trim() || enrichmentKindLabel(item.kind)
  return (
    <div className="task-enrichment">
      <Text strong>{label}</Text>
      <EnrichmentContent content={item.content} kind={item.kind} />
    </div>
  )
}
