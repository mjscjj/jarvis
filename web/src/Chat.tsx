import { useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Button, Input, Typography } from 'antd'
import { usePageContext } from './pageContext'
import type { ChatDeltaEvent, ChatErrorEvent, ChatRequest, ChatThreadEvent } from './types'

const { Text } = Typography

interface ChatMessage {
  role: 'user' | 'codex'
  text: string
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
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

export default function Chat() {
  const { context } = usePageContext()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState<string>()
  const threadId = useRef<string | null>(null)
  const listRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  const selectionLabel = context.selection ? context.selection.label : null
  const contextHint = [context.active_key || '未知页面', selectionLabel].filter(Boolean).join(' · ')

  const send = useCallback(async () => {
    const message = input.trim()
    if (!message || sending) return
    setInput('')
    setError(undefined)
    setSending(true)
    // Append the user bubble and an empty codex bubble that delta events grow.
    setMessages((prev) => [...prev, { role: 'user', text: message }, { role: 'codex', text: '' }])

    const appendDelta = (text: string) => setMessages((prev) => {
      const next = prev.slice()
      const last = next[next.length - 1]
      if (last && last.role === 'codex') next[next.length - 1] = { role: 'codex', text: last.text + text }
      return next
    })

    try {
      const req: ChatRequest = { message, thread_id: threadId.current, page_context: context }
      const response = await fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(req),
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
      const text = errorText(cause)
      setError(text)
      // Drop the trailing empty codex bubble so a failed round leaves no blank.
      setMessages((prev) => {
        const last = prev[prev.length - 1]
        if (last && last.role === 'codex' && last.text === '') return prev.slice(0, -1)
        return prev
      })
    } finally {
      setSending(false)
    }
  }, [input, sending, context])

  const onKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault()
      void send()
    }
  }

  return <div className="chat-panel">
    <div className="chat-header">
      <Text strong>codex 对话</Text>
      <Text type="secondary" className="chat-context">当前：{contextHint}</Text>
    </div>
    <div className="chat-messages" ref={listRef}>
      {messages.length === 0 && <div className="chat-empty"><Text type="secondary">向 codex 提问，它能看到你当前所在的页面上下文。</Text></div>}
      {messages.map((msg, index) => (
        <div key={index} className={`chat-bubble-row ${msg.role}`}>
          <div className={`chat-bubble ${msg.role}`}>
            {msg.role === 'codex' && msg.text === '' && sending ? <span className="chat-typing">codex 正在思考…</span> : msg.text}
          </div>
        </div>
      ))}
    </div>
    {error && <Alert className="chat-error" type="error" showIcon message="对话出错" description={error} closable onClose={() => setError(undefined)} />}
    <div className="chat-input">
      <Input.TextArea
        value={input}
        onChange={(event) => setInput(event.target.value)}
        onKeyDown={onKeyDown}
        disabled={sending}
        autoSize={{ minRows: 1, maxRows: 6 }}
        placeholder="输入消息，Enter 发送，Shift+Enter 换行"
      />
      <Button type="primary" loading={sending} disabled={!input.trim()} onClick={() => void send()}>发送</Button>
    </div>
  </div>
}
