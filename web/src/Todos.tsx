import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Card,
  Descriptions,
  Drawer,
  Flex,
  Segmented,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import { getTodo, listTodos, setTodoStatus } from './api'
import { usePageContext } from './pageContext'
import { TodoContextPanel } from './slots'
import PageHeader from './components/PageHeader'
import StatusBadge from './components/StatusBadge'
import { actionLabels, leaderColor, todoStatusMeta as statusMeta } from './status'
import type { Todo, TodoQuery, TodoStatus } from './types'
import './styles/clues-automation.css'

const { Text, Paragraph } = Typography

// 线索列表里可以手工互换的两个状态：都表示「眼下没人在动手」，区别只是
// 这条线索要不要重新进入 Task 固化与执行流水线。其余状态由流水线写入。
const settableStatuses: TodoStatus[] = ['extracted', 'observing']

type ClueScope = 'actionable' | 'observing' | 'materialized' | 'all'

const scopeStatuses: Record<ClueScope, TodoStatus[]> = {
  actionable: ['extracted'],
  observing: ['observing'],
  materialized: ['materialized'],
  all: [],
}

const scopeOptions = [
  { value: 'actionable', label: '待转任务' },
  { value: 'observing', label: '观察中' },
  { value: 'materialized', label: '已转任务' },
  { value: 'all', label: '全部' },
] satisfies Array<{ value: ClueScope; label: string }>

const initialQuery: TodoQuery = {
  statuses: scopeStatuses.actionable,
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
  const { context, setSelection, setViewState } = usePageContext()
  const routeScope = context.view_state.view in scopeStatuses ? context.view_state.view as ClueScope : 'actionable'
  const routePage = Math.max(1, Number(context.view_state.page) || 1)
  const routePageSize = [20, 50, 100].includes(Number(context.view_state.page_size)) ? Number(context.view_state.page_size) : 20
  const routeActionType = context.view_state.action_type || undefined
  const routeLeaderOnly = context.view_state.leader_only === 'true'
  const [scope, setScope] = useState<ClueScope>(routeScope)
  const [query, setQuery] = useState<TodoQuery>({
    ...initialQuery,
    statuses: scopeStatuses[routeScope],
    actionType: routeActionType,
    leaderOnly: routeLeaderOnly,
    page: routePage,
    pageSize: routePageSize,
  })
  const [items, setItems] = useState<Todo[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [selected, setSelected] = useState<Todo>()
  const [drawerLoading, setDrawerLoading] = useState(false)
  const [savingStatusID, setSavingStatusID] = useState<number>()

  const routedTodoID = context.active_key === 'todos' && context.selection?.kind === 'todo'
    ? context.selection.id
    : null

  const updateRoute = useCallback((nextScope: ClueScope, nextQuery: TodoQuery) => {
    setViewState({
      view: nextScope,
      action_type: nextQuery.actionType,
      leader_only: nextQuery.leaderOnly || undefined,
      page: nextQuery.page,
      page_size: nextQuery.pageSize === 20 ? undefined : nextQuery.pageSize,
    })
  }, [setViewState])

  useEffect(() => {
    setScope((current) => current === routeScope ? current : routeScope)
    setQuery((current) => {
      if (
        current.statuses === scopeStatuses[routeScope]
        && current.actionType === routeActionType
        && current.leaderOnly === routeLeaderOnly
        && current.page === routePage
        && current.pageSize === routePageSize
      ) return current
      return {
        ...current,
        statuses: scopeStatuses[routeScope],
        actionType: routeActionType,
        leaderOnly: routeLeaderOnly,
        page: routePage,
        pageSize: routePageSize,
      }
    })
  }, [routeActionType, routeLeaderOnly, routePage, routePageSize, routeScope])

  useEffect(() => {
    if (routedTodoID === null) {
      setSelected(undefined)
      return
    }
    if (selected?.id === routedTodoID) return
    const controller = new AbortController()
    setDrawerLoading(true)
    getTodo(routedTodoID, controller.signal)
      .then(setSelected)
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) {
          setError(cause instanceof Error ? cause.message : String(cause))
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setDrawerLoading(false)
      })
    return () => controller.abort()
  }, [routedTodoID, selected?.id])

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

  const changeStatus = useCallback((todo: Todo, next: TodoStatus) => {
    setSavingStatusID(todo.id)
    setError(undefined)
    setTodoStatus(todo.id, next, '在线索列表手工调整')
      .then((updated) => {
        const remainsVisible = scopeStatuses[scope].length === 0 || scopeStatuses[scope].includes(updated.status)
        setItems((current) => remainsVisible
          ? current.map((item) => (item.id === updated.id ? updated : item))
          : current.filter((item) => item.id !== updated.id))
        if (!remainsVisible) setTotal((current) => Math.max(0, current - 1))
        setSelected((current) => (current?.id === updated.id ? updated : current))
      })
      .catch((cause: unknown) => setError(cause instanceof Error ? cause.message : String(cause)))
      .finally(() => setSavingStatusID(undefined))
  }, [scope])

  const columns = useMemo<TableColumnsType<Todo>>(
    () => [
      {
        title: '线索',
        dataIndex: 'title',
        key: 'title',
        width: 360,
        render: (_, todo) => (
          <div className="clue-title-cell">
            <Space size={6} wrap>
              <Text strong ellipsis={{ tooltip: todo.title }}>
                {todo.title}
              </Text>
              {todo.is_leader_assigned && <StatusBadge label="Leader 交办" color={leaderColor} />}
            </Space>
            <Paragraph
              type="secondary"
              ellipsis={{ rows: 2, tooltip: todo.description }}
              className="clue-summary"
            >
              {todo.description}
            </Paragraph>
            <Space size={8} wrap className="clue-meta-line">
              <Text type="secondary">{actionLabels[todo.action_type] || todo.action_type}</Text>
              {todo.open_questions?.length ? (
                <Text type="warning">{todo.open_questions.length} 项待补充</Text>
              ) : null}
            </Space>
          </div>
        ),
      },
      {
        title: '状态',
        dataIndex: 'status',
        width: 108,
        // 只有「没人在动手」的两个状态可以就地互换：按下不表，或重新生成 Task。
        // 其余状态由 Task 固化与执行流水线写入，列表里只读。
        render: (value: TodoStatus, todo) =>
          settableStatuses.includes(value) ? (
            <Select
              size="small"
              variant="borderless"
              style={{ width: '100%' }}
              value={value}
              loading={savingStatusID === todo.id}
              disabled={savingStatusID !== undefined}
              options={settableStatuses.map((status) => ({ value: status, label: statusMeta[status].label }))}
              onClick={(event) => event.stopPropagation()}
              onChange={(next) => changeStatus(todo, next)}
            />
          ) : (
            <StatusBadge label={statusMeta[value].label} color={statusMeta[value].color} />
          ),
      },
      {
        title: '关联',
        key: 'context',
        width: 180,
        ellipsis: true,
        render: (_, todo) => (
          <Tooltip title={`${todo.project?.name || '未关联项目'} · ${todo.group?.name || todo.group?.chat_id || '未知会话'}`}>
            <Space orientation="vertical" size={0}>
              <Text ellipsis style={{ fontSize: 13 }}>
                {todo.project?.name || '未关联项目'}
              </Text>
              <Text type="secondary" ellipsis style={{ fontSize: 12 }}>
                {todo.group?.name || todo.group?.chat_id || '未知会话'}
              </Text>
            </Space>
          </Tooltip>
        ),
      },
      {
        title: '时间',
        key: 'time',
        width: 130,
        render: (_, todo) => (
          <Space orientation="vertical" size={0}>
            {todo.due_at && <Text style={{ fontSize: 12 }}>截止 {formatDate(todo.due_at)}</Text>}
            <Text type="secondary" style={{ fontSize: 12 }}>更新 {formatDate(todo.last_evidence_at)}</Text>
          </Space>
        ),
      },
    ],
    [changeStatus, savingStatusID],
  )

  return (
    <>
      <PageHeader title="线索" subtitle="Jarvis 从会话中发现的行动信号，需要做事的会转成 Task" />
      <Card className="filter-card" variant="borderless">
        <div className="clue-scope-row">
          <Segmented<ClueScope>
            value={scope}
            disabled={savingStatusID !== undefined}
            options={scopeOptions}
            onChange={(nextScope) => {
              const nextQuery = { ...query, statuses: scopeStatuses[nextScope], page: 1 }
              setScope(nextScope)
              setQuery(nextQuery)
              updateRoute(nextScope, nextQuery)
            }}
          />
          <Text type="secondary" className="scope-help">
            {scope === 'actionable' ? '优先展示还没有进入执行流程的近期线索' : '按最近证据时间排序'}
          </Text>
        </div>
        <Flex gap={16} align="end" wrap className="clue-filter-row">
          <label className="filter-field">
            <Text type="secondary">行动类型</Text>
            <Select
              allowClear
              value={query.actionType}
              placeholder="全部类型"
              options={Object.entries(actionLabels).map(([value, label]) => ({ value, label }))}
              onChange={(actionType) => {
                const nextQuery = { ...query, actionType, page: 1 }
                setQuery(nextQuery)
                updateRoute(scope, nextQuery)
              }}
            />
          </label>
          <label className="switch-field">
            <Switch
              checked={query.leaderOnly}
              onChange={(leaderOnly) => {
                const nextQuery = { ...query, leaderOnly, page: 1 }
                setQuery(nextQuery)
                updateRoute(scope, nextQuery)
              }}
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
          title="线索加载失败"
          description={error}
          closable
          onClose={() => setError(undefined)}
        />
      )}

      <Card className="table-card" variant="borderless">
        <Table<Todo>
          rowKey="id"
          size="small"
          columns={columns}
          dataSource={items}
          loading={loading}
          scroll={{ x: 790 }}
          onRow={(todo) => ({
            onClick: () => openTodo(todo),
            onKeyDown: (event) => {
              if (event.key === 'Enter' || event.key === ' ') {
                event.preventDefault()
                openTodo(todo)
              }
            },
            tabIndex: 0,
            role: 'button',
            className: 'clickable-row',
          })}
          pagination={{
            current: query.page,
            pageSize: query.pageSize,
            total,
            showSizeChanger: true,
            pageSizeOptions: [20, 50, 100],
            onChange: (page, pageSize) => {
              const nextQuery = { ...query, page, pageSize }
              setQuery(nextQuery)
              updateRoute(scope, nextQuery)
            },
          }}
        />
      </Card>

      <Drawer
        title={selected?.title || '线索详情'}
        open={Boolean(selected)}
        loading={drawerLoading}
        size={640}
        onClose={closeTodo}
      >
        {selected && (
          <Space orientation="vertical" size={24} className="drawer-content">
            <Space wrap>
              <StatusBadge label={statusMeta[selected.status].label} color={statusMeta[selected.status].color} />
              <Tag>{actionLabels[selected.action_type] || selected.action_type}</Tag>
              {selected.is_leader_assigned && <StatusBadge label="Leader 交办" color={leaderColor} />}
            </Space>
            <section className="clue-detail-section">
              <Text type="secondary">这条线索说了什么</Text>
              <Paragraph>{selected.description}</Paragraph>
            </section>
            <Descriptions column={2} size="small">
              <Descriptions.Item label="项目">{selected.project?.name || '未关联'}</Descriptions.Item>
              <Descriptions.Item label="会话">{selected.group?.name || selected.group?.chat_id || '未知'}</Descriptions.Item>
              <Descriptions.Item label="承诺强度">{selected.commitment_strength}</Descriptions.Item>
              <Descriptions.Item label="截止时间">{formatDate(selected.due_at)}</Descriptions.Item>
              <Descriptions.Item label="版本">rev {selected.revision} / v{selected.version}</Descriptions.Item>
              <Descriptions.Item label="证据数">{selected.source_message_ids.length}</Descriptions.Item>
              <Descriptions.Item label="首次发现">{formatDate(selected.first_seen_at)}</Descriptions.Item>
              <Descriptions.Item label="最近更新">{formatDate(selected.last_evidence_at)}</Descriptions.Item>
            </Descriptions>
            <section className="clue-detail-section">
              <Text type="secondary">源消息原文</Text>
              <blockquote>{selected.source_quote}</blockquote>
            </section>
            <section className="clue-detail-section">
              <Text type="secondary">目标、背景与待补充</Text>
              <TodoContextPanel target={selected.target} context={selected.context} openQuestions={selected.open_questions} />
            </section>
            {selected.resolution && (
              <section className="clue-detail-section">
                <Text type="secondary">项目与代码定位</Text>
                <Descriptions column={1} size="small" className="clue-resolution">
                  <Descriptions.Item label="推断方式">{selected.resolution.method}</Descriptions.Item>
                  <Descriptions.Item label="代码位置">{selected.resolution.repo_path || '未定位'}</Descriptions.Item>
                  <Descriptions.Item label="依据">{selected.resolution.basis || '—'}</Descriptions.Item>
                </Descriptions>
              </section>
            )}
          </Space>
        )}
      </Drawer>
    </>
  )
}
