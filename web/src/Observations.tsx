import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Card, Flex, Input, Popconfirm, Select, Space, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { deleteObservation, listObservations } from './api'
import PageHeader from './components/PageHeader'
import type { Observation, ObservationProducer, ObservationQuery } from './types'

const { Text, Paragraph } = Typography

const producerMeta: Record<ObservationProducer, { label: string; color: string }> = {
  m3: { label: '会话', color: 'blue' },
  m5: { label: '执行', color: 'purple' },
}

const initialQuery: ObservationQuery = { page: 1, pageSize: 20 }

function formatDate(value: string): string {
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function Observations() {
  const [query, setQuery] = useState<ObservationQuery>(initialQuery)
  const [keyword, setKeyword] = useState('')
  const [items, setItems] = useState<Observation[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [reloadKey, setReloadKey] = useState(0)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    listObservations(query, controller.signal)
      .then((result) => {
        setItems(result.items)
        setTotal(result.total)
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return
        setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [query, reloadKey])

  const removeRow = useCallback((id: number) => {
    deleteObservation(id)
      .then(() => setReloadKey((value) => value + 1))
      .catch((cause: unknown) => setError(errorText(cause)))
  }, [])

  const columns = useMemo<TableColumnsType<Observation>>(
    () => [
      {
        title: '主题',
        dataIndex: 'subject',
        width: 220,
        ellipsis: true,
        render: (value: string) => <Text strong ellipsis={{ tooltip: value }}>{value}</Text>,
      },
      {
        title: '观察到的事实',
        dataIndex: 'content',
        render: (value: string, row) => (
          <Space direction="vertical" size={2} style={{ width: '100%' }}>
            <Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }} ellipsis={{ rows: 2, expandable: true, symbol: '展开' }}>
              {value}
            </Paragraph>
            {row.source_quote && (
              <Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: row.source_quote }}>
                原文：{row.source_quote}
              </Text>
            )}
          </Space>
        ),
      },
      {
        title: '来源',
        dataIndex: 'producer',
        width: 76,
        render: (value: ObservationProducer) => (
          <Tag color={producerMeta[value]?.color}>{producerMeta[value]?.label ?? value}</Tag>
        ),
      },
      {
        title: '项目 / 会话',
        key: 'context',
        width: 180,
        ellipsis: true,
        render: (_, row) => (
          <Space direction="vertical" size={0}>
            <Text ellipsis style={{ fontSize: 13 }}>{row.project_name || '未关联项目'}</Text>
            <Text type="secondary" ellipsis style={{ fontSize: 12 }}>
              {row.group_name || (row.source_run_id ? `执行 run #${row.source_run_id}` : '—')}
            </Text>
          </Space>
        ),
      },
      {
        title: '发生时间',
        dataIndex: 'observed_at',
        width: 108,
        render: (value: string) => <Text type="secondary" style={{ fontSize: 12 }}>{formatDate(value)}</Text>,
      },
      {
        title: '',
        key: 'actions',
        width: 56,
        align: 'center',
        render: (_, row) => (
          <Popconfirm
            title="删除这条观察？"
            description="观察只是记录，删掉不影响任何待办。"
            okText="删除"
            cancelText="取消"
            onConfirm={() => removeRow(row.id)}
          >
            <a onClick={(event) => event.stopPropagation()}>删除</a>
          </Popconfirm>
        ),
      },
    ],
    [removeRow],
  )

  return (
    <>
      <PageHeader
        title="观察"
        subtitle="值得记住、但不需要我做事的事实。不会变成待办，也不会被执行。"
      />
      <Card className="filter-card" variant="borderless">
        <Flex gap={16} align="end" wrap>
          <label className="filter-field">
            <Text type="secondary">来源</Text>
            <Select
              allowClear
              value={query.producer}
              placeholder="全部来源"
              options={[
                { value: 'm3', label: '会话抽取' },
                { value: 'm5', label: '执行发现' },
              ]}
              onChange={(producer) => setQuery((current) => ({ ...current, producer, page: 1 }))}
            />
          </label>
          <label className="filter-field">
            <Text type="secondary">搜索</Text>
            <Input.Search
              allowClear
              value={keyword}
              placeholder="主题或内容"
              onChange={(event) => setKeyword(event.target.value)}
              onSearch={(value) => setQuery((current) => ({ ...current, keyword: value.trim() || undefined, page: 1 }))}
            />
          </label>
          <div className="result-count">
            <Text type="secondary">当前结果</Text>
            <Text strong>{total}</Text>
          </div>
        </Flex>
      </Card>

      {error && (
        <Alert
          type="error"
          showIcon
          message="观察加载失败"
          description={error}
          closable
          onClose={() => setError(undefined)}
        />
      )}

      <Card className="table-card" variant="borderless">
        <Table<Observation>
          rowKey="id"
          size="small"
          columns={columns}
          dataSource={items}
          loading={loading}
          scroll={{ x: 900 }}
          pagination={{
            current: query.page,
            pageSize: query.pageSize,
            total,
            showSizeChanger: true,
            pageSizeOptions: [20, 50, 100],
            onChange: (page, pageSize) => setQuery((current) => ({ ...current, page, pageSize })),
          }}
        />
      </Card>
    </>
  )
}
