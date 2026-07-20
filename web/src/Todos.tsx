import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Card,
  Descriptions,
  Drawer,
  Flex,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { getTodo, listTodos } from './api'
import { usePageContext } from './pageContext'
import { TodoContextPanel } from './slots'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { actionLabels, leaderColor, todoStatusMeta as statusMeta } from './status'
import type { ActionType, Todo, TodoQuery, TodoStatus } from './types'

const { Text, Paragraph } = Typography

const initialQuery: TodoQuery = {
  statuses: ['extracted', 'need_info', 'need_decision'],
  leaderOnly: false,
  page: 1,
  pageSize: 20,
}

function formatDate(value: string | null): string {
  if (!value) return '—'
  return new Intl.DateTimeFormat('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(new Date(value))
}

export default function Todos({ refreshKey }: { refreshKey: number }) {
  const { setSelection } = usePageContext()
  const [query, setQuery] = useState<TodoQuery>(initialQuery)
  const [items, setItems] = useState<Todo[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [selected, setSelected] = useState<Todo>()
  const [drawerLoading, setDrawerLoading] = useState(false)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    listTodos(query, controller.signal)
      .then((result) => {
        setItems(result.items)
        setTotal(result.total)
      })
      .catch((cause: unknown) => {
        if (cause instanceof DOMException && cause.name === 'AbortError') return
        setError(cause instanceof Error ? cause.message : String(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [query, refreshKey])

  const openTodo = useCallback(
    (todo: Todo) => {
      setSelected(todo)
      setSelection({ kind: 'todo', id: todo.id, label: todo.title })
      setDrawerLoading(true)
      getTodo(todo.id)
        .then(setSelected)
        .catch((cause: unknown) => setError(cause instanceof Error ? cause.message : String(cause)))
        .finally(() => setDrawerLoading(false))
    },
    [setSelection],
  )

  const closeTodo = useCallback(() => {
    setSelected(undefined)
    setSelection(null)
  }, [setSelection])

  const columns = useMemo<TableColumnsType<Todo>>(
    () => [
      {
        title: '行动线索',
        dataIndex: 'title',
        key: 'title',
        width: 360,
        render: (_, todo) => (
          <Space direction="vertical" size={3}>
            <Space size={8} wrap>
              {todo.is_leader_assigned && <StatusBadge label="Leader" color={leaderColor} />}
              <Text strong>{todo.title}</Text>
            </Space>
            <Text type="secondary" ellipsis={{ tooltip: todo.description }} className="description-cell">
              {todo.description}
            </Text>
          </Space>
        ),
      },
      {
        title: '类型',
        dataIndex: 'action_type',
        width: 130,
        render: (value: ActionType) => <Tag>{actionLabels[value]}</Tag>,
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 110,
        render: (value: TodoStatus) => <StatusBadge label={statusMeta[value].label} color={statusMeta[value].color} />,
      },
      {
        title: '项目 / 会话',
        key: 'context',
        width: 220,
        render: (_, todo) => (
          <Space direction="vertical" size={2}>
            <Text>{todo.project?.name || '未关联项目'}</Text>
            <Text type="secondary">{todo.group?.name || todo.group?.chat_id || '未知会话'}</Text>
          </Space>
        ),
      },
      {
        title: '待补充',
        dataIndex: 'open_questions',
        width: 180,
        render: (values: string[] | null) =>
          values?.length ? <Tag color="orange">{values.length} 个待拍板</Tag> : '—',
      },
      {
        title: '截止 / 最近证据',
        key: 'time',
        width: 170,
        render: (_, todo) => (
          <Space direction="vertical" size={2}>
            <Text>{formatDate(todo.due_at)}</Text>
            <Text type="secondary">证据 {formatDate(todo.last_evidence_at)}</Text>
          </Space>
        ),
      },
    ],
    [],
  )

  return (
    <>
      <PageHeader title="待办线索" subtitle="从会话中抽取的行动线索，点行查看详情" />
      <Card className="filter-card" variant="borderless">
        <Flex gap={16} align="end" wrap>
          <label className="filter-field filter-status">
            <Text type="secondary">状态</Text>
            <Select
              mode="multiple"
              value={query.statuses}
              options={Object.entries(statusMeta).map(([value, meta]) => ({ value, label: meta.label }))}
              onChange={(statuses) => setQuery((current) => ({ ...current, statuses, page: 1 }))}
              maxTagCount="responsive"
            />
          </label>
          <label className="filter-field">
            <Text type="secondary">行动类型</Text>
            <Select
              allowClear
              value={query.actionType}
              placeholder="全部类型"
              options={Object.entries(actionLabels).map(([value, label]) => ({ value, label }))}
              onChange={(actionType) => setQuery((current) => ({ ...current, actionType, page: 1 }))}
            />
          </label>
          <label className="switch-field">
            <Switch
              checked={query.leaderOnly}
              onChange={(leaderOnly) => setQuery((current) => ({ ...current, leaderOnly, page: 1 }))}
            />
            <Text>仅看 Leader 交办</Text>
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
          message="Todo 数据加载失败"
          description={error}
          closable
          onClose={() => setError(undefined)}
        />
      )}

      <Card className="table-card" variant="borderless">
        <Table<Todo>
          rowKey="id"
          columns={columns}
          dataSource={items}
          loading={loading}
          scroll={{ x: 1170 }}
          onRow={(todo) => ({ onClick: () => openTodo(todo), className: 'clickable-row' })}
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

      <Drawer
        title={selected?.title || 'Todo 详情'}
        open={Boolean(selected)}
        loading={drawerLoading}
        width={640}
        onClose={closeTodo}
      >
        {selected && (
          <Space direction="vertical" size={24} className="drawer-content">
            <Space wrap>
              <StatusBadge label={statusMeta[selected.status].label} color={statusMeta[selected.status].color} />
              <Tag>{actionLabels[selected.action_type]}</Tag>
              {selected.is_leader_assigned && <StatusBadge label="Leader 交办" color={leaderColor} />}
            </Space>
            <Paragraph>{selected.description}</Paragraph>
            <Descriptions column={2} size="small">
              <Descriptions.Item label="项目">{selected.project?.name || '未关联'}</Descriptions.Item>
              <Descriptions.Item label="会话">{selected.group?.name || selected.group?.chat_id || '未知'}</Descriptions.Item>
              <Descriptions.Item label="承诺强度">{selected.commitment_strength}</Descriptions.Item>
              <Descriptions.Item label="截止时间">{formatDate(selected.due_at)}</Descriptions.Item>
              <Descriptions.Item label="版本">rev {selected.revision} / v{selected.version}</Descriptions.Item>
              <Descriptions.Item label="证据数">{selected.source_message_ids.length}</Descriptions.Item>
            </Descriptions>
            <section>
              <Text type="secondary">源消息原文</Text>
              <blockquote>{selected.source_quote}</blockquote>
            </section>
            <section>
              <Text type="secondary">背景与待补充</Text>
              <TodoContextPanel target={selected.target} context={selected.context} openQuestions={selected.open_questions} />
            </section>
          </Space>
        )}
      </Drawer>
    </>
  )
}
