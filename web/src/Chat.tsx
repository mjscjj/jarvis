import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { CloseOutlined, SendOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Typography } from 'antd'
import type { TextAreaRef } from 'antd/es/input/TextArea'
import { usePageContext } from './pageContext'
import type { ChatDeltaEvent, ChatErrorEvent, ChatRequest, ChatThreadEvent } from './types'
import './styles/chat.css'

const { Text } = Typography

interface ChatMessage {
  role: 'user' | 'assistant'
  text: string
}

const PAGE_LABELS: Record<string, string> = {
  today: '今日',
  overview: '今日',
  workbench: '任务',
  tasks: '任务',
  review: '回顾',
  progress: '回顾',
  memory: '世界',
  background: '世界',
  automation: '自动化',
  'scheduled-tasks': '自动化',
  clues: '线索',
  todos: '线索',
  management: '系统设置',
  settings: '系统设置',
  debug: '运行诊断',
  'system-tasks': '系统任务',
}

const SELECTION_LABELS: Record<string, string> = {
  task: '任务',
  todo: '线索',
  project: '项目',
  person: '成员',
  group: '群组',
  resource: '资料',
}

const PAGE_SUGGESTIONS: Record<string, string[]> = {
  today: ['我现在最需要关注什么？', '帮我排一下今天的优先级', '有哪些事项正在等我决定？'],
  workbench: ['哪些任务最需要我处理？', '帮我梳理当前的阻塞', '检查进行中的任务是否偏离目标'],
  review: ['总结今天真正完成的事', '哪些承诺还没有闭环？', '帮我找出值得复盘的问题'],
  memory: ['Jarvis 目前是怎么理解我的工作的？', '检查项目背景有没有过时信息', '帮我找到某个项目的关键上下文'],
  automation: ['哪些自动化即将运行？', '检查自动化之间是否有冲突', '帮我设计一个新的自动化'],
  clues: ['最近出现了哪些重要线索？', '哪些线索还在等待更多证据？', '帮我解释线索到任务的转换'],
  system: ['检查 Jarvis 当前的关键配置', '有哪些系统异常会影响任务？', '帮我定位最近的运行问题'],
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function pageLabel(activeKey: string): string {
  return PAGE_LABELS[activeKey] ?? '当前页面'
}

function pageGroup(activeKey: string): string {
  if (['management', 'settings', 'debug', 'system-tasks'].includes(activeKey)) return 'system'
  const label = pageLabel(activeKey)
  return Object.keys(PAGE_SUGGESTIONS).find((key) => pageLabel(key) === label) ?? 'today'
}

// parseSSEBlock turns one `event:\ndata:` block into {event, data}. SSE allows
// multiple data: lines per event; we join them with \n per spec.
function parseSSEBlock(block: string): { event: string; data: string } {
  let event = 'message'
  const dataLines: string[] = []
  for (const rawLine of block.split('\n')) {
    const line = rawLine.replace(/\r$/, '')
    if (line.startsWith('event:')) event = line.slice(6).trim()
    else if (line.startsWith('data:')) dataLines.push(line.slice(5).replace(/^ /, ''))
  }
  return { event, data: dataLines.join('\n') }
}

function isAbortError(cause: unknown): boolean {
  return (cause instanceof DOMException && cause.name === 'AbortError')
    || (cause instanceof Error && cause.name === 'AbortError')
}

export default function Chat({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { context } = usePageContext()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [error, setError] = useState<string>()
  const threadId = useRef<string | null>(null)
  const listRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<TextAreaRef>(null)
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  useEffect(() => () => abortRef.current?.abort(), [])

  useEffect(() => {
    if (open) inputRef.current?.focus()
  }, [open])

  useEffect(() => {
    if (!sending && messages.length > 0) inputRef.current?.focus()
  }, [sending, messages.length])

  const currentPageLabel = pageLabel(context.active_key)
  const currentSelectionLabel = context.selection?.label
  const currentSelectionType = context.selection
    ? SELECTION_LABELS[context.selection.kind] ?? '对象'
    : null

  const suggestions = useMemo(() => {
    if (context.selection) {
      return [
        `总结「${context.selection.label}」的当前情况`,
        `这个${currentSelectionType}下一步最应该做什么？`,
        `检查这个${currentSelectionType}有没有风险或遗漏`,
      ]
    }
    return PAGE_SUGGESTIONS[pageGroup(context.active_key)] ?? PAGE_SUGGESTIONS.today
  }, [context.active_key, context.selection, currentSelectionType])

  const stop = useCallback(() => {
    if (!abortRef.current || stopping) return
    setStopping(true)
    abortRef.current.abort()
  }, [stopping])

  const fillSuggestion = useCallback((suggestion: string) => {
    setInput(suggestion)
    requestAnimationFrame(() => inputRef.current?.focus())
  }, [])

  const send = useCallback(async () => {
    const message = input.trim()
    if (!message || sending) return
    setInput('')
    setError(undefined)
    setStopping(false)
    setSending(true)
    // Append the user bubble and an empty assistant bubble that delta events grow.
    setMessages((prev) => [...prev, { role: 'user', text: message }, { role: 'assistant', text: '' }])

    const appendDelta = (text: string) => setMessages((prev) => {
      const next = prev.slice()
      const last = next[next.length - 1]
      if (last && last.role === 'assistant') next[next.length - 1] = { role: 'assistant', text: last.text + text }
      return next
    })

    const controller = new AbortController()
    abortRef.current = controller
    try {
      const req: ChatRequest = { message, thread_id: threadId.current, page_context: context }
      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
        signal: controller.signal,
      })
      if (!response.ok) throw new Error(`对话请求失败：HTTP ${response.status}`)
      if (!response.body) throw new Error('对话响应无数据流（response.body 为空）')

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let streamError: string | undefined

      // Manual SSE parse: split the byte stream into `\n\n`-delimited blocks.
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let sep: number
        while ((sep = buffer.indexOf('\n\n')) !== -1) {
          const block = buffer.slice(0, sep)
          buffer = buffer.slice(sep + 2)
          if (!block.trim()) continue
          const { event, data } = parseSSEBlock(block)
          if (event === 'thread') {
            const parsed = JSON.parse(data) as ChatThreadEvent
            threadId.current = parsed.thread_id
          } else if (event === 'delta') {
            const parsed = JSON.parse(data) as ChatDeltaEvent
            appendDelta(parsed.text)
          } else if (event === 'error') {
            const parsed = JSON.parse(data) as ChatErrorEvent
            streamError = parsed.message
          } else if (event === 'done') {
            // Round finished; keep reading until the stream closes.
          }
        }
      }
      if (streamError) throw new Error(streamError)
    } catch (cause: unknown) {
      if (isAbortError(cause)) {
        // Keep any partial reply; drop only a still-empty assistant bubble.
        setMessages((prev) => {
          const last = prev[prev.length - 1]
          if (last && last.role === 'assistant' && last.text === '') return prev.slice(0, -1)
          return prev
        })
        return
      }
      const text = errorText(cause)
      setError(text)
      setInput((current) => current.trim() ? current : message)
      // Drop the trailing empty assistant bubble so a failed round leaves no blank.
      setMessages((prev) => {
        const last = prev[prev.length - 1]
        if (last && last.role === 'assistant' && last.text === '') return prev.slice(0, -1)
        return prev
      })
    } finally {
      abortRef.current = null
      setStopping(false)
      setSending(false)
    }
  }, [input, sending, context])

  const onKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      if (!sending) void send()
    }
  }

  return <section className="chat-panel jarvis-chat" aria-label="Jarvis 对话">
    <header className="chat-header">
      <div className="chat-title-row">
        <Text strong className="chat-title">Jarvis 对话</Text>
        <span className="chat-ready" aria-label="对话将使用当前页面上下文"><span aria-hidden="true" />随当前页面</span>
        <Button type="text" size="small" className="chat-close" icon={<CloseOutlined />} aria-label="关闭 Jarvis 对话" onClick={onClose} />
      </div>
      <div className="chat-context" aria-label="当前对话上下文">
        <span className="chat-context-page">正在查看「{currentPageLabel}」</span>
        {currentSelectionLabel && <>
          <span className="chat-context-separator" aria-hidden="true">·</span>
          <span className="chat-context-selection">{currentSelectionType}：{currentSelectionLabel}</span>
        </>}
      </div>
      <Text type="secondary" className="chat-context-note">Jarvis 会结合这些上下文回答</Text>
    </header>
    <div
      className="chat-messages"
      ref={listRef}
      role="log"
      aria-live="polite"
      aria-relevant="additions text"
      aria-label="对话记录"
    >
      {messages.length === 0 && <div className="chat-empty">
        <div className="chat-empty-mark" aria-hidden="true">J</div>
        <Text strong className="chat-empty-title">从当前页面开始</Text>
        <Text type="secondary" className="chat-empty-description">你可以直接询问，也可以选一个建议填入输入框。</Text>
        <div className="chat-suggestions" aria-label="建议问题">
          {suggestions.map((suggestion) => (
            <button
              key={suggestion}
              type="button"
              className="chat-suggestion"
              onClick={() => fillSuggestion(suggestion)}
            >
              <span>{suggestion}</span>
              <span className="chat-suggestion-arrow" aria-hidden="true">→</span>
            </button>
          ))}
        </div>
      </div>}
      {messages.map((msg, index) => (
        <div
          key={index}
          className={`chat-bubble-row ${msg.role}`}
          aria-label={msg.role === 'assistant' ? 'Jarvis 的回复' : '你的消息'}
        >
          <div className={`chat-bubble ${msg.role}`}>
            {msg.role === 'assistant' && msg.text === '' && sending
              ? <span className="chat-typing"><span className="chat-typing-dots" aria-hidden="true"><i /><i /><i /></span>Jarvis 正在思考</span>
              : msg.text}
          </div>
        </div>
      ))}
    </div>
    {error && <Alert className="chat-error" type="error" showIcon title="Jarvis 暂时无法回复" description={error} closable onClose={() => setError(undefined)} />}
    <div className="chat-composer">
      <Input.TextArea
        ref={inputRef}
        className="chat-textarea"
        value={input}
        onChange={(event) => setInput(event.target.value)}
        onKeyDown={onKeyDown}
        disabled={sending}
        autoSize={{ minRows: 1, maxRows: 6 }}
        maxLength={4000}
        aria-label="发送给 Jarvis 的消息"
        aria-describedby="chat-composer-hint"
        placeholder="询问 Jarvis，或者告诉它你想做什么…"
      />
      <div className="chat-composer-footer">
        <Text id="chat-composer-hint" type="secondary" className="chat-composer-hint" aria-live="polite">
          {stopping ? '正在停止回复…' : sending ? 'Jarvis 正在回复，你可以随时停止' : 'Enter 发送 · Shift + Enter 换行'}
        </Text>
        {sending
          ? <Button danger icon={<StopOutlined />} disabled={stopping} aria-label="停止 Jarvis 回复" onClick={stop}>
            {stopping ? '正在停止' : '停止生成'}
          </Button>
          : <Button type="primary" icon={<SendOutlined />} disabled={!input.trim()} aria-label="发送消息" onClick={() => void send()}>发送</Button>}
      </div>
    </div>
  </section>
}
