import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'

interface MarkdownReportProps {
  content: string
  className?: string
}

// Markdown reports are model-generated content. Keep raw HTML disabled and rely
// on react-markdown's default URL transform to reject unsafe URL protocols.
export default function MarkdownReport({ content, className }: MarkdownReportProps) {
  const classes = ['markdown-report', className].filter(Boolean).join(' ')

  return (
    <div className={classes}>
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ node: _node, ...props }) => (
            <a {...props} target="_blank" rel="noopener noreferrer" />
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}
