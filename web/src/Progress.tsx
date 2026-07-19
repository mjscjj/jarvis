import { useEffect, useState } from 'react'
import { Alert, Button, Card, Empty, Segmented, Space, Table, Tabs, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { getDigests, summarizeDigest } from './api'
import type { Digest, GroupProgress, MyDay } from './types'

const { Text, Paragraph } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

// A day row shows a dash when nothing happened so quiet days read as quiet.
function num(value: number) {
  return value > 0 ? value : <Text type="secondary">—</Text>
}

const myColumns: TableColumnsType<MyDay> = [
  { title: '日期', dataIndex: 'date', width: 120 },
  { title: '新增交办 Todo', dataIndex: 'todos_created', width: 130, render: num },
  { title: '确认生成任务', dataIndex: 'confirmed', width: 130, render: num },
  { title: '完成任务', dataIndex: 'tasks_done', width: 110, render: num },
  { title: '失败', dataIndex: 'tasks_failed', width: 90, render: (v: number) => (v > 0 ? <Tag color="red">{v}</Tag> : <Text type="secondary">—</Text>) },
]

function groupColumns(): TableColumnsType<GroupProgress['days'][number]> {
  return [
    { title: '日期', dataIndex: 'date', width: 120 },
    { title: '消息数', dataIndex: 'messages', width: 110, render: num },
    { title: '抽出 Todo', dataIndex: 'todos_extracted', width: 110, render: num },
  ]
}

export default function Progress() {
  const [days, setDays] = useState(7)
  const [data, setData] = useState<Digest>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [summary, setSummary] = useState<string>()
  const [summarizing, setSummarizing] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setSummary(undefined)
    getDigests(days, controller.signal)
      .then((result) => { setData(result); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [days])

  const generateSummary = async () => {
    setSummarizing(true)
    setError(undefined)
    try {
      const result = await summarizeDigest(days)
      setSummary(result.summary)
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setSummarizing(false)
    }
  }

  return (
    <div className="progress">
      <Card className="filter-card" variant="borderless">
        <Space size={16} wrap>
          <Space size={8}>
            <Text type="secondary">时间窗口</Text>
            <Segmented
              value={days}
              onChange={(value) => setDays(value as number)}
              options={[{ label: '近 7 天', value: 7 }, { label: '近 14 天', value: 14 }, { label: '近 30 天', value: 30 }]}
            />
          </Space>
          <Button type="primary" loading={summarizing} onClick={generateSummary}>生成人话总结（codex）</Button>
        </Space>
      </Card>

      {error && <Alert type="error" showIcon message="进度加载失败" description={error} closable onClose={() => setError(undefined)} />}

      {summary && (
        <Alert
          type="info"
          showIcon
          style={{ marginTop: 12 }}
          message="进展总结"
          description={<Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>{summary}</Paragraph>}
          closable
          onClose={() => setSummary(undefined)}
        />
      )}

      <Tabs
        style={{ marginTop: 16 }}
        items={[
          {
            key: 'mine',
            label: '个人进度',
            children: (
              <Card variant="borderless">
                <Table<MyDay>
                  rowKey="date"
                  size="small"
                  columns={myColumns}
                  dataSource={data?.mine ?? []}
                  loading={loading}
                  pagination={false}
                />
              </Card>
            ),
          },
          {
            key: 'groups',
            label: '群进度',
            children:
              !loading && (data?.key_groups.length ?? 0) === 0 ? (
                <Card variant="borderless">
                  <Empty description="暂无标记为核心群的会话（可在“背景设置 → 群”里标记 is_key_group）" />
                </Card>
              ) : (
                <Space direction="vertical" size={16} style={{ width: '100%' }}>
                  {(data?.key_groups ?? []).map((group) => (
                    <Card key={group.group_id} variant="borderless" title={group.name || group.chat_id}>
                      <Table
                        rowKey="date"
                        size="small"
                        columns={groupColumns()}
                        dataSource={group.days}
                        loading={loading}
                        pagination={false}
                      />
                    </Card>
                  ))}
                </Space>
              ),
          },
        ]}
      />
    </div>
  )
}
