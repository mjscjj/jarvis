import { useState } from 'react'
import { Alert, Button, Card, DatePicker, Empty, Flex, Input, InputNumber, Pagination, Select, Space, Spin, Tag, Typography } from 'antd'
import type { Dayjs } from 'dayjs'
import dayjs from 'dayjs'
import { searchFacts } from '../api'
import type { FactSearchResult } from '../types'
import FactTimeline from './FactTimeline'

const { Text } = Typography

export default function FactsPanel() {
  const [keyword, setKeyword] = useState('')
  const [range, setRange] = useState<[Dayjs | null, Dayjs | null] | null>(null)
  const [subjectType, setSubjectType] = useState<string>()
  const [subjectId, setSubjectId] = useState<number | null>(null)
  const [sourceKind, setSourceKind] = useState<string>()
  const [result, setResult] = useState<FactSearchResult>()
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string>()

  const runSearch = async (page = 1) => {
    if (subjectId && !subjectType) {
      setError('填写主体 ID 时必须同时填写主体类型')
      return
    }
    setSearching(true)
    try {
      const data = await searchFacts({
        q: keyword.trim() || undefined,
        from: range?.[0]?.startOf('day').toISOString(),
        until: range?.[1]?.add(1, 'day').startOf('day').toISOString(),
        subjectType,
        subjectId: subjectId || undefined,
        sourceKind,
        page,
      })
      setResult(data)
      setError(undefined)
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setSearching(false)
    }
  }

  const clearSearch = () => {
    setKeyword('')
    setRange(null)
    setSubjectType(undefined)
    setSubjectId(null)
    setSourceKind(undefined)
    setResult(undefined)
    setError(undefined)
  }

  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Card variant="borderless" className="fact-search-card">
        <Flex justify="space-between" align="center" gap={12} wrap>
          <div>
            <Text strong>全部事实</Text>
            <div><Text type="secondary">跨主题查看证据索引，并按来源追溯原始材料。</Text></div>
          </div>
        </Flex>
        <Flex gap={10} wrap className="fact-search-controls">
          <Input.Search placeholder="搜索事实内容、主体名称或 type/id" value={keyword} onChange={(event) => setKeyword(event.target.value)} onSearch={() => void runSearch()} allowClear style={{ minWidth: 280, flex: 1 }} />
          <DatePicker.RangePicker value={range} onChange={(value) => setRange(value ? [value[0], value[1]] : null)} />
          <Select
            allowClear
            showSearch
            placeholder="主体类型"
            value={subjectType}
            onChange={setSubjectType}
            style={{ width: 140 }}
            options={['project', 'key_matter', 'project_risk', 'project_change', 'person', 'group', 'task', 'todo', 'resource', 'principal'].map((value) => ({ value, label: value }))}
          />
          <InputNumber min={1} placeholder="主体 ID" value={subjectId} onChange={setSubjectId} style={{ width: 110 }} />
          <Select allowClear placeholder="来源" value={sourceKind} onChange={setSourceKind} style={{ width: 130 }} options={['system', 'message', 'todo_event', 'task_event', 'execution_run', 'resource'].map((value) => ({ value, label: value }))} />
          <Button type="primary" onClick={() => void runSearch()} loading={searching}>搜索</Button>
          {result && <Button onClick={clearSearch}>回到时间线</Button>}
        </Flex>
      </Card>

      {error && <Alert type="error" showIcon title="事实操作失败" description={error} closable onClose={() => setError(undefined)} />}

      {result ? (
        <Card variant="borderless" title={`搜索结果 · ${result.total} 条`}>
          {searching ? <div className="fact-loading"><Spin /></div> : result.items.length === 0 ? <Empty description="没有匹配的事实" /> : (
            <Space orientation="vertical" size={10} style={{ width: '100%' }}>
              {result.items.map((fact) => (
                <div className="fact-search-result" key={fact.id}>
                  <Flex gap={8} align="center" wrap>
                    <Text strong>{fact.subject_label}</Text>
                    <Tag>{fact.subject_type}/{fact.subject_id}</Tag>
                    <Tag>{fact.source_kind || '未知来源'}</Tag>
                    <Text type="secondary">{dayjs(fact.occurred_at).format('YYYY-MM-DD HH:mm')}</Text>
                  </Flex>
                  <div className="fact-description">{fact.description}</div>
                </div>
              ))}
              {result.total > result.page_size && (
                <Flex justify="flex-end">
                  <Pagination current={result.page} pageSize={result.page_size} total={result.total} showSizeChanger={false} onChange={(page) => void runSearch(page)} />
                </Flex>
              )}
            </Space>
          )}
        </Card>
      ) : <FactTimeline title="全部事实时间线" />}
    </Space>
  )
}
