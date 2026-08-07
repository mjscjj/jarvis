import { useEffect, useState } from 'react'
import { Alert, Card, Empty, Space, Spin, Table, Tag, Typography } from 'antd'
import { FileDoneOutlined, FileTextOutlined, GitlabOutlined, InboxOutlined } from '@ant-design/icons'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { getCommitWorklog, getDocumentWorklog } from '../api'
import type { CommitMR, CommitWorklog, DocumentWorklog, WorkDoc } from '../types'

const { Link, Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function num(value: number) {
  return value > 0 ? value : <Text type="secondary">—</Text>
}

function timeOfDay(iso: string): string {
  if (!iso) return '—'
  const date = dayjs(iso)
  return date.isValid() ? date.format('HH:mm') : iso
}

const mrStatusMeta: Record<string, { color: string; label: string }> = {
  open: { color: 'processing', label: '进行中' },
  merged: { color: 'success', label: '已合入' },
  closed: { color: 'default', label: '已关闭' },
}

export function DocumentsView({ date }: { date: Dayjs }) {
  const [data, setData] = useState<DocumentWorklog>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setData(undefined)
    setError(undefined)
    getDocumentWorklog(date.format('YYYY-MM-DD'), controller.signal)
      .then(setData)
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [date])

  const renderDoc = (doc: WorkDoc, showFrom: boolean) => (
    <div key={`${doc.url}-${doc.time}`} className="review-worklog-row">
      <span className="review-worklog-icon"><FileTextOutlined /></span>
      <div className="review-worklog-content">
        <div className="review-worklog-title">
          {doc.url ? <Link href={doc.url} target="_blank">{doc.title || doc.url}</Link> : <Text>{doc.title || '(无标题)'}</Text>}
        </div>
        <Text type="secondary" className="review-worklog-meta">
          {timeOfDay(doc.time)}
          {showFrom && doc.from_chat ? ` · 来自 ${doc.from_chat}` : ''}
          {showFrom && doc.from_who ? ` · ${doc.from_who}` : ''}
        </Text>
      </div>
      <Tag>{doc.doc_type || '文档'}</Tag>
    </div>
  )

  if (loading) return <div className="review-loading"><Spin /></div>
  return (
    <div className="review-worklog-grid">
      {error && <Alert type="error" showIcon title="文档加载失败" description={error} />}
      <Card
        className="review-content-card review-worklog-card"
        variant="borderless"
        title={<div className="review-worklog-card-title"><span className="review-card-icon"><FileDoneOutlined /></span><div><Text>我写的文档</Text><small>{data?.authored.length ?? 0} 篇</small></div></div>}
      >
        {(data?.authored.length ?? 0) === 0
          ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有我编辑的文档" />
          : data!.authored.map((doc) => renderDoc(doc, false))}
      </Card>
      <Card
        className="review-content-card review-worklog-card"
        variant="borderless"
        title={<div className="review-worklog-card-title"><span className="review-card-icon review-card-icon-inbox"><InboxOutlined /></span><div><Text>我收到的文档</Text><small>{data?.received.length ?? 0} 篇</small></div></div>}
      >
        {(data?.received.length ?? 0) === 0
          ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有采集到的文档" />
          : data!.received.map((doc) => renderDoc(doc, true))}
      </Card>
    </div>
  )
}

export function CodeView({ date }: { date: Dayjs }) {
  const [data, setData] = useState<CommitWorklog>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setData(undefined)
    setError(undefined)
    getCommitWorklog(date.format('YYYY-MM-DD'), controller.signal)
      .then(setData)
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [date])

  const columns: TableColumnsType<CommitMR> = [
    { title: 'MR', dataIndex: 'title', render: (title: string, mr) => <Link href={mr.url} target="_blank">{title || mr.url}</Link> },
    {
      title: '状态', dataIndex: 'status', width: 90,
      render: (status: string) => {
        const meta = mrStatusMeta[status] ?? { color: 'default', label: status }
        return <Tag color={meta.color}>{meta.label}</Tag>
      },
    },
    { title: 'commit', dataIndex: 'commits_count', width: 80, render: num },
    { title: '变更行', dataIndex: 'changes_count', width: 80, render: num },
    { title: '目标分支', dataIndex: 'target_branch', width: 120, render: (branch: string) => branch || <Text type="secondary">—</Text> },
    { title: '更新', dataIndex: 'updated_at', width: 80, render: (value: string) => timeOfDay(value) },
  ]

  if (loading) return <div className="review-loading"><Spin /></div>
  if (error) return <Alert type="error" showIcon title="代码提交加载失败" description={error} />
  if ((data?.repos.length ?? 0) === 0) {
    return <Card className="review-content-card" variant="borderless"><div className="review-state-panel"><Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有我更新的 MR" /></div></Card>
  }
  return (
    <Space orientation="vertical" size={16} className="review-code-list">
      {data!.repos.map((repo) => (
        <Card
          key={repo.repo}
          className="review-content-card review-code-card"
          variant="borderless"
          title={<div className="review-worklog-card-title"><span className="review-card-icon review-card-icon-code"><GitlabOutlined /></span><div><Text>{repo.repo}</Text><small>{repo.mrs.length} 个 MR</small></div></div>}
        >
          <Table<CommitMR> rowKey="url" size="small" columns={columns} dataSource={repo.mrs} pagination={false} />
        </Card>
      ))}
    </Space>
  )
}
