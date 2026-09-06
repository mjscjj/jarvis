import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { CloseOutlined, CopyOutlined, DeleteOutlined, HistoryOutlined, PaperClipOutlined, PlusOutlined, ReloadOutlined, SendOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Typography } from 'antd'
import type { TextAreaRef } from 'antd/es/input/TextArea'
import { getChatHistory, getChatRuntimeConfig, getSignedInOpenID, isMissingChatHistoryError, listChatThreads, resolveChatBaseURL } from './api'
import { useAgentIdentity } from './agentIdentity'
import { usePageContext } from './pageContext'
import { isOKRTab, isWeeklyWorkspaceTab, OKR_TAB_DEFINITIONS } from './okr/navigation'
import type { ChatDeltaEvent, ChatErrorEvent, ChatRequest, ChatThreadEvent, ChatThreadSummary, PageContext } from './types'
import './styles/chat.css'

const { Text } = Typography

type ChatMessageStatus = 'queued' | 'sending' | 'sent' | 'failed' | 'partial' | 'paused'

interface ChatDiagnostic {
  message: string
  detail?: string
  logId?: string
  at: string
  recoverable?: boolean
}

interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  text: string
  status?: ChatMessageStatus
  imageName?: string
  retryText?: string
  retryImage?: File | null
  diagnostic?: ChatDiagnostic
}

interface QueuedChatTurn {
  id: string
  userMessageId: string
  text: string
  image: File | null
  pageContext: PageContext
  createdAt: number
}

const CHAT_THREAD_STORAGE_KEY = 'jarvis.chat.threadId'
const CHAT_IMAGE_MAX_BYTES = 10 * 1024 * 1024
const CHAT_IMAGE_TYPES = new Set(['image/png', 'image/jpeg'])
const CHAT_THREAD_LIST_LIMIT = 30
const RUNNING_STATUS_STEPS = ['正在理解页面', '正在查找资料', '正在整理回复']

const threadDateFormatter = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
})

const PAGE_LABELS: Record<string, string> = {
  today: '工作台',
  overview: '工作台',
  workbench: '工作台',
  tasks: '任务',
  review: '工作台',
  progress: '工作台',
  memory: '世界',
  background: '世界',
  okr: 'OKR',
  'agency-okr': 'Agency OKR',
  automation: '任务',
  'scheduled-tasks': '任务',
  plugins: '插件',
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

function pageSuggestions(agentName: string): Record<string, string[]> {
  return {
    workbench: ['我现在最需要关注什么？', '总结今天真正完成的事', '有哪些风险会影响今天交付？'],
    tasks: ['哪些任务最需要我处理？', '帮我梳理当前的阻塞', '检查进行中的任务是否偏离目标'],
    review: ['总结今天真正完成的事', '哪些承诺还没有闭环？', '帮我找出值得复盘的问题'],
    memory: [`${agentName} 目前是怎么理解我的工作的？`, '检查项目背景有没有过时信息', '帮我找到某个项目的关键上下文'],
    automation: ['哪些自动化即将运行？', '检查自动化之间是否有冲突', '帮我设计一个新的自动化'],
    weekly: ['总结当前周报的重点进展和风险', '检查哪些 KR 还需要补充', '帮我准备本周会议要点'],
    clues: ['最近出现了哪些重要线索？', '哪些线索还在等待更多证据？', '帮我解释线索到任务的转换'],
    system: [`检查 ${agentName} 当前的关键配置`, '有哪些系统异常会影响任务？', '帮我定位最近的运行问题'],
  }
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function pageLabel(context: PageContext): string {
  if (context.active_key === 'agency-okr') {
    const definition = OKR_TAB_DEFINITIONS.find((item) => item.key === context.view_state.tab)
    return `OKR · ${definition?.label ?? '管理与打标'}`
  }
  return PAGE_LABELS[context.active_key] ?? '当前页面'
}

function pageGroup(context: PageContext): string {
  if (context.active_key === 'agency-okr' && context.view_state.tab === 'agent-flows') return 'automation'
  if (context.active_key === 'agency-okr' && isOKRTab(context.view_state.tab) && isWeeklyWorkspaceTab(context.view_state.tab)) return 'weekly'
  if (['management', 'settings', 'debug', 'system-tasks'].includes(context.active_key)) return 'system'
  if (['today', 'overview', 'review', 'progress', 'workbench'].includes(context.active_key)) return 'workbench'
  if (['tasks', 'automation', 'scheduled-tasks'].includes(context.active_key)) return 'tasks'
  if (['memory', 'background'].includes(context.active_key)) return 'memory'
  if (['clues', 'todos'].includes(context.active_key)) return 'clues'
  return 'workbench'
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

function isStaleThreadError(text: string): boolean {
  return text.includes('no rollout found for thread id')
    || text.includes('missing thread.started')
    || text.includes('belongs to another CLI')
}

function chatImageError(file: File): string | undefined {
  if (!CHAT_IMAGE_TYPES.has(file.type)) return '截图只支持 PNG 或 JPEG'
  if (file.size === 0) return '截图文件为空'
  if (file.size > CHAT_IMAGE_MAX_BYTES) return '截图不能超过 10 MB'
  return undefined
}

function makeClientId(prefix: string): string {
  if (typeof globalThis.crypto?.randomUUID === 'function') return `${prefix}-${globalThis.crypto.randomUUID()}`
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function compactText(text: string, limit: number): string {
  const normalized = text.trim().replace(/\s+/g, ' ')
  if (normalized.length <= limit) return normalized
  return `${normalized.slice(0, Math.max(0, limit - 3))}...`
}

function formatThreadTime(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return threadDateFormatter.format(date)
}

function diagnosticFromEvent(event: ChatErrorEvent): ChatDiagnostic {
  return {
    message: event.message || '这轮没有完成，可以重试或继续发送。',
    detail: event.detail,
    logId: event.log_id,
    recoverable: event.recoverable,
    at: new Date().toISOString(),
  }
}

async function diagnosticFromResponse(response: Response): Promise<ChatDiagnostic> {
  let detail = `HTTP ${response.status}`
  try {
    const payload = await response.json() as { msg?: string }
    if (payload.msg) detail = payload.msg
  } catch {
    // Keep the HTTP status as the diagnostic detail when the response is not JSON.
  }
  return {
    message: '这轮没有完成，可以重试或继续发送。',
    detail,
    at: new Date().toISOString(),
    recoverable: response.status >= 500,
  }
}

function diagnosticFromCause(cause: unknown): ChatDiagnostic {
  if (typeof cause === 'object' && cause !== null && 'message' in cause && 'at' in cause) return cause as ChatDiagnostic
  const detail = errorText(cause)
  return {
    message: isStaleThreadError(detail) ? '这个会话暂时无法继续，已准备切换到新对话。' : '这轮没有完成，可以重试或继续发送。',
    detail,
    at: new Date().toISOString(),
    recoverable: isStaleThreadError(detail),
  }
}

function threadSummaryFromTurn(threadId: string, turn: QueuedChatTurn): ChatThreadSummary {
  const at = new Date(turn.createdAt).toISOString()
  return {
    thread_id: threadId,
    title: compactText(turn.text, 34) || '新对话',
    preview: '正在回复...',
    message_at: at,
    updated_at: at,
  }
}

export default function Chat({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { name: agentName, shortName: agentShortName } = useAgentIdentity()
  const { context } = usePageContext()
  const [messages, setMessages] = useState<ChatMessage[]>([])
  const [input, setInput] = useState('')
  const [image, setImage] = useState<File | null>(null)
  const [queue, setQueue] = useState<QueuedChatTurn[]>([])
  const [sending, setSending] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [queuePaused, setQueuePaused] = useState(false)
  const [paused, setPaused] = useState(false)
  const [historyLoading, setHistoryLoading] = useState(false)
  const [threadsLoading, setThreadsLoading] = useState(false)
  const [threadsOpen, setThreadsOpen] = useState(false)
  const [threads, setThreads] = useState<ChatThreadSummary[]>([])
  const [error, setError] = useState<ChatDiagnostic>()
  const [diagnosticOpen, setDiagnosticOpen] = useState(false)
  const [runningStatusIndex, setRunningStatusIndex] = useState(0)
  const [notice, setNotice] = useState<string>()
  const [chatBaseURL, setChatBaseURL] = useState<string>()
  const [threadId, setThreadId] = useState<string | null>(() => window.localStorage.getItem(CHAT_THREAD_STORAGE_KEY))
  const listRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<TextAreaRef>(null)
  const imageInputRef = useRef<HTMLInputElement>(null)
  const abortRef = useRef<AbortController | null>(null)
  const processingRef = useRef(false)
  const threadIdRef = useRef(threadId)

  const imagePreviewURL = useMemo(() => image ? URL.createObjectURL(image) : undefined, [image])

  useEffect(() => () => {
    if (imagePreviewURL) URL.revokeObjectURL(imagePreviewURL)
  }, [imagePreviewURL])

  useEffect(() => {
    threadIdRef.current = threadId
  }, [threadId])

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  useEffect(() => () => abortRef.current?.abort(), [])

  useEffect(() => {
    if (!sending) {
      setRunningStatusIndex(0)
      return undefined
    }
    const timer = window.setInterval(() => {
      setRunningStatusIndex((index) => (index + 1) % RUNNING_STATUS_STEPS.length)
    }, 5000)
    return () => window.clearInterval(timer)
  }, [sending])

  useEffect(() => {
    const controller = new AbortController()
    getChatRuntimeConfig(controller.signal)
      .then(({ port }) => {
        setChatBaseURL(resolveChatBaseURL(window.location.origin, port))
      })
      .catch((cause: unknown) => {
        if (!isAbortError(cause)) {
          setError({
            message: '对话服务暂时不可用。',
            detail: errorText(cause),
            at: new Date().toISOString(),
            recoverable: true,
          })
        }
      })
    return () => controller.abort()
  }, [])

  const refreshThreads = useCallback((baseURL = chatBaseURL) => {
    if (!baseURL) return undefined
    const controller = new AbortController()
    setThreadsLoading(true)
    listChatThreads(baseURL, controller.signal)
      .then((result) => {
        setThreads(result.threads)
        if (result.warnings && result.warnings.length > 0) {
          setError({
            message: '部分历史对话暂时无法读取。',
            detail: result.warnings.map((warning) => {
              const target = warning.thread_id || warning.file || 'unknown'
              return `${target}: ${warning.message}`
            }).join('\n'),
            at: new Date().toISOString(),
            recoverable: true,
          })
        }
      })
      .catch((cause: unknown) => {
        if (!isAbortError(cause)) {
          setError({
            message: '暂时无法读取历史对话。',
            detail: errorText(cause),
            at: new Date().toISOString(),
            recoverable: true,
          })
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) setThreadsLoading(false)
      })
    return () => controller.abort()
  }, [chatBaseURL])

  useEffect(() => {
    if (!chatBaseURL) return
    return refreshThreads(chatBaseURL)
  }, [chatBaseURL, refreshThreads])

  useEffect(() => {
    if (!chatBaseURL) return
    if (!threadId) {
      setHistoryLoading(false)
      return
    }
    if (sending || queue.length > 0) return
    const controller = new AbortController()
    setHistoryLoading(true)
    getChatHistory(chatBaseURL, threadId, controller.signal)
      .then((history) => {
        setMessages(history.messages.map((message) => ({
          id: makeClientId(message.role),
          role: message.role,
          text: message.text,
          status: 'sent',
        })))
        setError(undefined)
        setDiagnosticOpen(false)
        setNotice(undefined)
      })
      .catch((cause: unknown) => {
        if (isAbortError(cause)) return
        if (isMissingChatHistoryError(cause)) {
          window.localStorage.removeItem(CHAT_THREAD_STORAGE_KEY)
          setThreadId(null)
          setMessages([])
          setError(undefined)
          setNotice('上次会话已失效，已自动切换到新会话。')
          return
        }
        setError({
          message: '暂时无法恢复这个会话。',
          detail: errorText(cause),
          at: new Date().toISOString(),
          recoverable: true,
        })
      })
      .finally(() => {
        if (!controller.signal.aborted) setHistoryLoading(false)
      })
    return () => controller.abort()
  }, [chatBaseURL, queue.length, sending, threadId])

  useEffect(() => {
    if (open) inputRef.current?.focus()
  }, [open])

  useEffect(() => {
    if (!sending && messages.length > 0) inputRef.current?.focus()
  }, [sending, messages.length])

  const currentPageLabel = pageLabel(context)
  const currentSelectionLabel = context.selection?.label
  const currentActionLabel = context.active_key === 'agency-okr' && context.view_state.tab === 'agent-flows'
    ? context.view_state.action_label
    : undefined
  const currentQuarter = context.active_key === 'agency-okr' ? context.view_state.quarter : undefined
  const currentWeek = context.active_key === 'agency-okr' ? context.view_state.week : undefined
  const currentSelectionType = context.selection
    ? SELECTION_LABELS[context.selection.kind] ?? '对象'
    : null

  const suggestions = useMemo(() => {
    if (context.active_key === 'agency-okr' && context.view_state.tab === 'agent-flows') {
      const action = currentActionLabel ? `“${currentActionLabel}”` : '当前自动化流程'
      return [
        `检查${action}的配置和最近执行情况`,
        `手动执行${action}`,
        `解释${action}会使用哪些 Prompt 和工具`,
      ]
    }
    if (context.selection) {
      return [
        `总结「${context.selection.label}」的当前情况`,
        `这个${currentSelectionType}下一步最应该做什么？`,
        `检查这个${currentSelectionType}有没有风险或遗漏`,
      ]
    }
    const defaults = pageSuggestions(agentName)
    return defaults[pageGroup(context)] ?? defaults.today
  }, [agentName, context, currentActionLabel, currentSelectionType])

  const queueStatusText = useMemo(() => {
    if (historyLoading) return '正在恢复'
    if (sending) return RUNNING_STATUS_STEPS[runningStatusIndex]
    if (queue.length > 0 && queuePaused) return `${queue.length} 条待发送`
    if (queue.length > 0) return `${queue.length} 条排队中`
    if (paused) return '已暂停'
    return '就绪'
  }, [historyLoading, paused, queue.length, queuePaused, runningStatusIndex, sending])

  const stop = useCallback(() => {
    if (!abortRef.current || stopping) return
    setStopping(true)
    setQueuePaused(true)
    abortRef.current.abort()
  }, [stopping])

  const fillSuggestion = useCallback((suggestion: string) => {
    setInput(suggestion)
    requestAnimationFrame(() => inputRef.current?.focus())
  }, [])

  const startNewChat = useCallback(() => {
    if (sending || queue.length > 0) {
      setNotice('当前还有消息在处理，完成或清空队列后再新建对话。')
      return
    }
    window.localStorage.removeItem(CHAT_THREAD_STORAGE_KEY)
    setThreadId(null)
    setMessages([])
    setImage(null)
    setQueue([])
    setQueuePaused(false)
    setError(undefined)
    setDiagnosticOpen(false)
    setNotice(undefined)
    setPaused(false)
    setThreadsOpen(false)
    inputRef.current?.focus()
  }, [queue.length, sending])

  const attachImage = useCallback((file: File) => {
    const validationError = chatImageError(file)
    if (validationError) {
      setError({ message: validationError, at: new Date().toISOString(), recoverable: true })
      return
    }
    setImage(file)
    setError(undefined)
  }, [])

  const selectThread = useCallback((nextThreadID: string) => {
    if (sending || queue.length > 0) {
      setNotice('当前还有消息在处理，完成或清空队列后再切换对话。')
      return
    }
    const normalized = nextThreadID.trim()
    if (!normalized || normalized === threadId) {
      setThreadsOpen(false)
      return
    }
    window.localStorage.setItem(CHAT_THREAD_STORAGE_KEY, normalized)
    setThreadId(normalized)
    setImage(null)
    setError(undefined)
    setDiagnosticOpen(false)
    setNotice(undefined)
    setPaused(false)
    setThreadsOpen(false)
  }, [queue.length, sending, threadId])

  const clearQueue = useCallback(() => {
    setQueue([])
    setMessages((prev) => prev.filter((message) => message.status !== 'queued'))
    setQueuePaused(false)
    setNotice(undefined)
  }, [])

  const resumeQueue = useCallback(() => {
    setQueuePaused(false)
    setPaused(false)
    setError(undefined)
    setDiagnosticOpen(false)
    setNotice(undefined)
  }, [])

  const enqueueTurn = useCallback((text: string, nextImage: File | null, options?: { reuseMessageId?: string; front?: boolean }) => {
    const message = text.trim()
    if (!message) return
    const userMessageId = options?.reuseMessageId ?? makeClientId('user')
    const turn: QueuedChatTurn = {
      id: makeClientId('turn'),
      userMessageId,
      text: message,
      image: nextImage,
      pageContext: context,
      createdAt: Date.now(),
    }
    if (options?.reuseMessageId) {
      setMessages((prev) => prev.map((item) => item.id === userMessageId
        ? { ...item, status: 'queued', diagnostic: undefined, retryText: undefined, retryImage: undefined }
        : item))
    } else {
      setMessages((prev) => [...prev, {
        id: userMessageId,
        role: 'user',
        text: message,
        status: sending || queue.length > 0 || queuePaused ? 'queued' : 'sending',
        imageName: nextImage?.name,
        retryText: message,
        retryImage: nextImage,
      }])
    }
    setQueue((prev) => options?.front ? [turn, ...prev] : [...prev, turn])
    setQueuePaused(false)
    setPaused(false)
    setError(undefined)
    setDiagnosticOpen(false)
  }, [context, queue.length, queuePaused, sending])

  const send = useCallback(() => {
    const message = input.trim()
    if (!message) return
    if (!chatBaseURL) {
      setError({
        message: '对话服务还没有准备好。',
        detail: 'chat runtime config is not loaded',
        at: new Date().toISOString(),
        recoverable: true,
      })
      return
    }
    const imageToSend = image
    setInput('')
    setImage(null)
    setNotice(undefined)
    enqueueTurn(message, imageToSend)
    requestAnimationFrame(() => inputRef.current?.focus())
  }, [chatBaseURL, enqueueTurn, image, input])

  const copyDiagnostic = useCallback(async (diagnostic: ChatDiagnostic) => {
    const lines = [
      `message: ${diagnostic.message}`,
      diagnostic.detail ? `detail: ${diagnostic.detail}` : '',
      diagnostic.logId ? `log_id: ${diagnostic.logId}` : '',
      `at: ${diagnostic.at}`,
      threadId ? `thread_id: ${threadId}` : '',
      `page: ${currentPageLabel}`,
    ].filter(Boolean)
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard API unavailable')
      await navigator.clipboard.writeText(lines.join('\n'))
      setNotice('诊断信息已复制。')
    } catch {
      setNotice('无法自动复制诊断，可以展开日志后手动复制。')
    }
  }, [currentPageLabel, threadId])

  const retryMessage = useCallback((message: ChatMessage) => {
    const retryText = message.retryText ?? message.text
    enqueueTurn(retryText, message.retryImage ?? null, { reuseMessageId: message.id, front: true })
  }, [enqueueTurn])

  const runTurn = useCallback(async (turn: QueuedChatTurn) => {
    if (!chatBaseURL || processingRef.current) return
    processingRef.current = true
    setQueue((prev) => prev.filter((item) => item.id !== turn.id))
    setError(undefined)
    setDiagnosticOpen(false)
    setNotice(undefined)
    setStopping(false)
    setPaused(false)
    setSending(true)
    setMessages((prev) => prev.map((item) => item.id === turn.userMessageId
      ? { ...item, status: 'sending', diagnostic: undefined }
      : item))
    const assistantMessageId = makeClientId('assistant')
    setMessages((prev) => [...prev, { id: assistantMessageId, role: 'assistant', text: '', status: 'sending' }])

    const appendDelta = (text: string) => setMessages((prev) => {
      const next = prev.slice()
      const index = next.findIndex((item) => item.id === assistantMessageId)
      if (index !== -1) next[index] = { ...next[index], text: next[index].text + text }
      return next
    })

    const controller = new AbortController()
    abortRef.current = controller
    const threadAtStart = threadIdRef.current
    try {
      // Read the signed-in identity per turn: the person can log in or out
      // while the conversation stays open.
      const req: ChatRequest = {
        message: turn.text,
        thread_id: threadAtStart,
        page_context: turn.pageContext,
        image: turn.image,
        user_open_id: await getSignedInOpenID(controller.signal),
      }
      const form = new FormData()
      form.append('message', req.message)
      if (req.thread_id) form.append('thread_id', req.thread_id)
      if (req.user_open_id) form.append('user_open_id', req.user_open_id)
      if (req.page_context) form.append('page_context', JSON.stringify(req.page_context))
      if (req.image) form.append('image', req.image, req.image.name)
      const response = await fetch(`${chatBaseURL}/api/chat`, {
        method: 'POST',
        body: form,
        signal: controller.signal,
      })
      if (!response.ok) throw await diagnosticFromResponse(response)
      if (!response.body) throw new Error('对话响应无数据流（response.body 为空）')

      const reader = response.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      let streamError: ChatDiagnostic | undefined

      const consumeBlock = (block: string) => {
        if (!block.trim()) return
        const { event, data } = parseSSEBlock(block)
        if (event === 'thread') {
          const parsed = JSON.parse(data) as ChatThreadEvent
          setThreadId(parsed.thread_id)
          threadIdRef.current = parsed.thread_id
          window.localStorage.setItem(CHAT_THREAD_STORAGE_KEY, parsed.thread_id)
          setThreads((prev) => {
            const withoutCurrent = prev.filter((item) => item.thread_id !== parsed.thread_id)
            return [threadSummaryFromTurn(parsed.thread_id, turn), ...withoutCurrent].slice(0, CHAT_THREAD_LIST_LIMIT)
          })
        } else if (event === 'delta') {
          const parsed = JSON.parse(data) as ChatDeltaEvent
          appendDelta(parsed.text)
        } else if (event === 'error') {
          const parsed = JSON.parse(data) as ChatErrorEvent
          streamError = diagnosticFromEvent(parsed)
        } else if (event === 'done') {
          // Round finished; keep reading until the stream closes.
        }
      }

      // Manual SSE parse: split the byte stream into `\n\n`-delimited blocks.
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let sep: number
        while ((sep = buffer.indexOf('\n\n')) !== -1) {
          const block = buffer.slice(0, sep)
          buffer = buffer.slice(sep + 2)
          consumeBlock(block)
        }
      }
      if (buffer.trim()) consumeBlock(buffer)
      if (streamError) throw streamError
      setMessages((prev) => prev.map((item) => {
        if (item.id === turn.userMessageId || item.id === assistantMessageId) return { ...item, status: 'sent' }
        return item
      }))
      refreshThreads(chatBaseURL)
      window.dispatchEvent(new Event('jarvis:chat-completed'))
    } catch (cause: unknown) {
      if (isAbortError(cause)) {
        setPaused(true)
        setQueuePaused(true)
        // Keep any partial reply; drop only a still-empty assistant bubble.
        setMessages((prev) => {
          const assistant = prev.find((item) => item.id === assistantMessageId)
          return prev.flatMap((item) => {
            if (item.id === turn.userMessageId) return [{ ...item, status: 'sent' as ChatMessageStatus }]
            if (item.id === assistantMessageId && assistant?.text === '') return []
            if (item.id === assistantMessageId) return [{ ...item, status: 'paused' as ChatMessageStatus }]
            return [item]
          })
        })
        return
      }
      const diagnostic = diagnosticFromCause(cause)
      if (isStaleThreadError(`${diagnostic.detail ?? ''} ${diagnostic.message}`) && threadAtStart) {
        window.localStorage.removeItem(CHAT_THREAD_STORAGE_KEY)
        setThreadId(null)
        threadIdRef.current = null
        setNotice('这个会话暂时无法继续，已切换到新对话。你可以继续发送。')
      }
      setQueuePaused(true)
      setError(diagnostic)
      setMessages((prev) => {
        const assistant = prev.find((item) => item.id === assistantMessageId)
        return prev.flatMap((item) => {
          if (item.id === turn.userMessageId) {
            return [{ ...item, status: 'failed' as ChatMessageStatus, diagnostic, retryText: turn.text, retryImage: turn.image }]
          }
          if (item.id === assistantMessageId && assistant?.text === '') return []
          if (item.id === assistantMessageId) return [{ ...item, status: 'partial' as ChatMessageStatus, diagnostic }]
          return [item]
        })
      })
    } finally {
      abortRef.current = null
      setStopping(false)
      setSending(false)
      processingRef.current = false
    }
  }, [chatBaseURL, refreshThreads])

  useEffect(() => {
    if (!chatBaseURL || historyLoading || sending || queuePaused || queue.length === 0 || processingRef.current) return
    void runTurn(queue[0])
  }, [chatBaseURL, historyLoading, queue, queuePaused, runTurn, sending])

  const onKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing) return
    event.preventDefault()
    send()
  }

  const onPaste = (event: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const images = Array.from(event.clipboardData.files).filter((file) => file.type.startsWith('image/'))
    if (images.length === 0) return
    event.preventDefault()
    if (images.length > 1) {
      setError({ message: '每轮只能附一张截图。', at: new Date().toISOString(), recoverable: true })
      return
    }
    attachImage(images[0])
  }

  const hasPendingQueue = queue.length > 0
  const canSwitchThread = !sending && !hasPendingQueue && !historyLoading
  const activeThread = threadId ? threads.find((item) => item.thread_id === threadId) : undefined

  return <section className="chat-panel jarvis-chat" aria-label={`${agentName} 对话`}>
    <header className="chat-header">
      <div className="chat-title-row">
        <div className="chat-heading">
          <Text strong className="chat-title">{agentName}</Text>
          <span className={`chat-status ${sending ? 'is-running' : ''}`} aria-live="polite">
            <span aria-hidden="true" />
            {queueStatusText}
          </span>
        </div>
        <div className="chat-header-actions">
          <Button type="text" size="small" className="chat-icon-button" icon={<HistoryOutlined />} aria-label="查看历史对话" title="历史对话" onClick={() => setThreadsOpen((value) => !value)} />
          <Button type="text" size="small" className="chat-icon-button" icon={<PlusOutlined />} disabled={!canSwitchThread} aria-label="新建对话" title="新建对话" onClick={startNewChat} />
          <Button type="text" size="small" className="chat-close chat-icon-button" icon={<CloseOutlined />} aria-label={`关闭 ${agentName} 对话`} onClick={onClose} />
        </div>
      </div>
      {threadsOpen && <div className="chat-thread-panel" aria-label="历史对话">
        <div className="chat-thread-panel-head">
          <span>历史对话</span>
          <Button type="text" size="small" icon={<ReloadOutlined />} loading={threadsLoading} aria-label="刷新历史对话" onClick={() => refreshThreads()} />
        </div>
        {threadsLoading && threads.length === 0 ? <div className="chat-thread-empty">正在读取...</div> : threads.length === 0 ? <div className="chat-thread-empty">暂无历史对话</div> : <div className="chat-thread-list">
          {threads.map((thread) => (
            <button
              key={thread.thread_id}
              type="button"
              className={`chat-thread-item ${thread.thread_id === threadId ? 'is-active' : ''}`}
              disabled={!canSwitchThread}
              onClick={() => selectThread(thread.thread_id)}
            >
              <span className="chat-thread-title">{thread.title || '未命名对话'}</span>
              <span className="chat-thread-preview">{thread.preview || '暂无摘要'}</span>
              <span className="chat-thread-time">{formatThreadTime(thread.message_at || thread.updated_at)}</span>
            </button>
          ))}
        </div>}
      </div>}
      <div className="chat-context" aria-label="当前对话上下文">
        <span className="chat-context-page">当前页面：{currentPageLabel}</span>
        {currentSelectionLabel && <>
          <span className="chat-context-separator" aria-hidden="true">·</span>
          <span className="chat-context-selection">{currentSelectionType}：{currentSelectionLabel}</span>
        </>}
        {currentActionLabel && <>
          <span className="chat-context-separator" aria-hidden="true">·</span>
          <span className="chat-context-selection">流程：{currentActionLabel}</span>
        </>}
        {currentQuarter && <>
          <span className="chat-context-separator" aria-hidden="true">·</span>
          <span className="chat-context-selection">{currentQuarter.replace('-', ' ')}</span>
        </>}
        {currentWeek && <>
          <span className="chat-context-separator" aria-hidden="true">·</span>
          <span className="chat-context-selection">{currentWeek}</span>
        </>}
      </div>
      {activeThread && <div className="chat-active-thread" title={activeThread.title}>{activeThread.title}</div>}
    </header>
    <div
      className="chat-messages"
      ref={listRef}
      role="log"
      aria-live="polite"
      aria-relevant="additions text"
      aria-label="对话记录"
    >
      {historyLoading ? <div className="chat-history-loading">正在恢复本地会话…</div> : messages.length === 0 && <div className="chat-empty">
        <div className="chat-empty-mark" aria-hidden="true">{agentShortName}</div>
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
      {messages.map((msg) => (
        <div
          key={msg.id}
          className={`chat-bubble-row ${msg.role}`}
          aria-label={msg.role === 'assistant' ? `${agentName} 的回复` : '你的消息'}
        >
          <div className={`chat-bubble ${msg.role} ${msg.status ? `is-${msg.status}` : ''}`}>
            {msg.imageName && <span className="chat-bubble-attachment">截图：{msg.imageName}</span>}
            {msg.role === 'assistant' && msg.text === '' && msg.status === 'sending'
              ? <span className="chat-typing"><span className="chat-typing-dots" aria-hidden="true"><i /><i /><i /></span>{queueStatusText}</span>
              : msg.text}
            {(msg.status === 'queued' || msg.status === 'failed' || msg.status === 'partial' || msg.status === 'paused') && <div className="chat-bubble-meta">
              {msg.status === 'queued' && '等待发送'}
              {msg.status === 'failed' && '这条消息未完成'}
              {msg.status === 'partial' && '回复未完成'}
              {msg.status === 'paused' && '已暂停'}
              {msg.status === 'failed' && <button type="button" onClick={() => retryMessage(msg)}>重试</button>}
              {msg.diagnostic && <button type="button" onClick={() => { setError(msg.diagnostic); setDiagnosticOpen(true) }}>诊断</button>}
            </div>}
          </div>
        </div>
      ))}
    </div>
    {notice && <Alert className="chat-error" type="info" showIcon message="提示" description={notice} closable onClose={() => setNotice(undefined)} />}
    {error && <div className="chat-error-card" role="status" aria-live="polite">
      <div className="chat-error-main">
        <strong>{error.message}</strong>
        <span>对话可以继续；需要排查时复制诊断给 owner。</span>
        {error.logId && <span>日志 ID：{error.logId}</span>}
      </div>
      <div className="chat-error-actions">
        {queuePaused && queue.length > 0 && <Button size="small" onClick={resumeQueue}>继续队列</Button>}
        {queue.length > 0 && <Button size="small" icon={<DeleteOutlined />} onClick={clearQueue}>清空待发送</Button>}
        <Button size="small" icon={<CopyOutlined />} onClick={() => void copyDiagnostic(error)}>复制诊断</Button>
        <Button size="small" type="text" onClick={() => setDiagnosticOpen((value) => !value)}>{diagnosticOpen ? '收起日志' : '查看日志'}</Button>
        <Button size="small" type="text" icon={<CloseOutlined />} aria-label="关闭错误提示" onClick={() => { setError(undefined); setDiagnosticOpen(false) }} />
      </div>
      {diagnosticOpen && <pre className="chat-diagnostic-log">{[
        error.detail ? `detail: ${error.detail}` : '',
        error.logId ? `log_id: ${error.logId}` : '',
        `time: ${error.at}`,
        threadId ? `thread_id: ${threadId}` : '',
      ].filter(Boolean).join('\n')}</pre>}
    </div>}
    <div className="chat-composer">
      <input
        ref={imageInputRef}
        className="chat-image-input"
        type="file"
        accept="image/png,image/jpeg"
        aria-label="选择截图"
        onChange={(event) => {
          const file = event.currentTarget.files?.[0]
          if (file) attachImage(file)
          event.currentTarget.value = ''
        }}
      />
      {image && imagePreviewURL && <div className="chat-image-preview" aria-label={`已附加截图 ${image.name}`}>
        <img src={imagePreviewURL} alt="待发送截图预览" />
        <span title={image.name}>{image.name}</span>
        <Button type="text" size="small" icon={<CloseOutlined />} aria-label="移除截图" onClick={() => setImage(null)} />
      </div>}
      <Input.TextArea
        ref={inputRef}
        className="chat-textarea"
        value={input}
        onChange={(event) => setInput(event.target.value)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        disabled={historyLoading}
        autoSize={{ minRows: 1, maxRows: 6 }}
        maxLength={4000}
        aria-label={`发送给 ${agentName} 的消息`}
        aria-describedby="chat-composer-hint"
        placeholder={`询问 ${agentName}，或者告诉它你想做什么…`}
      />
      <div className="chat-composer-footer">
        <div className="chat-composer-tools">
          <Button
            type="text"
            size="small"
            icon={<PaperClipOutlined />}
            disabled={historyLoading}
            aria-label="附加截图"
            title="附加 PNG/JPEG 截图（也可直接粘贴）"
            onClick={() => imageInputRef.current?.click()}
          >截图</Button>
          <Text id="chat-composer-hint" type="secondary" className="chat-composer-hint" aria-live="polite">
            {stopping ? '正在暂停回复…' : sending ? `正在回复，继续发送会进入队列${queue.length > 0 ? `（${queue.length} 条）` : ''}` : paused ? '已暂停；发送新消息或继续队列即可恢复' : hasPendingQueue ? `${queue.length} 条消息等待发送` : 'Enter 发送 · Shift + Enter 换行 · 可粘贴截图'}
          </Text>
        </div>
        <div className="chat-composer-actions">
          {sending && <Button danger icon={<StopOutlined />} disabled={stopping} aria-label={`暂停 ${agentName} 回复`} onClick={stop}>
            {stopping ? '正在暂停' : '暂停生成'}
          </Button>}
          {!sending && queuePaused && queue.length > 0 && <Button icon={<ReloadOutlined />} onClick={resumeQueue}>继续队列</Button>}
          <Button type="primary" icon={<SendOutlined />} disabled={historyLoading || !input.trim()} aria-label="发送消息" onClick={send}>{sending || queue.length > 0 ? '加入队列' : '发送'}</Button>
        </div>
      </div>
    </div>
  </section>
}
