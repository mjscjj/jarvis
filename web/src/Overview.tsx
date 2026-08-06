import { useEffect, useMemo, useState } from 'react'
import { Alert, Badge, Button, Card, Skeleton, Space, Tag, Typography } from 'antd'
import {
  ArrowRightOutlined,
  CheckCircleFilled,
  ClockCircleOutlined,
  ExclamationCircleFilled,
  ReloadOutlined,
} from '@ant-design/icons'
import dayjs from 'dayjs'
import {
  getDailyDigests,
  getDebugAgentProcesses,
  getDebugFailures,
  getDigests,
  getMorningBriefs,
  getOverview,
  listTasks,
} from './api'
import PageHeader from './components/PageHeader'
import MorningBriefPanel from './components/MorningBriefPanel'
import { usePageContext } from './pageContext'
import { taskStatusMeta } from './status'
import { proposalOf, strField } from './tasks/taskPresentation'
import type {
  AgentProcessSnapshot,
  DailyDigest,
  Digest,
  FailureEvent,
  MorningBrief,
  Overview as OverviewData,
  Task,
  TaskList,
  TaskStatus,
} from './types'
import './styles/today.css'

const { Text, Title } = Typography

const ATTENTION_STATUSES: TaskStatus[] = ['needs_human', 'awaiting_approval']
const ACTIVE_STATUSES: TaskStatus[] = ['pending', 'executing', 'waiting']
const RESULT_STATUSES: TaskStatus[] = ['done', 'failed']

const digestStatusMeta: Record<DailyDigest['status'], { color: string; label: string }> = {
  pending: { color: 'default', label: '待生成' },
  generating: { color: 'processing', label: '生成中' },
  done: { color: 'success', label: '已生成' },
  failed: { color: 'error', label: '生成失败' },
}

interface LoadIssue {
  label: string
  detail: string
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function isAbortError(cause: unknown): boolean {
  return cause instanceof DOMException && cause.name === 'AbortError'
}

function timeLabel(value: string | null | undefined): string {
  if (!value) return '时间未知'
  const parsed = dayjs(value)
  if (!parsed.isValid()) return value
  return parsed.format('YYYY-MM-DD') === dayjs().format('YYYY-MM-DD')
    ? `今天 ${parsed.format('HH:mm')}`
    : parsed.format('MM-DD HH:mm')
}

function weekdayLabel(): string {
  return ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六'][dayjs().day()]
}

function taskSupportingText(task: Task): string | null {
  const proposal = proposalOf(task)
  if (task.status === 'awaiting_approval') {
    return proposal?.needs_followup
      ?? proposal?.proposal.action
      ?? strField(task.execution_result, 'needs_followup')
      ?? strField(task.execution_result, 'action')
  }
  return task.summary
    ?? strField(task.execution_result, 'summary')
    ?? strField(task.execution_result, 'needs_followup')
}

function waitingWakeAt(task: Task): string | null {
  const waiting = task.execution_result?.waiting
  if (!waiting || typeof waiting !== 'object' || Array.isArray(waiting)) return null
  const wakeAt = (waiting as Record<string, unknown>).wake_at
  return typeof wakeAt === 'string' && wakeAt.trim() ? wakeAt : null
}

function TaskRow({ task, onClick, emphasis = false }: { task: Task; onClick: () => void; emphasis?: boolean }) {
  const supportingText = taskSupportingText(task)
  const status = taskStatusMeta[task.status]

  return (
    <button
      type="button"
      className={`today-task-row${emphasis ? ' today-task-row-emphasis' : ''}`}
      onClick={onClick}
    >
      <span className="today-task-status" style={{ background: status.color }} aria-hidden="true" />
      <span className="today-task-content">
        <span className="today-task-title">{task.title}</span>
        {supportingText && <span className="today-task-summary">{supportingText}</span>}
        <span className="today-task-meta">
          <span>{status.label}</span>
          {task.target && <><span aria-hidden="true">·</span><span>{task.target}</span></>}
          <span aria-hidden="true">·</span>
          <span>{timeLabel(task.updated_at)}</span>
        </span>
      </span>
      <ArrowRightOutlined className="today-task-arrow" aria-hidden="true" />
    </button>
  )
}

function EmptyPanel({ text }: { text: string }) {
  return (
    <div className="today-empty">
      <CheckCircleFilled />
      <span>{text}</span>
    </div>
  )
}

export default function Overview() {
  const { navigate, setSelection } = usePageContext()
  const todayDate = dayjs().format('YYYY-MM-DD')
  const [overview, setOverview] = useState<OverviewData>()
  const [digest, setDigest] = useState<Digest>()
  const [dailyItems, setDailyItems] = useState<DailyDigest[]>([])
  const [morningBriefs, setMorningBriefs] = useState<MorningBrief[]>()
  const [attention, setAttention] = useState<TaskList>()
  const [active, setActive] = useState<TaskList>()
  const [results, setResults] = useState<TaskList>()
  const [failures, setFailures] = useState<FailureEvent[]>([])
  const [agents, setAgents] = useState<AgentProcessSnapshot>()
  const [loading, setLoading] = useState(true)
  const [agentLoading, setAgentLoading] = useState(true)
  const [loadIssues, setLoadIssues] = useState<LoadIssue[]>([])
  const [agentIssue, setAgentIssue] = useState<LoadIssue>()
  const [refreshVersion, setRefreshVersion] = useState(0)

  useEffect(() => {
    const controller = new AbortController()

    async function loadToday() {
      setLoading(true)
      const settled = await Promise.allSettled([
        getOverview(controller.signal),
        getDigests(1, controller.signal),
        getDailyDigests(todayDate, controller.signal),
        getMorningBriefs(14, controller.signal),
        listTasks(ATTENTION_STATUSES, 1, 5, controller.signal),
        listTasks(ACTIVE_STATUSES, 1, 5, controller.signal),
        listTasks(RESULT_STATUSES, 1, 16, controller.signal),
        getDebugFailures(24, controller.signal),
      ])
      if (controller.signal.aborted) return

      const issues: LoadIssue[] = []
      const recordIssue = (label: string, reason: unknown) => {
        if (!isAbortError(reason)) issues.push({ label, detail: errorText(reason) })
      }
      const [overviewResult, digestResult, dailyResult, morningBriefResult, attentionResult, activeResult, resultResult, failureResult] = settled

      if (overviewResult.status === 'fulfilled') setOverview(overviewResult.value)
      else recordIssue('任务统计', overviewResult.reason)

      if (digestResult.status === 'fulfilled') setDigest(digestResult.value)
      else recordIssue('今日进度', digestResult.reason)

      if (dailyResult.status === 'fulfilled') setDailyItems(dailyResult.value.items)
      else recordIssue('总结状态', dailyResult.reason)

      if (morningBriefResult.status === 'fulfilled') setMorningBriefs(morningBriefResult.value.items)
      else {
        setMorningBriefs(undefined)
        recordIssue('晨间作战简报', morningBriefResult.reason)
      }

      if (attentionResult.status === 'fulfilled') setAttention(attentionResult.value)
      else recordIssue('需要我处理', attentionResult.reason)

      if (activeResult.status === 'fulfilled') setActive(activeResult.value)
      else recordIssue('正在推进', activeResult.reason)

      if (resultResult.status === 'fulfilled') setResults(resultResult.value)
      else recordIssue('今日结果', resultResult.reason)

      if (failureResult.status === 'fulfilled') setFailures(failureResult.value.items)
      else recordIssue('风险状态', failureResult.reason)

      setLoadIssues(issues)
      setLoading(false)
    }

    void loadToday()
    return () => controller.abort()
  }, [refreshVersion, todayDate])

  useEffect(() => {
    const controller = new AbortController()
    let stopped = false
    let inFlight = false
    let firstLoad = true

    async function loadAgents() {
      if (inFlight) return
      inFlight = true
      if (firstLoad) setAgentLoading(true)
      try {
        const snapshot = await getDebugAgentProcesses(controller.signal)
        if (stopped) return
        setAgents(snapshot)
        setAgentIssue(undefined)
      } catch (cause: unknown) {
        if (!stopped && !isAbortError(cause)) {
          setAgentIssue({ label: 'Jarvis 运行状态', detail: errorText(cause) })
        }
      } finally {
        inFlight = false
        if (!stopped && firstLoad) {
          firstLoad = false
          setAgentLoading(false)
        }
      }
    }

    void loadAgents()
    const timer = window.setInterval(() => void loadAgents(), 10_000)
    return () => {
      stopped = true
      controller.abort()
      window.clearInterval(timer)
    }
  }, [refreshVersion])

  const todayDigest = digest?.mine.find((item) => item.date === todayDate)
  const personDigest = dailyItems.find((item) => item.scope === 'person')
  const groupDigests = dailyItems.filter((item) => item.scope === 'group')
  const failedDigests = dailyItems.filter((item) => item.status === 'failed')
  const completedGroupDigests = groupDigests.filter((item) => item.status === 'done').length
  const todayResults = useMemo(
    () => (results?.items ?? []).filter((item) => dayjs(item.updated_at).format('YYYY-MM-DD') === todayDate).slice(0, 4),
    [results, todayDate],
  )
  const failedTodayTasks = todayResults.filter((item) => item.status === 'failed')
  const failedTodayTaskCount = todayDigest?.tasks_failed ?? failedTodayTasks.length
  const unresolvedFailures = failures.filter((item) => !item.recovered)
  const unresolvedFailureCount = unresolvedFailures.reduce((sum, item) => sum + Math.max(item.count, 1), 0)
  const activeAgents = (agents?.summary.codex_executing ?? 0) + (agents?.summary.trae_cli ?? 0)
  const nextWakeAt = (active?.items ?? [])
    .map(waitingWakeAt)
    .filter((value): value is string => Boolean(value) && dayjs(value).isValid())
    .sort((left, right) => dayjs(left).valueOf() - dayjs(right).valueOf())[0]
  const activeTaskCount = (active?.items ?? []).filter((task) => task.status !== 'waiting').length
  const waitingTaskCount = (active?.items ?? []).filter((task) => task.status === 'waiting').length
  const healthPending = loading || (agentLoading && !agents)
  const hasHealthConcern = unresolvedFailureCount > 0 || Boolean(agentIssue) || loadIssues.length > 0

  function openTask(task: Task) {
    navigate('tasks')
    setSelection({ kind: 'task', id: task.id, label: `Task #${task.id} ${task.title}` })
  }

  const issueList = agentIssue ? [...loadIssues, agentIssue] : loadIssues

  return (
    <div className="today-home">
      <PageHeader title="今日" subtitle={`${dayjs().format('M 月 D 日')} · ${weekdayLabel()}`}>
        <Space size={12}>
          <Badge
            status={healthPending ? 'processing' : hasHealthConcern ? 'warning' : 'success'}
            text={healthPending ? '正在同步' : hasHealthConcern ? '有事项需关注' : 'Jarvis 正常'}
          />
          <Button
            size="small"
            icon={<ReloadOutlined />}
            loading={loading || agentLoading}
            onClick={() => setRefreshVersion((value) => value + 1)}
          >
            刷新
          </Button>
        </Space>
      </PageHeader>

      {issueList.length > 0 && (
        <Alert
          type="warning"
          showIcon
          className="today-load-alert"
          title={`部分信息暂时无法显示：${issueList.map((item) => item.label).join('、')}`}
          description={(
            <details>
              <summary>查看技术详情</summary>
              {issueList.map((item) => <div key={item.label}>{item.label}：{item.detail}</div>)}
            </details>
          )}
          action={<Button type="link" size="small" onClick={() => navigate('debug')}>运行状态</Button>}
        />
      )}

      <section className="today-command" aria-label="今日任务态势">
        <div className="today-command-copy">
          <Text className="today-command-kicker">今日任务态势</Text>
          <Title level={2}>
            有 <strong>{attention?.total ?? '—'}</strong> 件事等你拍板，另有 <strong>{active?.total ?? '—'}</strong> 件任务在途
          </Title>
          <Text type="secondary">
            {nextWakeAt ? `下一次将在 ${timeLabel(nextWakeAt)} 回来继续处理` : activeAgents > 0 ? `${activeAgents} 个执行器正在工作` : '当前没有明确的下一次唤醒时间'}
          </Text>
        </div>
        <div className="today-handoff-rail" aria-label="任务接力轨">
          <span className={(attention?.total ?? 0) > 0 ? 'is-human' : ''}><strong>{attention?.total ?? '—'}</strong> 等你处理</span>
          <i aria-hidden="true" />
          <span className={activeTaskCount > 0 ? 'is-agent' : ''}><strong>{activeTaskCount}</strong> Jarvis 执行</span>
          <i aria-hidden="true" />
          <span className={waitingTaskCount > 0 ? 'is-waiting' : ''}><strong>{waitingTaskCount}</strong> 等待外部</span>
          <i aria-hidden="true" />
          <span className="is-done"><strong>{todayDigest?.tasks_done ?? '—'}</strong> 今日交付</span>
        </div>
      </section>

      <div className="today-layout">
        <Card className="today-panel today-attention-panel" variant="borderless">
          <div className="today-panel-heading">
            <div>
              <Text className="today-eyebrow">需要你决定</Text>
              <div className="today-heading-line">
                <Title level={2}>{attention ? attention.total : '—'}</Title>
                <Text>件待处理事项</Text>
              </div>
            </div>
            <Button type="link" onClick={() => navigate('tasks')}>查看全部 <ArrowRightOutlined /></Button>
          </div>

          {loading && !attention ? (
            <Skeleton active paragraph={{ rows: 3 }} title={false} />
          ) : attention && attention.items.length > 0 ? (
            <div className="today-task-list">
              {attention.items.map((task) => (
                <TaskRow key={task.id} task={task} emphasis onClick={() => openTask(task)} />
              ))}
            </div>
          ) : (
            <EmptyPanel text="当前没有需要你决定的事项" />
          )}
        </Card>

        <Card className="today-panel today-active-panel" variant="borderless">
          <div className="today-panel-heading today-panel-heading-compact">
            <div>
              <Text className="today-eyebrow">在途任务</Text>
              <Title level={4}>{active ? `${active.total} 件未闭环` : '正在读取'}</Title>
            </div>
            <div className="today-agent-state">
              <span className={activeAgents > 0 ? 'is-active' : undefined} />
              {agentLoading && !agents ? '检查中' : activeAgents > 0 ? `${activeAgents} 个执行器工作中` : '暂无执行器占用'}
            </div>
          </div>

          {loading && !active ? (
            <Skeleton active paragraph={{ rows: 3 }} title={false} />
          ) : active && active.items.length > 0 ? (
            <div className="today-task-list today-task-list-compact">
              {active.items.map((task) => <TaskRow key={task.id} task={task} onClick={() => openTask(task)} />)}
            </div>
          ) : (
            <EmptyPanel text="当前没有正在推进的任务" />
          )}
          {active && active.total > active.items.length && (
            <Button className="today-more-button" type="text" onClick={() => navigate('tasks')}>
              另有 {active.total - active.items.length} 件 <ArrowRightOutlined />
            </Button>
          )}
        </Card>

        <MorningBriefPanel briefs={morningBriefs} loading={loading} today={todayDate} />

        <Card className="today-panel today-results-panel" variant="borderless">
          <div className="today-panel-heading today-panel-heading-compact">
            <div>
              <Text className="today-eyebrow">今日结果</Text>
              <Title level={4}>做成了什么</Title>
            </div>
            <Button type="link" onClick={() => navigate('progress')}>查看回顾 <ArrowRightOutlined /></Button>
          </div>

          <div className="today-result-summary">
            <div className="today-result-count today-result-count-success">
              <CheckCircleFilled />
              <strong>{todayDigest ? todayDigest.tasks_done : '—'}</strong>
              <span>完成</span>
            </div>
            <div className={`today-result-count${failedTodayTaskCount > 0 ? ' today-result-count-error' : ''}`}>
              <ExclamationCircleFilled />
              <strong>{todayDigest ? todayDigest.tasks_failed : '—'}</strong>
              <span>失败</span>
            </div>
            <div className="today-digest-state">
              <span>个人总结</span>
              {personDigest
                ? <Tag color={digestStatusMeta[personDigest.status].color}>{digestStatusMeta[personDigest.status].label}</Tag>
                : <Tag>尚未生成</Tag>}
              <Text type="secondary">群总结 {completedGroupDigests}/{groupDigests.length}</Text>
            </div>
          </div>

          {todayResults.length > 0 ? (
            <div className="today-result-list">
              {todayResults.map((task) => (
                <button key={task.id} type="button" onClick={() => openTask(task)}>
                  <span className={task.status === 'failed' ? 'is-failed' : undefined}>
                    {task.status === 'failed' ? <ExclamationCircleFilled /> : <CheckCircleFilled />}
                  </span>
                  <span>
                    <strong>{task.title}</strong>
                    {taskSupportingText(task) && <small>{taskSupportingText(task)}</small>}
                  </span>
                  <ArrowRightOutlined />
                </button>
              ))}
            </div>
          ) : (
            <Text type="secondary" className="today-no-result">今天还没有产生可展示的任务结果</Text>
          )}
        </Card>

        <Card className="today-panel today-risk-panel" variant="borderless">
          <div className="today-panel-heading today-panel-heading-compact">
            <div>
              <Text className="today-eyebrow">发现与风险</Text>
              <Title level={4}>可能影响你的事项</Title>
            </div>
          </div>

          <div className="today-risk-list">
            {failedTodayTaskCount > 0 && (
              <button type="button" onClick={() => navigate('tasks')}>
                <span className="today-risk-icon"><ExclamationCircleFilled /></span>
                <span><strong>{failedTodayTaskCount} 项任务今天执行失败</strong><small>预期结果可能尚未交付，需要检查后续动作。</small></span>
                <ArrowRightOutlined />
              </button>
            )}
            {failedDigests.length > 0 && (
              <button type="button" onClick={() => navigate('progress')}>
                <span className="today-risk-icon"><ClockCircleOutlined /></span>
                <span><strong>{failedDigests.length} 项总结未生成成功</strong><small>今天的工作回顾可能不完整。</small></span>
                <ArrowRightOutlined />
              </button>
            )}
            {unresolvedFailureCount > 0 && (
              <button type="button" onClick={() => navigate('debug')}>
                <span className="today-risk-icon"><ExclamationCircleFilled /></span>
                <span><strong>后台能力有 {unresolvedFailureCount} 次异常尚未恢复</strong><small>任务采集、执行或总结可能受到影响。</small></span>
                <ArrowRightOutlined />
              </button>
            )}
            {agentIssue && (
              <button type="button" onClick={() => navigate('debug')}>
                <span className="today-risk-icon"><ClockCircleOutlined /></span>
                <span><strong>暂时无法确认 Jarvis 运行状态</strong><small>任务仍可查看，执行状态需要到运行页面确认。</small></span>
                <ArrowRightOutlined />
              </button>
            )}
            {failedTodayTaskCount === 0 && failedDigests.length === 0 && unresolvedFailureCount === 0 && !agentIssue && (
              <EmptyPanel text="暂未发现会影响你工作的风险" />
            )}
          </div>

          <div className="today-system-footnote">
            <Text type="secondary">
              全局 {overview ? overview.tasks.pending : '—'} 件任务在途 · {overview ? overview.todos.leader_open : '—'} 件 Leader 交办未闭环
            </Text>
            <Button type="link" size="small" onClick={() => navigate('todos')}>查看线索</Button>
          </div>
        </Card>
      </div>
    </div>
  )
}
