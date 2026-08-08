import { useState } from 'react'
import { Alert, Button, Card, DatePicker, Empty, Flex, Form, Input, InputNumber, Modal, Pagination, Segmented, Select, Space, Spin, Tag, Typography } from 'antd'
import type { Dayjs } from 'dayjs'
import dayjs from 'dayjs'
import { appendFact, searchFacts } from '../api'
import type { FactSearchResult } from '../types'
import FactTimeline from './FactTimeline'

const { Text } = Typography

type Layer = 'all' | 'detail' | 'rollup'

interface FactInputFields {
  subject_type: string
  subject_id: number
  description: string
  occurred_at: Dayjs
}

export default function FactsPanel() {
  const [keyword, setKeyword] = useState('')
  const [range, setRange] = useState<[Dayjs | null, Dayjs | null] | null>(null)
  const [subjectType, setSubjectType] = useState<string>()
  const [subjectId, setSubjectId] = useState<number | null>(null)
  const [sourceKind, setSourceKind] = useState<string>()
  const [layer, setLayer] = useState<Layer>('all')
  const [result, setResult] = useState<FactSearchResult>()
  const [searching, setSearching] = useState(false)
  const [error, setError] = useState<string>()
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [refreshToken, setRefreshToken] = useState(0)
  const [form] = Form.useForm<FactInputFields>()

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
        layer,
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
    setLayer('all')
    setResult(undefined)
    setError(undefined)
  }

  const create = async () => {
    const values = await form.validateFields()
    setSaving(true)
    try {
      await appendFact({
        subject_type: values.subject_type,
        subject_id: values.subject_id,
        description: values.description,
        occurred_at: values.occurred_at.toISOString(),
        source_kind: 'manual',
      })
      setOpen(false)
      form.resetFields()
      setRefreshToken((value) => value + 1)
      setError(undefined)
      if (result) await runSearch()
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Card variant="borderless" className="fact-search-card">
        <Flex justify="space-between" align="center" gap={12} wrap>
          <div>
            <Text strong>搜索与管理事实</Text>
            <div><Text type="secondary">搜索会覆盖压缩摘要和被折叠的原始事实。</Text></div>
          </div>
          <Button type="primary" onClick={() => { form.setFieldsValue({ occurred_at: dayjs() }); setOpen(true) }}>记录事实</Button>
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
            options={['project', 'key_matter', 'person', 'group', 'task', 'todo', 'resource', 'managed_resource', 'principal'].map((value) => ({ value, label: value }))}
          />
          <InputNumber min={1} placeholder="主体 ID" value={subjectId} onChange={setSubjectId} style={{ width: 110 }} />
          <Select allowClear placeholder="来源" value={sourceKind} onChange={setSourceKind} style={{ width: 130 }} options={['manual', 'm3', 'm5', 'message', 'background', 'rollup'].map((value) => ({ value, label: value }))} />
          <Segmented<Layer> value={layer} onChange={setLayer} options={[{ value: 'all', label: '全部层' }, { value: 'detail', label: '原始' }, { value: 'rollup', label: '压缩' }]} />
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
                    <Tag color={fact.source_kind === 'rollup' ? 'green' : undefined}>{fact.source_kind === 'rollup' ? '压缩' : fact.source_kind || '未知来源'}</Tag>
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
      ) : <FactTimeline title="事实时间线" refreshToken={refreshToken} />}

      <Modal title="记录事实" open={open} confirmLoading={saving} onOk={() => void create()} onCancel={() => setOpen(false)} okText="记录" destroyOnHidden>
        <Form form={form} layout="vertical">
          <Flex gap={12}>
            <Form.Item name="subject_type" label="主体类型" rules={[{ required: true, message: '请输入主体类型' }]} style={{ flex: 1 }}>
              <Input placeholder="如 project / person / task" />
            </Form.Item>
            <Form.Item name="subject_id" label="主体 ID" rules={[{ required: true, message: '请输入主体 ID' }]} style={{ width: 140 }}>
              <InputNumber min={1} style={{ width: '100%' }} />
            </Form.Item>
          </Flex>
          <Form.Item name="occurred_at" label="发生时间" rules={[{ required: true }]}><DatePicker showTime style={{ width: '100%' }} /></Form.Item>
          <Form.Item name="description" label="事实" rules={[{ required: true, whitespace: true, message: '请输入事实' }]}><Input.TextArea rows={6} placeholder="只记录已经发生、可追溯的事实。" /></Form.Item>
        </Form>
      </Modal>
    </Space>
  )
}
