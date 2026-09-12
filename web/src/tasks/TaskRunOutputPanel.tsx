import { useEffect, useMemo, useState } from 'react'
import { Alert, Empty, Space, Spin, Tabs, Tag, Typography } from 'antd'
import { BulbOutlined, ToolOutlined } from '@ant-design/icons'
import type { TaskRunOutput } from '../types'
import { getTaskRunOutput } from '../api'
import { EnrichmentBlock } from './TaskEnrichments'
import { asRecord, enrichmentItems, errorText, formatTime, printableValue } from './taskValues'

const { Paragraph, Text } = Typography

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

export default function TaskRunOutputPanel({
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
          setError(errorText(cause))
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
