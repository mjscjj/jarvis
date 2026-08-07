import { useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  Alert,
  Button,
  Collapse,
  Descriptions,
  Empty,
  Modal,
  Space,
  Spin,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import {
  ApiOutlined,
  BulbOutlined,
  CalendarOutlined,
  CheckCircleOutlined,
  CloseOutlined,
  CommentOutlined,
  ExclamationCircleOutlined,
  FileTextOutlined,
  HistoryOutlined,
  LinkOutlined,
  MessageOutlined,
  PaperClipOutlined,
  PullRequestOutlined,
  SafetyOutlined,
  ToolOutlined,
  UndoOutlined,
} from '@ant-design/icons'
import type { Effect, ExecutionRun, RunEnrichment, Task, TaskEvent, TaskRunOutput } from '../types'
import { getTaskRunOutput } from '../api'
import EntityRelations from '../components/EntityRelations'
import StatusBadge from '../components/StatusBadge'
import { actionLabels, taskStatusMeta as statusMeta } from '../status'
import {
  failureKindOf,
  failureMeta,
  modelCloseReason,
  proposalOf,
  proposalArtifactLabel,
  structureProposalAction,
  strField,
  taskHandlerMeta,
  taskProjectName,
  taskSourceName,
} from './taskPresentation'

const { Link, Paragraph, Text, Title } = Typography

const taskEventLabels: Record<string, string> = {
  created: '任务已创建',
  execution_started: '开始执行',
  approval_requested: '等待审批',
  approval_granted: '已批准执行',
  approval_rejected: '已驳回',
  rerun_requested: '请求重跑',
  updated: '主动维护',
  reapply_started: '重新落地',
  human_input_requested: '等待我的回应',
  human_response_received: '已回复并继续',
  resumed: '恢复原 Session',
  supplemented: '我的补充',
  execution_succeeded: '执行成功',
  execution_failed: '执行失败',
  execution_observing: '查完，无需动手',
  execution_interrupted: '执行已打断',
  feishu_message_recalled: '撤回飞书消息',
  stale_failed: '执行超时',
  stale_requeued: '执行中断，重新排队',
  snapshot_imported: '导入当前状态',
  closed: '主动收口',
}

const actorLabels: Record<string, string> = {
  user: '我',
  m5: 'M5',
  proactive: '主动 Agent',
  scheduled_task: '定时恢复器',
  system: '系统',
  seed: '初始化',
  migration: '迁移',
}

// EffectRecall 把「撤回一条已发出的飞书消息」这个动作交给 effect 卡片：pending 是
// 正在撤回的 message_id（用于按钮 loading），run 触发撤回。真正的确认弹窗、接口调用
// 和错误展示都在页面层，卡片只负责发起。
interface EffectRecall {
  pending?: string
  run: (messageID: string) => void
}

interface TaskDetailModalProps {
  task?: Task
  runs: ExecutionRun[]
  events: TaskEvent[]
  runsLoading: boolean
  eventsLoading: boolean
  runsError?: string
  eventsError?: string
  executing: boolean
  approveSubmitting: boolean
  resumeSubmitting: boolean
  interrupting: boolean
  recallingMessageID?: string
  recallError?: string
  onRecallMessage: (task: Task, messageID: string) => void
  onClose: () => void
  onExecute: (task: Task) => void
  onApprove: (task: Task) => void
  onReject: (task: Task) => void
  onRerun: (task: Task) => void
  onResume: (task: Task) => void
  onInterrupt: (task: Task) => void
}

type HistoryItem =
  | { key: string; at: string; kind: 'event'; event: TaskEvent; run?: ExecutionRun }
  | { key: string; at: string; kind: 'supplement'; note: string; scope: string }
  | { key: string; at: string; kind: 'run'; run: ExecutionRun }

function formatDuration(ms: number | null): string {
  if (ms == null) return '—'
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function outputErrorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

type CodexOutputItem = Record<string, unknown> & {
  id?: string
  type?: string
  status?: string
}

interface CodexOutputEntry {
  key: string
  eventType: string
  item: CodexOutputItem
  rawEvents: Record<string, unknown>[]
}

interface ParsedCodexOutput {
  entries: CodexOutputEntry[]
  lifecycle: string[]
  invalidLines: string[]
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null
}

function printableValue(value: unknown): string {
  if (typeof value === 'string') return value
  if (value == null) return ''
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function enrichmentItems(value: unknown): RunEnrichment[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    const record = asRecord(item)
    if (!record || typeof record.kind !== 'string' || typeof record.label !== 'string' || !('content' in record)) {
      return []
    }
    return [{
      kind: record.kind,
      label: record.label,
      content: record.content,
    }]
  })
}

function parseCodexOutput(stdout: string): ParsedCodexOutput {
  const entries: CodexOutputEntry[] = []
  const byID = new Map<string, CodexOutputEntry>()
  const lifecycle: string[] = []
  const invalidLines: string[] = []

  for (const [index, line] of stdout.split('\n').entries()) {
    const trimmed = line.trim()
    if (!trimmed) continue
    let event: Record<string, unknown>
    try {
      const parsed = JSON.parse(trimmed)
      const record = asRecord(parsed)
      if (!record) throw new Error('event is not an object')
      event = record
    } catch {
      invalidLines.push(trimmed)
      continue
    }

    const eventType = printableValue(event.type) || 'unknown'
    const item = asRecord(event.item) as CodexOutputItem | null
    if (!item) {
      lifecycle.push(eventType)
      continue
    }

    const itemID = printableValue(item.id) || `line-${index}`
    const existing = byID.get(itemID)
    if (existing) {
      existing.eventType = eventType
      existing.item = { ...existing.item, ...item }
      existing.rawEvents.push(event)
      continue
    }
    const entry: CodexOutputEntry = {
      key: itemID,
      eventType,
      item,
      rawEvents: [event],
    }
    entries.push(entry)
    byID.set(itemID, entry)
  }

  return { entries, lifecycle, invalidLines }
}

function outputItemLabel(type: string): string {
  switch (type) {
    case 'agent_message': return 'Agent 消息'
    case 'command_execution': return '终端命令'
    case 'mcp_tool_call': return '工具调用'
    case 'web_search': return '网页搜索'
    case 'file_change': return '文件变更'
    case 'reasoning': return '思考'
    default: return type || '执行事件'
  }
}

function outputItemStatus(entry: CodexOutputEntry): { label: string; color?: string } {
  const status = printableValue(entry.item.status)
  if (entry.eventType === 'item.started' || status === 'in_progress') {
    return { label: '执行中', color: 'processing' }
  }
  if (status === 'failed' || status === 'error') return { label: '失败', color: 'error' }
  if (status === 'completed' || entry.eventType === 'item.completed') return { label: '完成', color: 'success' }
  return { label: status || '已记录' }
}

function itemInput(item: CodexOutputItem): string {
  return printableValue(
    item.command
    ?? item.arguments
    ?? item.input
    ?? item.query
    ?? item.request,
  )
}

function itemOutput(item: CodexOutputItem): string {
  return printableValue(
    item.aggregated_output
    ?? item.output
    ?? item.result
    ?? item.response,
  )
}

function outputToolName(item: CodexOutputItem): string {
  const type = printableValue(item.type)
  if (type === 'command_execution') return 'Shell'
  return printableValue(
    item.tool_name
    ?? item.name
    ?? item.tool
    ?? item.server,
  ) || outputItemLabel(type)
}

function outputItemText(item: CodexOutputItem): string {
  return printableValue(item.text ?? item.summary ?? item.content)
}

function oneLineSummary(value: string, maxLength = 120): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  if (!compact) return ''
  return compact.length > maxLength ? `${compact.slice(0, maxLength)}…` : compact
}

function contentMeta(value: string): string {
  if (!value.trim()) return '空'
  const lines = value.trimEnd().split('\n').length
  return lines > 1 ? `${lines} 行` : `${value.length} 字符`
}

function ToolCallDetails({
  input,
  result,
  running,
  failed,
}: {
  input: string
  result: string
  running: boolean
  failed: boolean
}) {
  return (
    <div className="task-output-tool-details">
      <details>
        <summary>
          <span>入参</span>
          <Text type="secondary">{contentMeta(input)}</Text>
        </summary>
        <pre className="task-output-code">{input || '无入参'}</pre>
      </details>
      <details className={failed ? 'task-output-result-failed' : ''} open={failed}>
        <summary>
          <span>结果</span>
          <Text type="secondary">{running ? '等待返回' : contentMeta(result)}</Text>
        </summary>
        <pre className="task-output-code">{result || (running ? '工具仍在执行…' : '无结果')}</pre>
      </details>
    </div>
  )
}

function AgentMessageOutput({ text }: { text: string }) {
  const parsed = (() => {
    try {
      return asRecord(JSON.parse(text))
    } catch {
      return null
    }
  })()
  const summary = parsed ? printableValue(parsed.summary) : text
  const enrichments = parsed ? enrichmentItems(parsed.enrichments) : []

  return (
    <div className="task-output-agent-message">
      <Paragraph>{summary || 'Agent 未提供消息正文。'}</Paragraph>
      {enrichments.length > 0 && (
        <details className="task-output-details">
          <summary>补充信息（{enrichments.length}）</summary>
          <div className="task-enrichment-list">
            {enrichments.map((item, index) => (
              <EnrichmentBlock key={`${item.label}-${index}`} item={item} />
            ))}
          </div>
        </details>
      )}
      {parsed && (
        <details className="task-output-details">
          <summary>查看完整消息数据</summary>
          <pre className="task-output-code">{JSON.stringify(parsed, null, 2)}</pre>
        </details>
      )}
    </div>
  )
}

function StructuredCodexOutput({ stdout }: { stdout: string }) {
  const parsed = useMemo(() => parseCodexOutput(stdout), [stdout])
  if (!stdout.trim()) return <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="尚未产生实时输出" />

  const toolCount = parsed.entries.filter((entry) => {
    const type = printableValue(entry.item.type)
    return type !== 'agent_message' && type !== 'reasoning'
  }).length
  return (
    <div className="task-output-structured">
      <Space size={6} wrap className="task-output-summary">
        <Tag>{parsed.entries.length} 个步骤</Tag>
        <Tag>{toolCount} 次工具调用</Tag>
        {parsed.invalidLines.length > 0 && <Tag color="warning">{parsed.invalidLines.length} 行待解析</Tag>}
      </Space>

      <div className="task-output-timeline">
        <div className="task-output-run-marker">
          <span />
          <Text type="secondary">Agent 开始执行</Text>
        </div>
        {parsed.entries.map((entry) => {
          const type = printableValue(entry.item.type)
          const status = outputItemStatus(entry)
          const input = itemInput(entry.item)
          const result = itemOutput(entry.item)
          const messageText = outputItemText(entry.item)
          const exitCode = entry.item.exit_code
          const isThought = type === 'agent_message' || type === 'reasoning'
          const failed = status.label === '失败' || (exitCode != null && exitCode !== 0)
          const toolName = outputToolName(entry.item)
          return (
            <article
              key={entry.key}
              className={[
                'task-output-step',
                isThought ? 'task-output-step-thought' : 'task-output-step-tool',
                failed ? 'task-output-step-failed' : '',
              ].filter(Boolean).join(' ')}
            >
              <span className="task-output-step-dot">
                {isThought ? <BulbOutlined /> : <ToolOutlined />}
              </span>
              <header>
                <div>
                  <Text strong>{isThought ? 'Agent 思考' : toolName}</Text>
                  {!isThought && input && <Text type="secondary">{oneLineSummary(input)}</Text>}
                </div>
                <Space size={4}>
                  <Tag variant="filled" color={status.color}>{status.label}</Tag>
                  {exitCode != null && (
                    <Tag variant="filled" color={exitCode === 0 ? 'success' : 'error'}>
                      code {printableValue(exitCode)}
                    </Tag>
                  )}
                </Space>
              </header>

              {isThought ? (
                <AgentMessageOutput text={messageText} />
              ) : (
                <ToolCallDetails
                  input={input}
                  result={result}
                  running={status.label === '执行中'}
                  failed={failed}
                />
              )}

              <details className="task-output-details task-output-raw-event">
                <summary>原始事件</summary>
                <pre className="task-output-code">{JSON.stringify(entry.rawEvents, null, 2)}</pre>
              </details>
            </article>
          )
        })}
        <div className="task-output-run-marker task-output-run-finish">
          <span />
          <Text type="secondary">
            {parsed.lifecycle.includes('turn.completed') ? 'Agent 执行完成' : '等待后续步骤'}
          </Text>
        </div>
      </div>

      {(parsed.lifecycle.length > 0 || parsed.invalidLines.length > 0) && (
        <details className="task-output-details task-output-stream-meta">
          <summary>流元数据与未解析内容</summary>
          <pre className="task-output-code">
            {[
              parsed.lifecycle.length > 0 ? `生命周期：${parsed.lifecycle.join(' → ')}` : '',
              parsed.invalidLines.length > 0 ? `未解析：\n${parsed.invalidLines.join('\n')}` : '',
            ].filter(Boolean).join('\n\n')}
          </pre>
        </details>
      )}
    </div>
  )
}

function TaskRunOutputPanel({
  taskID,
  active,
}: {
  taskID: number
  active: boolean
}) {
  const [output, setOutput] = useState<TaskRunOutput>()
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()

  useEffect(() => {
    if (!active) {
      setOutput(undefined)
      setError(undefined)
      return
    }
    const controller = new AbortController()
    let timer: number | undefined
    let first = true
    const refresh = async () => {
      if (first) setLoading(true)
      try {
        const result = await getTaskRunOutput(taskID, controller.signal)
        setOutput(result)
        setError(undefined)
        first = false
        if (result.running && !controller.signal.aborted) {
          timer = window.setTimeout(refresh, 1000)
        }
      } catch (cause: unknown) {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) {
          setError(outputErrorText(cause))
        }
      } finally {
        if (!controller.signal.aborted) setLoading(false)
      }
    }
    void refresh()
    return () => {
      controller.abort()
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [active, taskID])

  const outputTabs = [
    {
      key: 'stdout',
      label: '过程',
      children: <StructuredCodexOutput stdout={output?.stdout || ''} />,
    },
    {
      key: 'prompt',
      label: '完整输入',
      children: <pre className="task-live-output">{output?.prompt || '尚未记录输入。'}</pre>,
    },
    {
      key: 'stderr',
      label: '错误输出',
      children: <pre className="task-live-output">{output?.stderr || '没有 stderr 输出。'}</pre>,
    },
    {
      key: 'raw',
      label: '原始输出',
      children: <pre className="task-live-output">{output?.stdout || '尚未产生 stdout 输出。'}</pre>,
    },
  ]

  if (!active) return null

  return (
    <section className="task-process-panel">
      {loading && !output ? (
        <div className="task-detail-loading"><Spin /></div>
      ) : error ? (
        <Alert type="error" showIcon title="执行过程加载失败" description={error} />
      ) : !output?.available ? (
        <Empty description={output?.running ? '执行器正在准备输入，输出文件尚未建立。' : '这个 Task 暂无执行输出记录。'} />
      ) : (
        <>
          <Space wrap className="task-output-meta">
            <Tag color={output.running ? 'processing' : 'default'}>{output.running ? '实时刷新中' : '执行已结束'}</Tag>
            <Tag>{output.stage || 'execute'}</Tag>
            <Text type="secondary">{output.run_key}</Text>
            {output.updated_at && <Text type="secondary">更新于 {formatTime(output.updated_at)}</Text>}
          </Space>
          <Tabs items={outputTabs} />
        </>
      )}
    </section>
  )
}

function formatShortTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return `${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`
}

function runStatusColor(status: string): string {
  if (status === 'succeeded') return 'var(--color-success)'
  if (status === 'failed') return 'var(--color-error)'
  if (status === 'waiting' || status === 'needs_human') return 'var(--color-warning)'
  if (status === 'running') return 'var(--color-info)'
  return 'var(--color-text-tertiary)'
}

function taskEventColor(event: TaskEvent): string {
  if (event.actor_type === 'user' || event.event_type === 'supplemented') return 'var(--color-warning)'
  if (event.to_status === 'done') return 'var(--color-success)'
  if (event.to_status === 'failed') return 'var(--color-error)'
  if (event.to_status === 'awaiting_approval') return 'var(--color-warning)'
  if (event.to_status === 'waiting' || event.to_status === 'needs_human') return 'var(--color-warning)'
  if (event.to_status === 'executing') return 'var(--color-info)'
  return 'var(--color-text-tertiary)'
}

function enrichmentKindLabel(kind: string): string {
  switch (kind) {
    case 'context': return '正文'
    case 'doc_link': return '相关文档'
    case 'code_link': return '相关代码'
    case 'commit_digest': return 'Commit 摘要'
    default: return kind || '补充'
  }
}

function EnrichmentContent({ content, kind }: { content: unknown; kind: string }) {
  if (content === null) {
    return <pre className="task-enrichment-json">null</pre>
  }
  if (typeof content !== 'string') {
    return <pre className="task-enrichment-json">{printableValue(content)}</pre>
  }

  const isLink = kind === 'doc_link'
    || kind === 'code_link'
    || kind === 'link'
    || /^https?:\/\//.test(content.trim())
  if (!isLink) {
    return <Paragraph className="task-enrichment-detail">{content}</Paragraph>
  }

  const paths = content.split(/[；;\n]+/).map((path) => path.trim()).filter(Boolean)
  return (
    <Space orientation="vertical" size={2} className="task-enrichment-links">
      {paths.map((path, index) => /^https?:\/\//.test(path)
        ? <Link key={index} href={path} target="_blank" rel="noreferrer">{path}</Link>
        : <Text key={index} className="mono" copyable>{path}</Text>)}
    </Space>
  )
}

function EnrichmentBlock({ item }: { item: RunEnrichment }) {
  const label = item.label?.trim() || enrichmentKindLabel(item.kind)
  return (
    <div className="task-enrichment">
      <Text strong>{label}</Text>
      <EnrichmentContent content={item.content} kind={item.kind} />
    </div>
  )
}

// KNOWN_EFFECT_FIELDS are the fields the "对外产出" card renders in dedicated
// slots. Every OTHER field on an effect is unknown/pass-through and is listed
// verbatim as key:value — the card never drops information it did not expect.
const KNOWN_EFFECT_FIELDS = new Set(['kind', 'title', 'url', 'target', 'preview', 'recalled_at'])

// effectItems parses execution_result.effects (or run.effects) into a loose
// Effect[]. It only requires that each entry be an object; a missing/blank kind
// still renders under the generic fallback card, because effects are an open,
// display-only payload — we show whatever the agent declared.
function effectItems(value: unknown): Effect[] {
  if (!Array.isArray(value)) return []
  return value.flatMap((item) => {
    const record = asRecord(item)
    if (!record) return []
    const kind = typeof record.kind === 'string' ? record.kind : ''
    return [{ ...record, kind } as Effect]
  })
}

// effectKindMeta maps a known kind to an icon + human label. Unknown kinds fall
// back to a generic API/plug icon and show the raw kind string, so a brand-new
// kind the agent invents still renders as a proper card instead of being hidden.
function effectKindMeta(kind: string): { icon: ReactNode; label: string } {
  switch (kind) {
    case 'feishu_message':
    case 'message':
    case 'reply_message':
    case 'summary_post':
      return { icon: <MessageOutlined />, label: '飞书消息' }
    case 'feishu_doc':
    case 'doc':
    case 'doc_write':
    case 'document':
      return { icon: <FileTextOutlined />, label: '飞书文档' }
    case 'calendar_event':
    case 'meeting':
    case 'schedule_meeting':
      return { icon: <CalendarOutlined />, label: '日程 / 会议' }
    case 'merge_request':
    case 'mr':
    case 'pull_request':
      return { icon: <PullRequestOutlined />, label: 'Merge Request' }
    case 'permission_request':
    case 'permission':
      return { icon: <SafetyOutlined />, label: '权限申请' }
    case 'file':
    case 'attachment':
      return { icon: <PaperClipOutlined />, label: '文件' }
    default:
      return { icon: <ApiOutlined />, label: kind || '对外动作' }
  }
}

// EffectExtraFields renders every field that is NOT one of KNOWN_EFFECT_FIELDS
// as a friendly key:value row. This is the deliberate "尽力展示未知字段" behavior:
// whatever extra keys the agent attached (message_id, doc_token, chat_name, …)
// stay visible rather than being silently discarded.
function EffectExtraFields({ effect }: { effect: Effect }) {
  const extras = Object.entries(effect).filter(([key]) => !KNOWN_EFFECT_FIELDS.has(key))
  if (extras.length === 0) return null
  return (
    <div className="task-effect-extra">
      {extras.map(([key, value]) => {
        const text = printableValue(value)
        const isLink = typeof value === 'string' && /^https?:\/\//.test(value.trim())
        return (
          <div className="task-effect-extra-row" key={key}>
            <Text type="secondary" className="task-effect-extra-key">{key}</Text>
            {isLink
              ? <Link href={(value as string).trim()} target="_blank" rel="noreferrer">{text}</Link>
              : <Text className="task-effect-extra-value">{text}</Text>}
          </div>
        )
      })}
    </div>
  )
}

// recallableMessageID returns the Feishu message id the agent declared on this
// effect, which is the only handle a recall can use. Effects without one (docs,
// meetings, MRs, or a message the agent never reported an id for) are not
// recallable.
function recallableMessageID(effect: Effect): string {
  const id = typeof effect.message_id === 'string' ? effect.message_id.trim() : ''
  return id.startsWith('om_') ? id : ''
}

// EffectCard renders one declared side effect. Known kinds get a themed icon +
// label; unknown kinds get the generic fallback card. url becomes a clickable
// link (new tab), preview is shown as a quoted block, and any extra fields are
// listed via EffectExtraFields — nothing the agent declared is dropped. A sent
// Feishu message additionally gets a 撤回 button, or the 已撤回 mark once recalled.
function EffectCard({ effect, recall }: { effect: Effect; recall: EffectRecall }) {
  const meta = effectKindMeta(effect.kind)
  const url = typeof effect.url === 'string' ? effect.url.trim() : ''
  const target = typeof effect.target === 'string' ? effect.target.trim() : ''
  const preview = typeof effect.preview === 'string' ? effect.preview.trim() : ''
  const title = typeof effect.title === 'string' && effect.title.trim()
    ? effect.title.trim()
    : meta.label
  const messageID = recallableMessageID(effect)
  const recalledAt = typeof effect.recalled_at === 'string' ? effect.recalled_at.trim() : ''
  return (
    <div className={`task-effect-card${recalledAt ? ' task-effect-recalled' : ''}`}>
      <div className="task-effect-head">
        <span className="task-effect-icon">{meta.icon}</span>
        <div className="task-effect-headings">
          <Text strong className="task-effect-title">{title}</Text>
          <Tag className="task-effect-kind">{meta.label}</Tag>
        </div>
        {recalledAt ? (
          <Tag icon={<UndoOutlined />}>已撤回 · {formatTime(recalledAt)}</Tag>
        ) : messageID ? (
          <Button
            size="small"
            danger
            icon={<UndoOutlined />}
            loading={recall.pending === messageID}
            onClick={() => recall.run(messageID)}
          >
            撤回
          </Button>
        ) : null}
      </div>
      {target && (
        <div className="task-effect-target">
          <Text type="secondary">对象</Text>
          <Text>{target}</Text>
        </div>
      )}
      {url && (
        <div className="task-effect-link">
          <LinkOutlined />
          <Link href={url} target="_blank" rel="noreferrer">{url}</Link>
        </div>
      )}
      {preview && <div className="task-effect-preview">{preview}</div>}
      <EffectExtraFields effect={effect} />
    </div>
  )
}

// EffectsCard is the "对外产出" surface: one card per declared side effect.
// It renders nothing when there are no effects (老任务无 effects 就不显示该卡片).
function EffectsCard({ effects, recall }: { effects: Effect[]; recall: EffectRecall }) {
  if (effects.length === 0) return null
  return (
    <div className="task-primary-card task-effects-card">
      <div className="task-section-kicker">对外产出（{effects.length}）</div>
      <div className="task-effect-list">
        {effects.map((effect, index) => <EffectCard key={index} effect={effect} recall={recall} />)}
      </div>
    </div>
  )
}

function RunDetails({ run, latest, recall }: { run: ExecutionRun; latest: boolean; recall: EffectRecall }) {
  const enrichments = run.output?.enrichments ?? []
  const runEffects = effectItems(run.effects ?? run.output?.effects)
  const followup = run.output?.needs_followup?.trim()
  return (
    <details className="task-run-details" open={latest}>
      <summary>
        <span className="task-run-summary-main">
          <span className="task-history-dot" style={{ background: runStatusColor(run.status) }} />
          Run #{run.id} · {run.status}
        </span>
        <Text type="secondary">{formatDuration(run.duration_ms)}</Text>
      </summary>
      <div className="task-run-body">
        <Space size={8} wrap>
          <Tag>{actionLabels[run.action_type] || run.action_type}</Tag>
          <Text type="secondary">沙箱 {run.sandbox}</Text>
          {run.codex_session_id && <Text type="secondary">session {run.codex_session_id.slice(0, 12)}…</Text>}
          {run.repo_path && (
            <Text type="secondary" className="mono">
              {run.repo_path}
            </Text>
          )}
        </Space>
        {run.summary && <Paragraph className="task-readable-text">{run.summary}</Paragraph>}
        {enrichments.length > 0 && (
          <div className="task-enrichment-list">
            {enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
          </div>
        )}
        {runEffects.length > 0 && (
          <div className="task-effect-list task-run-effect-list">
            {runEffects.map((effect, index) => <EffectCard key={index} effect={effect} recall={recall} />)}
          </div>
        )}
        {followup && <Alert type="info" showIcon title="待你拍板 / 后续" description={followup} />}
        {run.error_detail && <Alert type="error" showIcon title="执行错误" description={<Text className="mono">{run.error_detail}</Text>} />}
        {run.output && Object.keys(run.output).length > 0 && (
          <details className="task-raw-details">
            <summary>Codex 原始输出</summary>
            <pre className="inline-json">{JSON.stringify(run.output, null, 2)}</pre>
          </details>
        )}
      </div>
    </details>
  )
}

function InlineCodeText({ text }: { text: string }) {
  return <>{text.split(/(`[^`]+`)/g).filter(Boolean).map((part, index) => (
    part.startsWith('`') && part.endsWith('`')
      ? <code key={index}>{part.slice(1, -1)}</code>
      : <span key={index}>{part}</span>
  ))}</>
}

function ProposalContent({ task, actions }: { task: Task; actions: ReactNode }) {
  const result = proposalOf(task)
  if (!result) return null
  const { proposal } = result
  const currentProgress = task.summary?.trim()
  const structuredAction = structureProposalAction(proposal.action)
  const evidenceCount = result.enrichments?.length ?? 0
  return (
    <div className="task-decision-card">
      <div className="task-decision-heading task-decision-heading-actions-only">
        <Space wrap>{actions}</Space>
      </div>

      <section className="task-decision-plan">
        <div className="task-decision-section-title">批准后会做什么</div>
        <Text className="task-decision-plan-intro">
          <InlineCodeText text={structuredAction.introduction || proposal.action} />
        </Text>
        {structuredAction.steps.length > 0 && (
          <details className="task-plan-details">
            <summary>
              <span>查看完整实施步骤</span>
              <Tag>{structuredAction.steps.length} 步</Tag>
            </summary>
            <ol>
              {structuredAction.steps.map((step, index) => (
                <li key={index}>
                  <span>{index + 1}</span>
                  <div><InlineCodeText text={step} /></div>
                </li>
              ))}
            </ol>
          </details>
        )}
      </section>

      <div className="task-decision-scope">
        <div>
          <Text type="secondary">操作范围</Text>
          <Text><InlineCodeText text={proposal.target} /></Text>
        </div>
        <div>
          <Text type="secondary">{proposalArtifactLabel(task)}</Text>
          <Text><InlineCodeText text={proposal.artifact} /></Text>
        </div>
      </div>

      {(result.summary || evidenceCount > 0) && (
        <section className="task-decision-evidence">
          <div className="task-decision-evidence-heading">
            <span>为什么这样建议</span>
            <Text type="secondary">调查结论{evidenceCount > 0 ? ` · ${evidenceCount} 条依据` : ''}</Text>
          </div>
          <div className="task-decision-evidence-body">
            {result.summary && <Paragraph className="task-readable-text">{result.summary}</Paragraph>}
            {result.enrichments && result.enrichments.length > 0 && (
              <div className="task-enrichment-list">
                {result.enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
              </div>
            )}
          </div>
        </section>
      )}

      {currentProgress && (
        <section className="task-decision-progress">
          <div className="task-decision-section-title">当前进展</div>
          <Paragraph className="task-readable-text">{currentProgress}</Paragraph>
        </section>
      )}
    </div>
  )
}

function ResultContent({ task, actions }: { task: Task; actions: ReactNode }) {
  const result = task.execution_result
  const summary = task.summary?.trim() || strField(result, 'summary')
  const error = strField(result, 'error')
  const rejectReason = strField(result, 'reject_reason')
  const followup = strField(result, 'needs_followup')
  const enrichments = enrichmentItems(result?.enrichments)
  const stateCopy = taskStateCopy(task)
  const closedByModel = task.resolution?.actor_type === 'proactive'
    && task.resolution.event_type === 'closed'
  const closeReason = modelCloseReason(task)

  const sectionTitle = (() => {
    if (task.status === 'pending') return '下一步'
    if (task.status === 'executing') return '正在推进'
    if (task.status === 'waiting') return '等待中'
    if (task.status === 'needs_human') return '需要你回复'
    if (closedByModel) return '模型关闭原因'
    if (task.status === 'done') return '完成结果'
    if (task.status === 'observing') return '调查结论'
    return '异常原因'
  })()

  return (
    <div className="task-primary-card">
      <div className="task-section-kicker">{sectionTitle}</div>
      {task.status === 'failed' && (
        <Alert
          type={failureKindOf(task) === 'rejected' || failureKindOf(task) === 'manual' || failureKindOf(task) === 'interrupted' ? 'warning' : 'error'}
          showIcon
          title={failureMeta[failureKindOf(task) || 'unknown'].label}
          description={rejectReason || error || summary || '任务没有记录失败详情。'}
        />
      )}
      {closedByModel && (
        <Alert
          type="info"
          showIcon
          title="主动 Agent 的判断"
          description={closeReason || '数据异常：这次模型关闭没有记录理由。'}
        />
      )}
      {task.status !== 'failed' && !closedByModel && (
        <Paragraph className="task-readable-text task-primary-summary">{stateCopy.current}</Paragraph>
      )}
      {task.status === 'needs_human' ? (
        <Alert type="warning" showIcon title="Agent 的问题" description={followup || stateCopy.next} />
      ) : (
        <Text type="secondary"><strong>接下来：</strong>{stateCopy.next}</Text>
      )}
      {enrichments.length > 0 && (
        <div className="task-enrichment-list">
          {enrichments.map((item, index) => <EnrichmentBlock key={index} item={item} />)}
        </div>
      )}
      {followup && task.status !== 'needs_human' && task.status !== 'done' && (
        <Alert type="info" showIcon title="后续事项" description={followup} />
      )}
      <Space className="task-decision-actions" wrap>{actions}</Space>
    </div>
  )
}

function taskStateCopy(task: Task): { current: string; next: string } {
  const result = task.execution_result
  const summary = task.summary?.trim() || strField(result, 'summary')
  const followup = strField(result, 'needs_followup')
  const error = strField(result, 'error')
  const rejectReason = strField(result, 'reject_reason')
  if (task.resolution?.actor_type === 'proactive' && task.resolution.event_type === 'closed') {
    return {
      current: modelCloseReason(task) || '数据异常：这次模型关闭没有记录理由。',
      next: '当前任务已由主动 Agent 停止追踪；如果判断有误，可以重跑任务。',
    }
  }
  if (task.status === 'awaiting_approval') {
    return {
      current: summary || '已生成完整产出物，尚未执行外部写入。',
      next: followup || '请审阅产出物。批准后，Jarvis 将执行写入并验证结果。',
    }
  }
  if (task.status === 'done') {
    return {
      current: summary || '任务已完成。',
      next: followup || '当前任务不需要继续操作。',
    }
  }
  if (task.status === 'observing') {
    return {
      current: summary || task.summary || '调查已经完成，当前没有需要执行的动作。',
      next: followup || '无需继续处理；后续出现新变化时会形成新的工作事项。',
    }
  }
  if (task.status === 'failed') {
    const kind = failureKindOf(task)
    return {
      current: rejectReason || error || summary || '任务执行失败。',
      next: kind === 'rejected'
        ? '可重跑任务，重新生成审批方案。'
        : kind === 'manual'
          ? '这是你手动标记的失败；需要时可以重跑任务。'
          : '检查失败原因后重跑任务。',
    }
  }
  if (task.status === 'executing') {
    return {
      current: 'Jarvis 正在执行任务。',
      next: '可以等待执行完成；如需停止，可使用“打断执行”。',
    }
  }
  if (task.status === 'waiting') {
    const waiting = result?.waiting && typeof result.waiting === 'object'
      ? result.waiting as Record<string, unknown>
      : null
    return {
      current: summary || String(waiting?.reason || '任务正在等待外部条件。'),
      next: waiting?.wake_at
        ? `将在 ${String(waiting.wake_at)} 自动恢复同一个执行会话。`
        : '已预约自动恢复同一个执行会话。',
    }
  }
  if (task.status === 'needs_human') {
    return {
      current: summary || 'Jarvis 已暂停当前执行会话。',
      next: followup || '回复后将继续同一个执行会话，不会重跑任务。',
    }
  }
  return {
    current: task.target || task.title,
    next: '开始执行后，Jarvis 将使用完整任务上下文完成工作。',
  }
}

function ProgressStrip({
  events,
  onSelect,
}: {
  events: TaskEvent[]
  onSelect: (event: TaskEvent) => void
}) {
  const ordered = useMemo(
    () => [...events].sort((a, b) => new Date(b.occurred_at).getTime() - new Date(a.occurred_at).getTime()),
    [events],
  )
  const visible = ordered.slice(0, 5)
  return (
    <section className="task-progress-strip">
      <div className="task-progress-header">
        <Text strong>任务进展（共 {events.length} 条）</Text>
        {ordered.length > visible.length && (
          <Button type="link" size="small" onClick={() => onSelect(ordered[0])}>
            查看全部
          </Button>
        )}
      </div>
      {visible.length === 0 ? (
        <Text type="secondary">暂无任务进展</Text>
      ) : (
        <div className="task-progress-items">
          {visible.map((event, index) => (
            <button key={event.id} className="task-progress-item" onClick={() => onSelect(event)}>
              <span className="task-progress-node">
                <span className="task-progress-dot" style={{ background: taskEventColor(event) }} />
                {index < visible.length - 1 && <span className="task-progress-line" />}
              </span>
              <span className="task-progress-copy">
                <strong>{taskEventLabels[event.event_type] || event.event_type}</strong>
                <small>{formatShortTime(event.occurred_at)} · {actorLabels[event.actor_type] || event.actor_type}</small>
              </span>
            </button>
          ))}
          {ordered.length > visible.length && (
            <div className="task-progress-more">+{ordered.length - visible.length}</div>
          )}
        </div>
      )}
    </section>
  )
}

function supplementScope(noteAt: string, events: TaskEvent[]): string {
  const timestamp = new Date(noteAt).getTime()
  const related = events.find((event) => {
    if (event.event_type !== 'approval_granted' && event.event_type !== 'rerun_requested') return false
    return Math.abs(new Date(event.occurred_at).getTime() - timestamp) <= 5000
  })
  if (related?.event_type === 'approval_granted') return '审批批注'
  if (related?.event_type === 'rerun_requested') return '重跑指示'
  return '任务级补充'
}

function buildHistory(task: Task, events: TaskEvent[], runs: ExecutionRun[]): HistoryItem[] {
  const runByID = new Map(runs.map((run) => [run.id, run]))
  const referencedRuns = new Set<number>()
  const items: HistoryItem[] = []

  for (const event of events) {
    if (event.event_type === 'supplemented') continue
    const run = event.run_id ? runByID.get(event.run_id) : undefined
    if (run) referencedRuns.add(run.id)
    items.push({ key: `event-${event.id}`, at: event.occurred_at, kind: 'event', event, run })
  }
  for (const [index, supplement] of (task.execution_supplements ?? []).entries()) {
    items.push({
      key: `supplement-${index}-${supplement.at}`,
      at: supplement.at,
      kind: 'supplement',
      note: supplement.note,
      scope: supplementScope(supplement.at, events),
    })
  }
  for (const run of runs) {
    if (!referencedRuns.has(run.id)) {
      items.push({ key: `run-${run.id}`, at: run.started_at, kind: 'run', run })
    }
  }
  return items.sort((a, b) => new Date(b.at).getTime() - new Date(a.at).getTime())
}

function TaskHistory({
  task,
  events,
  runs,
  loading,
  eventsError,
  runsError,
  recall,
}: {
  task: Task
  events: TaskEvent[]
  runs: ExecutionRun[]
  loading: boolean
  eventsError?: string
  runsError?: string
  recall: EffectRecall
}) {
  const history = useMemo(() => buildHistory(task, events, runs), [task, events, runs])
  const latestRunID = runs[0]?.id
  if (loading) return <div className="task-detail-loading"><Spin /></div>
  return (
    <div className="task-history">
      {eventsError && <Alert type="error" showIcon title="任务进展加载失败" description={eventsError} />}
      {runsError && <Alert type="error" showIcon title="执行历史加载失败" description={runsError} />}
      {history.length === 0 ? (
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无任务历史" />
      ) : history.map((item) => {
        if (item.kind === 'supplement') {
          return (
            <article key={item.key} id={`task-history-${item.key}`} className="task-history-item task-history-user-note">
              <div className="task-history-marker"><CommentOutlined /></div>
              <div className="task-history-content">
                <div className="task-history-heading">
                  <Space size={8}><Text strong>我的{item.scope}</Text><Tag color="gold">{item.scope}</Tag></Space>
                  <Text type="secondary">{formatTime(item.at)}</Text>
                </div>
                <blockquote>{item.note}</blockquote>
              </div>
            </article>
          )
        }
        if (item.kind === 'run') {
          return (
            <article key={item.key} id={`task-history-${item.key}`} className="task-history-item">
              <div className="task-history-marker"><HistoryOutlined /></div>
              <div className="task-history-content">
                <div className="task-history-heading">
                  <Text strong>执行记录</Text>
                  <Text type="secondary">{formatTime(item.at)}</Text>
                </div>
                <RunDetails run={item.run} latest={item.run.id === latestRunID} recall={recall} />
              </div>
            </article>
          )
        }
        const { event, run } = item
        const userEvent = event.actor_type === 'user'
        return (
          <article
            key={item.key}
            id={`task-history-event-${event.id}`}
            className={`task-history-item ${userEvent ? 'task-history-user-event' : ''}`}
          >
            <div className="task-history-marker" style={{ color: taskEventColor(event) }}>
              {event.to_status === 'failed' ? <ExclamationCircleOutlined /> : <CheckCircleOutlined />}
            </div>
            <div className="task-history-content">
              <div className="task-history-heading">
                <Space size={8}>
                  <Text strong>{taskEventLabels[event.event_type] || event.event_type}</Text>
                  {userEvent && <Tag color="gold">我的操作</Tag>}
                </Space>
                <Text type="secondary">{formatTime(event.occurred_at)}</Text>
              </div>
              <Text type="secondary">
                {actorLabels[event.actor_type] || event.actor_type} · v{event.task_version}
                {event.from_status ? ` · ${event.from_status} → ${event.to_status}` : ` · ${event.to_status}`}
              </Text>
              {run && <RunDetails run={run} latest={run.id === latestRunID} recall={recall} />}
            </div>
          </article>
        )
      })}
    </div>
  )
}

function objectField(value: Record<string, unknown>, key: string): Record<string, unknown> | null {
  const field = value[key]
  return field && typeof field === 'object' && !Array.isArray(field)
    ? field as Record<string, unknown>
    : null
}

function stringValue(value: unknown): string | null {
  return typeof value === 'string' && value.trim() ? value : null
}

function TaskMeta({ task }: { task: Task }) {
  const group = objectField(task.background, 'group')
  const project = objectField(task.background, 'project')
  const assigner = objectField(task.background, 'assigner')
  const handler = taskHandlerMeta(task)
  return (
    <aside className="task-meta-card">
      <div className="task-section-kicker">任务信息</div>
      <Descriptions size="small" column={1} colon={false}>
        <Descriptions.Item label="交办人">{stringValue(assigner?.name) || '—'}</Descriptions.Item>
        <Descriptions.Item label="来源会话">{stringValue(group?.name) || '—'}</Descriptions.Item>
        <Descriptions.Item label="所属项目">{stringValue(project?.name) || (task.project_id != null ? `#${task.project_id}` : '未关联')}</Descriptions.Item>
        <Descriptions.Item label="来源">{task.source_type}{task.source_id != null ? ` #${task.source_id}` : ''}</Descriptions.Item>
        <Descriptions.Item label="最终处理">{handler?.label || '尚未收口'}</Descriptions.Item>
        <Descriptions.Item label="Todo">{task.todo_id != null ? `#${task.todo_id}` : '—'}</Descriptions.Item>
        <Descriptions.Item label="Task">#{task.id}</Descriptions.Item>
        <Descriptions.Item label="版本">v{task.version}</Descriptions.Item>
      </Descriptions>
    </aside>
  )
}

function ContextPanel({ task }: { task: Task }) {
  const conversationValue = task.background.conversation
  const messagesValue = task.background.messages
  const conversation = Array.isArray(conversationValue)
    ? conversationValue
    : Array.isArray(messagesValue) ? messagesValue : []
  const memories = Array.isArray(task.background.memories) ? task.background.memories : []
  return (
    <div className="task-readable-panel">
      <section>
        <Title level={5}>原始会话</Title>
        {conversation.length === 0 ? (
          <Text type="secondary">没有记录原始会话。</Text>
        ) : (
          <div className="task-conversation">
            {conversation.map((item, index) => {
              const record = item && typeof item === 'object' ? item as Record<string, unknown> : {}
              return (
                <div key={index} className="task-conversation-item">
                  <div>
                    <Text strong>{stringValue(record.sender_name) || '未知发送人'}</Text>
                    {typeof record.create_time === 'number' && (
                      <Text type="secondary">{new Date(record.create_time * 1000).toLocaleString()}</Text>
                    )}
                  </div>
                  <Paragraph>{stringValue(record.content) || '—'}</Paragraph>
                </div>
              )
            })}
          </div>
        )}
      </section>
      {memories.length > 0 && (
        <section>
          <Title level={5}>相关记忆</Title>
          <div className="task-memory-list">
            {memories.map((item, index) => {
              const record = item && typeof item === 'object' ? item as Record<string, unknown> : {}
              return <Paragraph key={index}>{stringValue(record.memory) || '—'}</Paragraph>
            })}
          </div>
        </section>
      )}
      <EntityRelations entityType="task" entityId={task.id} />
    </div>
  )
}

export default function TaskDetailModal({
  task,
  runs,
  events,
  runsLoading,
  eventsLoading,
  runsError,
  eventsError,
  executing,
  approveSubmitting,
  resumeSubmitting,
  interrupting,
  recallingMessageID,
  recallError,
  onRecallMessage,
  onClose,
  onExecute,
  onApprove,
  onReject,
  onRerun,
  onResume,
  onInterrupt,
}: TaskDetailModalProps) {
  const [activeTab, setActiveTab] = useState('history')
  const [contextOpen, setContextOpen] = useState(false)
  useEffect(() => {
    setActiveTab('history')
    setContextOpen(false)
  }, [task?.id])
  if (!task) return null

  const failure = failureKindOf(task)
  const handler = taskHandlerMeta(task)
  const recall: EffectRecall = {
    pending: recallingMessageID,
    run: (messageID: string) => onRecallMessage(task, messageID),
  }
  const jumpToHistory = (event: TaskEvent) => {
    setActiveTab('history')
    window.setTimeout(() => {
      document.getElementById(`task-history-event-${event.id}`)?.scrollIntoView({
        behavior: 'smooth',
        block: 'center',
      })
    }, 0)
  }

  const actions = (() => {
    if (task.status === 'pending') {
      return <Button type="primary" loading={executing} onClick={() => onExecute(task)}>开始执行</Button>
    }
    if (task.status === 'awaiting_approval') {
      return <>
        <Button type="primary" loading={approveSubmitting} onClick={() => onApprove(task)}>批准方案</Button>
        <Button danger onClick={() => onReject(task)}>驳回</Button>
      </>
    }
    if (task.status === 'done' || task.status === 'failed' || task.status === 'observing') {
      return <Button onClick={() => onRerun(task)}>重跑</Button>
    }
    if (task.status === 'waiting') {
      return <Text type="secondary">到达唤醒时间后会自动继续</Text>
    }
    if (task.status === 'needs_human') {
      return <Button type="primary" loading={resumeSubmitting} onClick={() => onResume(task)}>回复并继续</Button>
    }
    return <>
      <Button danger loading={interrupting} onClick={() => onInterrupt(task)}>打断执行</Button>
    </>
  })()

  return (<>
      <Modal
        open
        footer={null}
        closable={false}
        centered
        width={1180}
        mask={{ closable: true }}
        onCancel={onClose}
        className="task-detail-modal"
        destroyOnHidden
      >
        <div className="task-detail-shell">
        <header className="task-detail-header">
          <div className="task-detail-title">
            <Space size={10} wrap>
              <StatusBadge label={statusMeta[task.status].label} color={statusMeta[task.status].color} />
              {failure && <Tag color={failureMeta[failure].color}>{failureMeta[failure].label}</Tag>}
              {handler && <Tag color={handler.color} title={handler.detail}>{handler.label}</Tag>}
              <Title level={3}>{task.title}</Title>
            </Space>
            <Text type="secondary">
              {taskProjectName(task)}
              {' · '}{taskSourceName(task)}
              {' · '}{actionLabels[task.action_type] || task.action_type}
              {' · '}Task #{task.id}
              {' · '}更新于 {formatTime(task.updated_at)}
            </Text>
          </div>
          <div className="task-detail-header-actions">
            <Button size="small" icon={<FileTextOutlined />} onClick={() => setContextOpen(true)}>上下文依据</Button>
            <Button type="text" icon={<CloseOutlined />} aria-label="关闭" onClick={onClose} />
          </div>
        </header>

        <div className="task-detail-scroll">
          {recallError && <Alert type="error" showIcon title="撤回飞书消息失败" description={recallError} />}

          <div className="task-detail-main-grid task-detail-main-single">
            <section aria-label="任务结论与产出">
              {proposalOf(task)
                ? <ProposalContent task={task} actions={actions} />
                : <ResultContent task={task} actions={actions} />}
              <EffectsCard effects={effectItems(task.execution_result?.effects)} recall={recall} />
            </section>
          </div>

          {eventsLoading ? (
            <section className="task-progress-strip"><Spin size="small" /></section>
          ) : eventsError ? (
            <Alert type="error" showIcon title="任务进展加载失败" description={eventsError} />
          ) : (
            <ProgressStrip events={events} onSelect={jumpToHistory} />
          )}

          <Collapse
            className="task-secondary-meta"
            ghost
            items={[{ key: 'meta', label: '任务信息', children: <TaskMeta task={task} /> }]}
          />

          <Tabs
            className="task-detail-tabs"
            activeKey={activeTab}
            onChange={setActiveTab}
            items={[
              {
                key: 'history',
                label: '任务历史',
                children: (
                  <TaskHistory
                    task={task}
                    events={events}
                    runs={runs}
                    loading={eventsLoading || runsLoading}
                    eventsError={eventsError}
                    runsError={runsError}
                    recall={recall}
                  />
                ),
              },
              {
                key: 'process',
                label: <span><HistoryOutlined /> 执行过程</span>,
                children: <TaskRunOutputPanel taskID={task.id} active={activeTab === 'process'} />,
              },
            ]}
          />

        </div>
        </div>
      </Modal>
      <Modal
        title="上下文依据"
        open={contextOpen}
        footer={null}
        width={760}
        zIndex={1300}
        destroyOnHidden
        className="task-context-modal"
        onCancel={() => setContextOpen(false)}
      >
        <ContextPanel task={task} />
      </Modal>
    </>)
}
