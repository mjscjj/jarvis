import { LinkOutlined } from '@ant-design/icons'
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
