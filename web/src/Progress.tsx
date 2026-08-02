import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Card, DatePicker, Empty, Segmented, Space, Spin, Table, Tabs, Tag, Tooltip, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import dayjs from 'dayjs'
import type { Dayjs } from 'dayjs'
import { generateDailyDigest, getCommitWorklog, getDailyDigests, getDigests, getDocumentWorklog, getProfile } from './api'
import PageHeader from './components/PageHeader'
import EmptyState from './components/EmptyState'
import MarkdownReport from './components/MarkdownReport'
import type { CommitMR, CommitWorklog, DailyDigest, DailyDigestScope, Digest, DocumentWorklog, GroupProgress, MyDay, ProfileView, WorkDoc } from './types'

const { Text, Link } = Typography

const DAILY_DIGEST_POLL_MS = 5000
const DAILY_DIGEST_RETRY_MS = 10000
const DAILY_DIGEST_DATE_TAB_COUNT = 7

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function dailyDigestDates(selected: Dayjs): Dayjs[] {
  const selectedDay = selected.startOf('day')
  const recent = Array.from(
    { length: DAILY_DIGEST_DATE_TAB_COUNT },
    (_, index) => dayjs().startOf('day').subtract(index, 'day'),
  )
  return recent.some((date) => date.isSame(selectedDay, 'day'))
    ? recent
    : [selectedDay, ...recent]
}

function dailyDigestDateLabel(date: Dayjs): string {
  if (date.isSame(dayjs(), 'day')) return `今天 ${date.format('MM-DD')}`
  return date.isSame(dayjs(), 'year') ? date.format('MM-DD') : date.format('YYYY-MM-DD')
}

// A day row shows a dash when nothing happened so quiet days read as quiet.
function num(value: number) {
  return value > 0 ? value : <Text type="secondary">—</Text>
}

const myColumns: TableColumnsType<MyDay> = [
  { title: '日期', dataIndex: 'date', width: 120 },
  { title: '新增交办 Todo', dataIndex: 'todos_created', width: 130, render: num },
  { title: '生成任务', dataIndex: 'tasks_created', width: 130, render: num },
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

// DayPicker 是两个工作日志 Tab 共用的「选一天」控件，默认今天，不可选未来。
function DayPicker({ value, onChange }: { value: Dayjs; onChange: (d: Dayjs) => void }) {
  return (
    <Card className="filter-card" variant="borderless">
      <Space size={8}>
        <Text type="secondary">日期</Text>
        <DatePicker
          value={value}
          onChange={(d) => onChange(d ?? dayjs())}
          allowClear={false}
          disabledDate={(d) => d.isAfter(dayjs(), 'day')}
        />
        <Button size="small" onClick={() => onChange(dayjs())} disabled={value.isSame(dayjs(), 'day')}>今天</Button>
      </Space>
    </Card>
  )
}

// DocsTab —— 我在选定日期写/编辑的飞书文档 + 我当天收到的文档（消息里采集到的）。
function DocsTab() {
  const [date, setDate] = useState<Dayjs>(dayjs())
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
      <DayPicker value={date} onChange={setDate} />
      {error && <Alert type="error" showIcon style={{ marginTop: 12 }} message="文档加载失败" description={error} />}
      {loading ? (
        <div style={{ padding: '32px 0', textAlign: 'center' }}><Spin /></div>
      ) : (
        <Space direction="vertical" size={16} style={{ width: '100%', marginTop: 12 }}>
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
function CodeTab() {
  const [date, setDate] = useState<Dayjs>(dayjs())
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
      <DayPicker value={date} onChange={setDate} />
      {error && <Alert type="error" showIcon style={{ marginTop: 12 }} message="代码提交加载失败" description={error} />}
      {loading ? (
        <div style={{ padding: '32px 0', textAlign: 'center' }}><Spin /></div>
      ) : (data?.repos.length ?? 0) === 0 ? (
        <Card variant="borderless" style={{ marginTop: 12 }}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这天没有我更新的 MR" />
        </Card>
      ) : (
        <Space direction="vertical" size={16} style={{ width: '100%', marginTop: 12 }}>
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
  const [days, setDays] = useState(7)
  const [data, setData] = useState<Digest>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [dailyDate, setDailyDate] = useState<Dayjs>(dayjs())
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
    const controller = new AbortController()
    setLoading(true)
    getDigests(days, controller.signal)
      .then((result) => { setData(result); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [days])

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
          <Alert type="error" showIcon message="生成失败" description={item?.error_detail || '未记录错误详情'} />
        ) : item?.summary ? (
          <>
            <MarkdownReport className="daily-digest-markdown" content={item.summary} />
            <Space direction="vertical" size={6}>
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

  const dailyDateItems = dailyDigestDates(dailyDate).map((date) => {
    const dateKey = date.format('YYYY-MM-DD')
    return {
      key: dateKey,
      label: dailyDigestDateLabel(date),
      children: dateKey === selectedDate ? (
        <>
          {dailyError && <Alert type="error" showIcon message="每日总结加载失败" description={dailyError} />}
          <Spin spinning={dailyLoading}>
            <Tabs
              activeKey={dailyScopeTab}
              onChange={(key) => setDailyScopeTab(key as 'person' | 'groups')}
              items={[
                {
                  key: 'person',
                  label: '个人总结',
                  children: (
                    <Space direction="vertical" size={16} style={{ width: '100%' }}>
                      {profile
                        ? digestCard('个人总结', 'person', profile.open_id)
                        : <Card variant="borderless"><Spin size="small" /></Card>}
                    </Space>
                  ),
                },
                {
                  key: 'groups',
                  label: `群总结（${data?.key_groups.length ?? 0}）`,
                  children: (
                    <Space direction="vertical" size={16} style={{ width: '100%' }}>
                      {(data?.key_groups ?? []).map((group) => digestCard(group.name || group.chat_id, 'group', String(group.group_id)))}
                      {!loading && (data?.key_groups.length ?? 0) === 0 && (
                        <Card variant="borderless">
                          <EmptyState description="暂无标记为核心群的会话" hint="可在「背景 → 会话背景」里标记 is_key_group" />
                        </Card>
                      )}
                    </Space>
                  ),
                },
              ]}
            />
          </Spin>
        </>
      ) : null,
    }
  })

  return (
    <div className="progress">
      <PageHeader title="进度" subtitle="按自然日查看我的推进与关键群进展" />

      {error && <Alert type="error" showIcon message="进度加载失败" description={error} closable onClose={() => setError(undefined)} />}

      <Tabs
        defaultActiveKey="daily"
        items={[
          {
            key: 'daily',
            label: '每日总结',
            children: (
              <>
                <Tabs
                  activeKey={selectedDate}
                  onChange={(date) => setDailyDate(dayjs(date))}
                  tabBarExtraContent={(
                    <Space size={8}>
                      <Text type="secondary">其他日期</Text>
                      <DatePicker
                        size="small"
                        value={dailyDate}
                        onChange={(date) => setDailyDate(date ?? dayjs())}
                        allowClear={false}
                        disabledDate={(date) => date.isAfter(dayjs(), 'day')}
                      />
                    </Space>
                  )}
                  items={dailyDateItems}
                />
              </>
            ),
          },
          {
            key: 'mine',
            label: '数量趋势',
            children: (
              <>
                <Card className="filter-card" variant="borderless">
                  <Space size={8}>
                    <Text type="secondary">时间窗口</Text>
                    <Segmented value={days} onChange={(value) => setDays(value as number)} options={[{ label: '近 7 天', value: 7 }, { label: '近 14 天', value: 14 }, { label: '近 30 天', value: 30 }]} />
                  </Space>
                </Card>
                <Card variant="borderless" style={{ marginTop: 12 }}>
                  <Table<MyDay> rowKey="date" size="small" columns={myColumns} dataSource={data?.mine ?? []} loading={loading} pagination={false} />
                </Card>
              </>
            ),
          },
          {
            key: 'groups',
            label: '群进度',
            children:
              !loading && (data?.key_groups.length ?? 0) === 0 ? (
                <Card variant="borderless">
                  <EmptyState description="暂无标记为核心群的会话" hint="可在「背景 → 会话背景」里标记 is_key_group" />
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
          {
            key: 'docs',
            label: '今天的文档',
            children: <DocsTab />,
          },
          {
            key: 'code',
            label: '项目代码',
            children: <CodeTab />,
          },
        ]}
      />
    </div>
  )
}
