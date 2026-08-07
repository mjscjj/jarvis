import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, DatePicker, Empty, Flex, Segmented, Space, Spin, Table, Tag, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { generateDailyDigest, getCommitWorklog, getDailyDigests, getDigests, getDocumentWorklog, getProfile } from './api'
import PageHeader from './components/PageHeader'
import EmptyState from './components/EmptyState'
import MarkdownReport from './components/MarkdownReport'
import { usePageContext } from './pageContext'
import type { CommitMR, CommitWorklog, DailyDigest, DailyDigestScope, Digest, DocumentWorklog, ProfileView, WorkDoc } from './types'
import './styles/review-memory.css'

const { Text, Link } = Typography

const DAILY_DIGEST_POLL_MS = 5000
const DAILY_DIGEST_RETRY_MS = 10000

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function num(value: number) {
  return value > 0 ? value : <Text type="secondary">—</Text>
}

// 只显示时分（数据已是本地时区的 ISO 串）。
function timeOfDay(iso: string): string {
  if (!iso) return '—'
  const d = dayjs(iso)
  return d.isValid() ? d.format('HH:mm') : iso
}

const mrStatusMeta: Record<string, { color: string; label: string }> = {
  open: { color: 'processing', label: '进行中' },
  merged: { color: 'success', label: '已合入' },
  closed: { color: 'default', label: '已关闭' },
}

const dailySourceLabels: Record<string, string> = {
  jarvis_internal: 'Jarvis 内部事实',
  feishu_work: '飞书工作证据',
  engineering_execution: '工程执行证据',
  jarvis_messages: '消息',
  jarvis_todos: 'Todo',
  jarvis_tasks: 'Task',
  lark_documents: '文档',
  lark_calendar: '日历',
  lark_meetings: '会议',
  lark_minutes: '妙记',
  code_mrs: 'MR',
  git_commits: 'Commit',
  jarvis_group_messages: '已采集群消息',
  lark_group_messages: '飞书群消息',
  code_commits: '关联 Commit',
  other_materials: '其他材料',
}

// DocsTab —— 我在选定日期写/编辑的飞书文档 + 我当天收到的文档（消息里采集到的）。
function DocsTab({ date }: { date: Dayjs }) {
  const [data, setData] = useState<DocumentWorklog>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    getDocumentWorklog(date.format('YYYY-MM-DD'), controller.signal)
      .then((result) => setData(result))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [date])

  const renderDoc = (doc: WorkDoc, showFrom: boolean) => (
    <div key={`${doc.url}-${doc.time}`} style={{ padding: '8px 0', borderBottom: '1px solid #f0f0f0' }}>
      <Space size={8} align="start">
        <Tag>{doc.doc_type || '文档'}</Tag>
        <div>
          {doc.url ? <Link href={doc.url} target="_blank">{doc.title || doc.url}</Link> : <Text>{doc.title || '(无标题)'}</Text>}
          <div>
            <Text type="secondary" style={{ fontSize: 12 }}>
              {timeOfDay(doc.time)}
              {showFrom && doc.from_chat ? ` · 来自 ${doc.from_chat}` : ''}
              {showFrom && doc.from_who ? ` · ${doc.from_who}` : ''}
            </Text>
          </div>
        </div>
      </Space>
    </div>
  )

  return (
    <div>
      {error && <Alert type="error" showIcon title="文档加载失败" description={error} />}
      {loading ? (
        <div style={{ padding: '32px 0', textAlign: 'center' }}><Spin /></div>
      ) : (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <Card variant="borderless" title={`我写的文档（${data?.authored.length ?? 0}）`}>
            {(data?.authored.length ?? 0) === 0
              ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有我编辑的文档" />
              : data!.authored.map((doc) => renderDoc(doc, false))}
          </Card>
          <Card variant="borderless" title={`我收到的文档（${data?.received.length ?? 0}）`}>
            {(data?.received.length ?? 0) === 0
              ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有采集到的文档" />
              : data!.received.map((doc) => renderDoc(doc, true))}
          </Card>
        </Space>
      )}
    </div>
  )
}

// CodeTab —— 我在选定日期于各仓库更新的 MR，按仓库分组。
function CodeTab({ date }: { date: Dayjs }) {
  const [data, setData] = useState<CommitWorklog>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    getCommitWorklog(date.format('YYYY-MM-DD'), controller.signal)
      .then((result) => setData(result))
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [date])

  const mrColumns: TableColumnsType<CommitMR> = [
    {
      title: 'MR',
      dataIndex: 'title',
      render: (title: string, mr) => <Link href={mr.url} target="_blank">{title || mr.url}</Link>,
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (status: string) => {
        const meta = mrStatusMeta[status] ?? { color: 'default', label: status }
        return <Tag color={meta.color}>{meta.label}</Tag>
      },
    },
    { title: 'commit', dataIndex: 'commits_count', width: 80, render: num },
    { title: '变更行', dataIndex: 'changes_count', width: 80, render: num },
    { title: '目标分支', dataIndex: 'target_branch', width: 120, render: (b: string) => b || <Text type="secondary">—</Text> },
    { title: '更新', dataIndex: 'updated_at', width: 80, render: (t: string) => timeOfDay(t) },
  ]

  return (
    <div>
      {error && <Alert type="error" showIcon title="代码提交加载失败" description={error} />}
      {loading ? (
        <div style={{ padding: '32px 0', textAlign: 'center' }}><Spin /></div>
      ) : (data?.repos.length ?? 0) === 0 ? (
        <Card variant="borderless">
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有我更新的 MR" />
        </Card>
      ) : (
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          {data!.repos.map((repo) => (
            <Card key={repo.repo} variant="borderless" title={repo.repo}>
              <Table<CommitMR>
                rowKey="url"
                size="small"
                columns={mrColumns}
                dataSource={repo.mrs}
                pagination={false}
              />
            </Card>
          ))}
        </Space>
      )}
    </div>
  )
}

export default function Progress() {
  const { context, setViewState } = usePageContext()
  type ReviewView = 'summary' | 'docs' | 'code'
  const reviewView = (value: string | undefined): ReviewView => (
    value === 'docs' || value === 'code' ? value : 'summary'
  )
  const [activeView, setActiveView] = useState<ReviewView>(() => reviewView(context.view_state.view))
  const [data, setData] = useState<Digest>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [dailyDate, setDailyDate] = useState<Dayjs>(() => {
    const routeDate = dayjs(context.view_state.date)
    return routeDate.isValid() ? routeDate : dayjs()
  })
  const [dailyItems, setDailyItems] = useState<DailyDigest[]>([])
  const [profile, setProfile] = useState<ProfileView>()
  const [dailyLoading, setDailyLoading] = useState(false)
  const [dailyError, setDailyError] = useState<string>()
  const [dailyScopeTab, setDailyScopeTab] = useState<'person' | 'groups'>('person')
  const [generating, setGenerating] = useState<Set<string>>(new Set())
  const [dailyRefresh, setDailyRefresh] = useState(0)
  const loadedDailyDateRef = useRef<string | undefined>(undefined)
  const generatingDatesRef = useRef<Set<string>>(new Set())

  useEffect(() => {
    setActiveView(reviewView(context.view_state.view))
    const routeDate = dayjs(context.view_state.date)
    if (routeDate.isValid() && !routeDate.isSame(dailyDate, 'day')) setDailyDate(routeDate)
  }, [context.view_state.date, context.view_state.view, dailyDate])

  const selectView = (view: ReviewView) => {
    setActiveView(view)
    setViewState({ view, date: dailyDate.format('YYYY-MM-DD') })
  }

  const selectDate = (date: Dayjs) => {
    setDailyDate(date)
    setViewState({ view: activeView, date: date.format('YYYY-MM-DD') })
  }

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    getDigests(7, controller.signal)
      .then((result) => { setData(result); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [])

  const selectedDate = dailyDate.format('YYYY-MM-DD')
  const activeDailyDateRef = useRef(selectedDate)
  activeDailyDateRef.current = selectedDate

  useEffect(() => {
    const controller = new AbortController()
    let timer: number | undefined
    const scheduleLoad = (delay: number) => {
      timer = window.setTimeout(() => void load(false), delay)
    }
    const load = async (initial: boolean) => {
      if (initial) {
        setDailyLoading(true)
        setDailyItems([])
      }
      try {
        const [digests, currentProfile] = await Promise.all([
          getDailyDigests(selectedDate, controller.signal),
          profile ? Promise.resolve(profile) : getProfile(),
        ])
        if (controller.signal.aborted) return
        setDailyItems(digests.items)
        setProfile(currentProfile)
        setDailyError(undefined)
        const stillGenerating = digests.items.some((item) => item.status === 'generating')
        if (stillGenerating) {
          generatingDatesRef.current.add(selectedDate)
          scheduleLoad(DAILY_DIGEST_POLL_MS)
        } else {
          generatingDatesRef.current.delete(selectedDate)
        }
      } catch (cause) {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) {
          const retrying = generatingDatesRef.current.has(selectedDate)
          const detail = errorText(cause)
          setDailyError(retrying ? `${detail}；生成仍在进行，将在约 10 秒后重试` : detail)
          if (retrying) scheduleLoad(DAILY_DIGEST_RETRY_MS)
        }
      } finally {
        if (!controller.signal.aborted && initial) {
          loadedDailyDateRef.current = selectedDate
          setDailyLoading(false)
        }
      }
    }
    void load(loadedDailyDateRef.current !== selectedDate)
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [selectedDate, dailyRefresh]) // profile 故意不做依赖：首次加载后复用，避免重复请求。

  const digestFor = (scope: DailyDigestScope, scopeId: string) =>
    dailyItems.find((item) => item.scope === scope && item.scope_id === scopeId)

  const generate = async (scope: DailyDigestScope, scopeId: string) => {
    const requestDate = selectedDate
    const key = `${requestDate}:${scope}:${scopeId}`
    setGenerating((current) => new Set(current).add(key))
    setDailyError(undefined)
    try {
      await generateDailyDigest(scope, scopeId, requestDate)
      generatingDatesRef.current.add(requestDate)
      if (activeDailyDateRef.current !== requestDate) return
      setDailyItems((current) => {
        const existing = current.find((item) => item.scope === scope && item.scope_id === scopeId)
        if (existing) {
          return current.map((item) => item === existing ? {
            ...item,
            status: 'generating',
            trigger_type: 'manual',
            started_at: new Date().toISOString(),
            cutoff_at: null,
            error_detail: null,
          } : item)
        }
        return [...current, {
          id: 0, scope, scope_id: scopeId, digest_date: requestDate, summary: '', status: 'generating',
          trigger_type: 'manual', source_count: 0, source_coverage: {},
          engine: 'codex', error_detail: null,
          started_at: new Date().toISOString(), cutoff_at: null,
          generated_at: null, updated_at: new Date().toISOString(),
        }]
      })
      // 触发 effect 立即读取；发现 generating 后由 effect 约每 5 秒持续轮询。
      setDailyRefresh((value) => value + 1)
    } catch (cause) {
      if (activeDailyDateRef.current === requestDate) setDailyError(errorText(cause))
    } finally {
      setGenerating((current) => {
        const next = new Set(current)
        next.delete(key)
        return next
      })
    }
  }

  const digestCard = (title: string, scope: DailyDigestScope, scopeId: string) => {
    const item = digestFor(scope, scopeId)
    const isGenerating = item?.status === 'generating' || generating.has(`${selectedDate}:${scope}:${scopeId}`)
    const status = item?.status
    const statusTag = status === 'done'
      ? <Tag color="success">已生成</Tag>
      : status === 'failed'
        ? <Tag color="error">失败</Tag>
        : status === 'generating'
          ? <Tag color="processing">生成中</Tag>
          : <Tag>未生成</Tag>
    const buttonLabel = scope === 'person'
      ? status === 'failed' ? '手动重试' : item ? '手动重新生成' : '手动生成'
      : status === 'failed' ? '重试' : item ? '重新生成' : '生成'
    const coverage = Object.entries(item?.source_coverage ?? {})
    return (
      <Card
        key={`${selectedDate}:${scope}:${scopeId}`}
        variant="borderless"
        title={<Space>{title}{statusTag}</Space>}
        extra={<Button type={scope === 'person' ? 'primary' : 'default'} size="small" loading={isGenerating} disabled={isGenerating} onClick={() => generate(scope, scopeId)}>{buttonLabel}</Button>}
      >
        {status === 'failed' ? (
          <Alert type="error" showIcon title="生成失败" description={item?.error_detail || '未记录错误详情'} />
        ) : item?.summary ? (
          <>
            <MarkdownReport className="daily-digest-markdown" content={item.summary} />
            <Space orientation="vertical" size={6}>
              <Text type="secondary">
                {item.generated_at ? `生成于 ${dayjs(item.generated_at).format('YYYY-MM-DD HH:mm')}` : '尚未生成'}
                {item.cutoff_at ? ` · 数据截至 ${dayjs(item.cutoff_at).format('YYYY-MM-DD HH:mm')}` : ''}
                {` · ${item.source_count} 条证据 · ${item.trigger_type === 'schedule' ? '自动生成' : '手动生成'} · ${item.engine}`}
              </Text>
              {coverage.length > 0 && (
                <Space size={[4, 4]} wrap>
                  <Text type="secondary">数据来源：</Text>
                  {coverage.map(([source, sourceState]) => (
                    <Tooltip key={source} title={sourceState.note}>
                      <Tag color={
                        sourceState.status === 'error' || sourceState.status === 'unavailable'
                          ? 'error'
                          : sourceState.status === 'ok' || sourceState.status === 'complete'
                            ? 'success'
                            : sourceState.status === 'partial'
                              ? 'warning'
                              : 'default'
                      }>
                        {dailySourceLabels[source] ?? source}{' '}
                        {sourceState.status === 'error'
                          ? '失败'
                          : sourceState.status === 'unavailable'
                            ? '不可用'
                            : sourceState.status === 'partial'
                              ? `部分 ${sourceState.count}`
                              : sourceState.count}
                      </Tag>
                    </Tooltip>
                  ))}
                </Space>
              )}
            </Space>
          </>
        ) : isGenerating ? (
          <Space><Spin size="small" /><Text type="secondary">正在生成，页面会自动刷新</Text></Space>
        ) : (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这一天还没有总结" />
        )}
      </Card>
    )
  }

  const summaryContent = (
    <>
      <Card className="review-scope-card" variant="borderless">
        <Flex justify="space-between" align="center" gap={12} wrap>
          <div>
            <Text strong>{dailyDate.isSame(dayjs(), 'day') ? '今天的结果' : `${dailyDate.format('M 月 D 日')}的结果`}</Text>
            <div><Text type="secondary">先看个人工作结果，需要时再切换到关键群总结。</Text></div>
          </div>
          <Segmented
            value={dailyScopeTab}
            onChange={(value) => setDailyScopeTab(value as 'person' | 'groups')}
            options={[
              { value: 'person', label: '我的总结' },
              { value: 'groups', label: `群总结 ${data?.key_groups.length ?? 0}` },
            ]}
          />
        </Flex>
      </Card>
      {dailyError && <Alert type="error" showIcon title="每日总结加载失败" description={dailyError} />}
      <Spin spinning={dailyLoading}>
        {dailyScopeTab === 'person' ? (
          profile
            ? digestCard('我的工作回顾', 'person', profile.open_id)
            : <Card variant="borderless"><Spin size="small" /></Card>
        ) : (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            {(data?.key_groups ?? []).map((group) => digestCard(group.name || '未命名会话', 'group', String(group.group_id)))}
            {!loading && (data?.key_groups.length ?? 0) === 0 && (
              <Card variant="borderless">
                <EmptyState description="暂无关键会话" hint="可在「记忆 → 会话」中将重要会话标为关键群" />
              </Card>
            )}
          </Space>
        )}
      </Spin>
    </>
  )

  const activeContent = (() => {
    switch (activeView) {
      case 'summary':
        return summaryContent
      case 'docs':
        return <DocsTab date={dailyDate} />
      case 'code':
        return <CodeTab date={dailyDate} />
    }
  })()

  return (
    <div className="progress review-page">
      <PageHeader title="回顾" subtitle="从结果开始，回看一天里完成的工作、协作和产出">
        <Space size={8} className="review-date-control">
          <DatePicker
            value={dailyDate}
            onChange={(date) => selectDate(date ?? dayjs())}
            allowClear={false}
            disabledDate={(date) => date.isAfter(dayjs(), 'day')}
          />
          <Button onClick={() => selectDate(dayjs())} disabled={dailyDate.isSame(dayjs(), 'day')}>今天</Button>
        </Space>
      </PageHeader>

      {error && <Alert type="error" showIcon title="回顾加载失败" description={error} closable onClose={() => setError(undefined)} />}

      <Flex className="review-view-nav" gap={8} wrap>
        <div className="review-subnav">
          <Button type={activeView === 'summary' ? 'primary' : 'text'} onClick={() => selectView('summary')}>每日总结</Button>
          <Button type={activeView === 'docs' ? 'primary' : 'text'} onClick={() => selectView('docs')}>文档</Button>
          <Button type={activeView === 'code' ? 'primary' : 'text'} onClick={() => selectView('code')}>代码</Button>
        </div>
      </Flex>

      <div className="review-content">{activeContent}</div>
    </div>
  )
}
