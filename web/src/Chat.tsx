import { appPath } from './appPath.ts'
import { createContext, useContext, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { DeleteOutlined, DownloadOutlined, EditOutlined, FileOutlined, HistoryOutlined, InboxOutlined, MenuOutlined, PaperClipOutlined, PlusOutlined, SearchOutlined, SendOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Drawer, Dropdown, Empty, Input, Modal, Select, Spin, Tooltip, Typography } from 'antd'
import type { MenuProps } from 'antd'
import type { TextAreaRef } from 'antd/es/input/TextArea'
import { useAgentIdentity } from './agentIdentity'
import { usePageContext } from './pageContext'
import { apiFetch, looksLikeServiceRestart, pingHealth, ServiceUnavailableError, isServiceUnavailableError } from './api'
import MarkdownReport from './components/MarkdownReport'
import ChatDock from './components/ChatDock'
import type { ChatAgent, ChatAttachment, ChatHistoryMessage, ChatModel, ChatSession } from './types'
import './styles/chat.css'

const { Text } = Typography
interface APIEnvelope<T> {
  code: number
  data?: T
  msg?: string
}
interface ListEnvelope<T> {
  items: T[]
}

const SUGGESTIONS = [
  ['看清进展', '我现在最需要关注什么？'],
  ['理解材料', '帮我阅读这份材料，提炼结论和疑问'],
  ['推进事情', '帮我把这个想法整理成可执行的方案'],
]

async function api<T>(url: string, options?: RequestInit): Promise<T> {
  const response = await apiFetch(url, options)
  // 服务重启时网关返回 HTML 错误页；先识别，避免 JSON.parse 抛出 "Unexpected token '<'"。
  if (looksLikeServiceRestart(response)) throw new ServiceUnavailableError('与服务的连接中断，可能正在重启', response.status)
  const payload = (await response.json()) as APIEnvelope<T>
  if (!response.ok || payload.code !== 0 || payload.data === undefined) throw new Error(payload.msg || `请求失败：HTTP ${response.status}`)
  return payload.data
}

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
function agentLabel(agent: string): string {
  return ({ codex: 'Codex', trae: 'TRAE', cursor: 'Cursor' } as Record<string, string>)[agent] || agent
}
function dayGroup(value: string): string {
  const date = new Date(value),
    now = new Date()
  if (date.toDateString() === now.toDateString()) return '今天'
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  return date.toDateString() === yesterday.toDateString() ? '昨天' : '更早'
}
function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}
function defaultEffort(model?: ChatModel): string {
  return model?.default_reasoning_effort || model?.reasoning_efforts?.find((value) => value === 'medium') || model?.reasoning_efforts?.[0] || 'medium'
}
function isMissingChatSession(cause: unknown): boolean {
  return cause instanceof Error && cause.message.includes('chat record not found')
}

const ChatAPIContext = createContext('/api/chat')

export default function Chat({ compact = false, hidden = false, isolated = false }: { compact?: boolean; hidden?: boolean; isolated?: boolean }) {
  return <ChatAPIContext.Provider value={isolated ? '/api/okr-chat' : '/api/chat'}>
    <ChatInner compact={compact} hidden={hidden} isolated={isolated} />
  </ChatAPIContext.Provider>
}

function ChatInner({ compact, hidden, isolated }: { compact: boolean; hidden: boolean; isolated: boolean }) {
  const [expanded, setExpanded] = useState(false)
  const apiBase = useContext(ChatAPIContext)
  const { name: agentName, shortName } = useAgentIdentity()
  const { context, setViewState, navigate } = usePageContext()
  const [sessions, setSessions] = useState<ChatSession[]>([])
  const [active, setActive] = useState<ChatSession | null>(null)
  const [agents, setAgents] = useState<ChatAgent[]>([])
  const [models, setModels] = useState<Record<string, ChatModel[]>>({})
  const [loading, setLoading] = useState(true)
  const [historyOpen, setHistoryOpen] = useState(false)
  const [archived, setArchived] = useState(false)
  const [query, setQuery] = useState('')
  const [input, setInput] = useState('')
  const [attachments, setAttachments] = useState<ChatAttachment[]>([])
  const [uploading, setUploading] = useState(false)
  const [running, setRunning] = useState<Set<string>>(new Set())
  const [accepting, setAccepting] = useState<string | null>(null)
  const [streamText, setStreamText] = useState<Record<string, string>>({})
  const [error, setError] = useState<string>()
  // connectionLost 记录服务疑似重启导致的断连；reconnecting 表示正在探活恢复。
  const [connectionLost, setConnectionLost] = useState(false)
  const [reconnecting, setReconnecting] = useState(false)
  const [editingTitle, setEditingTitle] = useState(false)
  const [switching, setSwitching] = useState(false)
  const [saving, setSaving] = useState(false)
  const [creating, setCreating] = useState(false)
  const inputRef = useRef<TextAreaRef>(null),
    fileRef = useRef<HTMLInputElement>(null),
    listRef = useRef<HTMLDivElement>(null)
  const activeID = useRef('')
  const draftTimer = useRef<number | undefined>(undefined)
  // The chat stays mounted across pages. Async results must use the current route,
  // never the route captured when a request started.
  const pageRef = useRef({ context, setViewState })
  pageRef.current = { context, setViewState }
  const draftRef = useRef({ text: input, attachment_ids: attachments.map((item) => item.id) })
  draftRef.current = { text: input, attachment_ids: attachments.map((item) => item.id) }
  const sessionRequest = useRef(0)
  const draftWrite = useRef<Promise<unknown>>(Promise.resolve())
  const reportError = (cause: unknown) => {
    // 服务重启导致的断连单独提示并触发重连，不把它当普通错误弹给用户。
    if (isServiceUnavailableError(cause)) { setConnectionLost(true); return }
    setError(cause instanceof Error ? cause.message : String(cause))
  }
  const attempt = async (action: Promise<unknown>) => { try { await action } catch (cause) { reportError(cause) } }
  const saveDraft = useCallback((id: string, draft: ChatSession['draft']) => {
    // Keep an older debounce request from overwriting a newer session-switch save.
    // Each caller reports its own error; a later edit can still retry after failure.
    const write = draftWrite.current.catch(() => undefined).then(() => api<ChatSession>(`${apiBase}/sessions/${id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ draft }),
    }))
    draftWrite.current = write
    return write
  }, [])

  const loadModels = useCallback(
    async (agent: string, force = false) => {
      if (!force && models[agent]) return models[agent]
      const result = await api<ListEnvelope<ChatModel>>(`${apiBase}/agents/${agent}/models`)
      setModels((current) => ({ ...current, [agent]: result.items }))
      return result.items
    },
    [models],
  )
  const loadSessions = useCallback(
    async (nextArchived = archived, nextQuery = query) => {
      const result = await api<ListEnvelope<ChatSession>>(`${apiBase}/sessions?archived=${nextArchived}&query=${encodeURIComponent(nextQuery)}`)
      setSessions(result.items)
      return result.items
    },
    [archived, query],
  )
  const openSession = useCallback(
    async (id: string, closeDrawer = true) => {
      if (id === activeID.current) {
        if (closeDrawer) setHistoryOpen(false)
        return
      }
      const request = ++sessionRequest.current
      setSwitching(true)
      window.clearTimeout(draftTimer.current)
      try {
        if (activeID.current && activeID.current !== id) {
          await saveDraft(activeID.current, draftRef.current)
        }
        const detail = await api<ChatSession>(`${apiBase}/sessions/${id}`)
        if (request !== sessionRequest.current) return
        setActive(detail)
        activeID.current = detail.id
        setInput(detail.draft?.text || '')
        setAttachments(detail.pending_attachments || [])
        setError(undefined)
        if (!isolated && pageRef.current.context.active_key === 'chat') pageRef.current.setViewState({ session: detail.id })
        if (closeDrawer) setHistoryOpen(false)
        void loadModels(detail.agent).catch(reportError)
      } finally {
        if (request === sessionRequest.current) setSwitching(false)
      }
    },
    [loadModels, saveDraft],
  )
  const createSession = useCallback(
    async (agent?: string, model?: string, fromSessionID?: string) => {
      setCreating(true)
      try {
        const chosenAgent = agent || agents.find((item) => item.default && item.available)?.id || agents.find((item) => item.available)?.id || 'codex'
        let available = models[chosenAgent] || []
        if (!available.length) available = await loadModels(chosenAgent)
        const chosenModel = model || available.find((item) => item.default)?.id || available[0]?.id
        if (!chosenModel) throw new Error(`${agentLabel(chosenAgent)} 没有可用模型`)
        const chosen = available.find((item) => item.id === chosenModel)
        const detail = await api<ChatSession>(`${apiBase}/sessions`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            title: fromSessionID && active ? `${active.title} · 继续` : '新对话',
            agent: chosenAgent,
            model: chosenModel,
            reasoning_effort: defaultEffort(chosen),
            sources: [],
            draft: {},
            from_session_id: fromSessionID || '',
          }),
        })
        await loadSessions(false, '')
        setArchived(false)
        setQuery('')
        await openSession(detail.id)
        requestAnimationFrame(() => inputRef.current?.focus())
        return detail
      } finally { setCreating(false) }
    },
    [active, agents, loadModels, loadSessions, models, openSession],
  )

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        const [agentData, sessionData] = await Promise.all([api<ListEnvelope<ChatAgent>>(`${apiBase}/agents`), api<ListEnvelope<ChatSession>>(`${apiBase}/sessions?archived=false`)])
        if (!alive) return
        setAgents(agentData.items)
        setSessions(sessionData.items)
        const requested = !isolated && pageRef.current.context.active_key === 'chat' ? pageRef.current.context.view_state.session : undefined
        if (requested) {
          try {
            await openSession(requested, false)
            return
          } catch (cause) {
            if (!isMissingChatSession(cause)) throw cause
          }
        }
        const targetID = sessionData.items[0]?.id
        if (targetID) await openSession(targetID, false)
        else {
          const available = agentData.items.find((item) => item.default && item.available) || agentData.items.find((item) => item.available)
          if (!available) throw new Error('没有可用的底层 Agent，请先安装并登录 Codex、TRAE 或 Cursor')
          const discovered = await api<ListEnvelope<ChatModel>>(`${apiBase}/agents/${available.id}/models`)
          if (!alive) return
          setModels((current) => ({
            ...current,
            [available.id]: discovered.items,
          }))
          const selected = discovered.items.find((item) => item.default) || discovered.items[0]
          if (!selected) throw new Error(`${available.name} 没有可用模型`)
          const created = await api<ChatSession>(`${apiBase}/sessions`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              title: '新对话',
              agent: available.id,
              model: selected.id,
              reasoning_effort: defaultEffort(selected),
              sources: [],
              draft: {},
              from_session_id: '',
            }),
          })
          await openSession(created.id, false)
          setSessions([created])
        }
      } catch (cause) {
        if (alive) setError(cause instanceof Error ? cause.message : String(cause))
      } finally {
        if (alive) setLoading(false)
      }
    })()
    return () => {
      alive = false
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (isolated || loading || context.active_key !== 'chat') return
    const requested = context.view_state.session
    if (requested && requested !== activeID.current) void attempt((async () => {
      try {
        await openSession(requested)
      } catch (cause) {
        if (!isMissingChatSession(cause)) throw cause
        const items = await loadSessions(false, '')
        if (items[0]) await openSession(items[0].id)
        else await createSession()
      }
    })())
    else if (!requested && activeID.current) setViewState({ session: activeID.current })
  }, [context.active_key, context.view_state.session, loading]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (!active) return
    window.clearTimeout(draftTimer.current)
    draftTimer.current = window.setTimeout(() => {
      void saveDraft(active.id, { text: input, attachment_ids: attachments.map((item) => item.id) }).catch(reportError)
    }, 500)
    return () => window.clearTimeout(draftTimer.current)
  }, [active?.id, attachments, input])
  useEffect(() => {
    const list = listRef.current
    if (list) list.scrollTop = list.scrollHeight
  }, [active?.messages, streamText, active?.id])

  const refreshActive = useCallback(
    async (sessionID: string) => {
      const detail = await api<ChatSession>(`${apiBase}/sessions/${sessionID}`)
      if (activeID.current === sessionID) setActive(detail)
      await loadSessions()
    },
    [loadSessions],
  )
  // reconnect 探一次后端健康，恢复后清掉断连提示并刷新当前会话，让界面重新可用。
  // 手动「重连」按钮和自动重连共用它，避免两套逻辑。
  const reconnect = useCallback(async () => {
    if (reconnecting) return
    setReconnecting(true)
    try {
      if (!(await pingHealth())) return
      setConnectionLost(false)
      setError(undefined)
      const sessionID = activeID.current
      if (sessionID) await refreshActive(sessionID).catch(() => undefined)
    } finally {
      setReconnecting(false)
    }
  }, [reconnecting, refreshActive])
  // 断连后自动探活：带退避（3s 起，最多 15s），服务恢复即自动清除断连状态。
  useEffect(() => {
    if (!connectionLost) return
    let cancelled = false
    let timer: number
    let delay = 3000
    const tick = async () => {
      if (cancelled) return
      if (await pingHealth()) {
        if (cancelled) return
        setConnectionLost(false)
        setError(undefined)
        const sessionID = activeID.current
        if (sessionID) await refreshActive(sessionID).catch(() => undefined)
        return
      }
      if (cancelled) return
      delay = Math.min(delay + 3000, 15000)
      timer = window.setTimeout(() => void tick(), delay)
    }
    timer = window.setTimeout(() => void tick(), delay)
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [connectionLost, refreshActive])
  // Only poll a reply whose stream belongs to an earlier page load/tab.
  useEffect(() => {
    if (!active?.running || running.has(active.id)) return
    const sessionID = active.id
    let cancelled = false
    let timer: number
    const poll = async () => {
      try {
        const detail = await api<ChatSession>(`${apiBase}/sessions/${sessionID}`)
        if (cancelled || activeID.current !== sessionID) return
        setActive(detail)
        if (!detail.running) return
      } catch (cause) {
        if (cancelled) return
        reportError(cause)
        return
      }
      timer = window.setTimeout(() => void poll(), 2000)
    }
    timer = window.setTimeout(() => void poll(), 2000)
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [active?.id, active?.running, running])
  const send = useCallback(async () => {
    if (!active || active.running || running.has(active.id) || uploading || switching || saving || creating) return
    const text = input.trim()
    if (!text && !attachments.length) return
    const selectedModel = models[active.agent]?.find((item) => item.id === active.model)
    if (attachments.some((file) => file.mime_type.startsWith('image/')) && !selectedModel?.input_modalities?.includes('image')) {
      setError(`${agentLabel(active.agent)} 的 ${active.model} 未声明图片输入能力，请换一个支持图片的模型`)
      return
    }
    const sessionID = active.id
    setAccepting(sessionID)
    setError(undefined)
    setRunning((current) => new Set(current).add(sessionID))
    setStreamText((current) => ({ ...current, [sessionID]: '' }))
    try {
      window.clearTimeout(draftTimer.current)
      await saveDraft(sessionID, { text: input, attachment_ids: attachments.map((item) => item.id) })
      const response = await apiFetch(`${apiBase}/sessions/${sessionID}/messages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          message: text,
          attachment_ids: attachments.map((item) => item.id),
          sources: [],
        }),
      })
      // 服务重启时网关会用 HTML 错误页回应流式请求；识别后按断连处理，而非当成普通失败。
      if (looksLikeServiceRestart(response)) throw new ServiceUnavailableError('与服务的连接中断，可能正在重启', response.status)
      if (!response.ok || !response.body) throw new Error(`对话请求失败：HTTP ${response.status}`)
      const reader = response.body.getReader(),
        decoder = new TextDecoder()
      let buffer = '',
        streamError = ''
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        let separator: number
        while ((separator = buffer.indexOf('\n\n')) !== -1) {
          const block = buffer.slice(0, separator)
          buffer = buffer.slice(separator + 2)
          const parsed = parseSSEBlock(block)
          if (!parsed.data) continue
          if (parsed.event === 'accepted') {
            setAccepting(null)
            if (activeID.current === sessionID) {
              setInput('')
              setAttachments([])
              setActive((current) => current?.id === sessionID ? {
                ...current,
                messages: [...(current.messages || []), { id: `local-${Date.now()}`, role: 'user', text, attachments, created_at: new Date().toISOString() }],
              } : current)
            }
          } else if (parsed.event === 'delta') {
            const delta = (JSON.parse(parsed.data) as { text: string }).text
            setStreamText((current) => ({
              ...current,
              [sessionID]: (current[sessionID] || '') + delta,
            }))
          } else if (parsed.event === 'error') streamError = (JSON.parse(parsed.data) as { message: string }).message
        }
      }
      if (streamError) throw new Error(streamError)
    } catch (cause) {
      reportError(cause)
    } finally {
      await refreshActive(sessionID).catch(reportError)
      setAccepting(null)
      setRunning((current) => {
        const next = new Set(current)
        next.delete(sessionID)
        return next
      })
      setStreamText((current) => {
        const next = { ...current }
        delete next[sessionID]
        return next
      })
    }
  }, [active, attachments, context, input, models, refreshActive, running, uploading, switching, saving, creating, saveDraft])
  const stop = async () => {
    if (active) await api<{ canceled: boolean }>(`${apiBase}/sessions/${active.id}/cancel`, { method: 'POST' })
  }
  const uploadFiles = async (files: FileList | File[]) => {
    if (!active) return
    const sessionID = active.id
    const items = Array.from(files)
    if (attachments.length + items.length > 10) {
      setError('每条消息最多添加 10 个文件')
      return
    }
    if (items.some((file) => file.size > 12 * 1024 * 1024)) {
      setError('单个文件不能超过 12 MiB')
      return
    }
    setUploading(true)
    setError(undefined)
    try {
      const uploaded: ChatAttachment[] = []
      for (const file of items) {
        const form = new FormData()
        form.append('file', file)
        uploaded.push(await api<ChatAttachment>(`${apiBase}/sessions/${sessionID}/attachments`, { method: 'POST', body: form }))
      }
      if (activeID.current === sessionID) setAttachments((current) => [...current, ...uploaded])
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setUploading(false)
    }
  }
  const removeAttachment = async (file: ChatAttachment) => {
    if (!active) return
    try {
      await api<{ deleted: boolean }>(`${apiBase}/sessions/${active.id}/attachments/${file.id}`, { method: 'DELETE' })
      if (activeID.current === active.id) setAttachments((items) => items.filter((item) => item.id !== file.id))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }
  const updateSession = async (patch: Record<string, unknown>) => {
    if (!active) return
    setSaving(true)
    try {
      const detail = await api<ChatSession>(`${apiBase}/sessions/${active.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(patch),
      })
      if (activeID.current === detail.id) setActive(detail)
      await loadSessions()
    } finally { setSaving(false) }
  }
  const changeAgent = async (agent: string) => {
    if (!active || agent === active.agent) return
    if (active.running || running.has(active.id)) {
      setError('请先停止当前回复，再切换 Agent')
      return
    }
    try {
      const available = await loadModels(agent),
        model = available.find((item) => item.default) || available[0]
      if (!model) throw new Error(`${agentLabel(agent)} 没有可用模型`)
      if ((active.messages || []).length > 0)
        Modal.confirm({
          title: `切换到 ${agentLabel(agent)}`,
          content: '已有记录会复制到新会话，原会话保持不变。底层工具状态不会迁移。',
          okText: '携带记录新建',
          cancelText: '取消',
          onOk: () => createSession(agent, model.id, active.id),
        })
      else
        await updateSession({
          agent,
          model: model.id,
          reasoning_effort: defaultEffort(model),
        })
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }
  const changeModel = async (model: string) => {
    if (!active) return
    const item = models[active.agent]?.find((candidate) => candidate.id === model)
    await updateSession({
      model,
      reasoning_effort: item ? defaultEffort(item) : active.reasoning_effort,
    })
  }
  const doArchive = async () => {
    if (!active) return
    const showArchived = !active.archived
    await updateSession({ archived: showArchived })
    setArchived(showArchived)
    await loadSessions(showArchived, '')
  }
  const removeSession = async () => {
    if (!active) return
    Modal.confirm({
      title: '永久删除这个会话？',
      content: '会话记录会从 Jarvis 中删除，已经产生的外部动作不会撤销。',
      okText: '永久删除',
      okButtonProps: { danger: true },
      onOk: async () => {
        await api<{ deleted: boolean }>(`${apiBase}/sessions/${active.id}`, {
          method: 'DELETE',
        })
        window.clearTimeout(draftTimer.current)
        activeID.current = ''
        const items = await loadSessions()
        if (items[0]) await openSession(items[0].id)
        else await createSession()
      },
    })
  }
  const exportSession = () => {
    if (!active) return
    const content = [
      `# ${active.title}`,
      '',
      `Agent: ${agentLabel(active.agent)} / ${active.model}`,
      '',
      ...(active.messages || []).flatMap((item) => [`## ${item.role === 'user' ? '用户' : agentName}`, '', item.text, '', ...(item.attachments || []).map((file) => `- 附件：${file.name}`), '']),
    ].join('\n')
    const url = URL.createObjectURL(new Blob([content], { type: 'text/markdown' })),
      link = document.createElement('a')
    link.href = url
    link.download = `${active.title.replace(/[\\/:*?"<>|]/g, '-')}.md`
    link.click()
    setTimeout(() => URL.revokeObjectURL(url), 30_000)
  }

  const activeRunning = active ? running.has(active.id) || Boolean(active.running) : false
  const menuItems: MenuProps['items'] = [
    { key: 'export', icon: <DownloadOutlined />, label: '导出 Markdown' },
    {
      key: 'archive',
      icon: <InboxOutlined />,
      label: active?.archived ? '恢复会话' : '归档会话',
      disabled: activeRunning,
    },
    { type: 'divider' },
    {
      key: 'delete',
      icon: <DeleteOutlined />,
      danger: true,
      label: '永久删除',
      disabled: activeRunning,
    },
  ]
  const grouped = useMemo(() => {
    const result = new Map<string, ChatSession[]>()
    for (const session of sessions) {
      const group = dayGroup(session.updated_at)
      result.set(group, [...(result.get(group) || []), session])
    }
    return result
  }, [sessions])
  const currentModels = active ? models[active.agent] || [] : []
  const currentModel = currentModels.find((model) => model.id === active?.model)
  const modelOptions = [
    {
      label: '推荐浏览',
      options: currentModels.slice(0, 5).map((model) => ({ value: model.id, label: model.name })),
    },
    ...(currentModels.length > 5
      ? [
          {
            label: '全部模型',
            options: currentModels.slice(5).map((model) => ({ value: model.id, label: model.name })),
          },
        ]
      : []),
  ]
  const displayMessages = (active?.messages || []).filter((item) => item.status !== 'streaming'),
    currentStream = active ? streamText[active.id] || (active.running && !running.has(active.id) ? active.messages?.find((item) => item.status === 'streaming')?.text || '上一轮仍在回复，完成后自动更新…' : '') : ''
  if (hidden) return null

  if (compact && !expanded) {
    const latest = activeRunning
      ? { id: 'stream', role: 'assistant' as const, text: currentStream || '', created_at: '', agent: active?.agent, model: active?.model }
      : [...displayMessages].reverse().find((item) => item.role === 'assistant')
    return <ChatDock
      active={active} sessions={sessions} agents={agents} models={currentModels}
      input={input} onInput={setInput} attachments={attachments} uploading={uploading}
      loading={loading} busy={switching || saving || creating || accepting === active?.id} running={activeRunning}
      error={error} onDismissError={() => setError(undefined)}
      connectionLost={connectionLost} reconnecting={reconnecting} onReconnect={() => void reconnect()}
      replyText={activeRunning ? currentStream || '正在思考…' : latest?.text || (latest?.attachments?.length ? '已生成附件，点击查看' : '')}
      replyContent={latest && <ChatMessageCard message={latest} agentName={agentName} shortName={shortName} typing={activeRunning} />}
      onSend={() => void attempt(send())} onStop={() => void attempt(stop())}
      onNew={() => void attempt(createSession())}
      onOpenSession={(id) => void attempt(openSession(id))}
      onOpenHistory={() => isolated ? setExpanded(true) : navigate('chat', active ? { session: active.id } : {})}
      onRefreshSessions={() => { setArchived(false); setQuery(''); void attempt(loadSessions(false, '')) }}
      onAgent={(value) => void attempt(changeAgent(value))}
      onModel={(value) => void attempt(changeModel(value))}
      onRefreshModels={() => { if (active) void attempt(loadModels(active.agent, true)) }}
      onEffort={(value) => void attempt(updateSession({ reasoning_effort: value }))}
      onUpload={(files) => void uploadFiles(files)}
      onRemoveAttachment={(file) => void removeAttachment(file)}
    />
  }
  if (loading)
    return (
      <div className="chat-workspace-loading">
        <Spin />
        <span>正在加载对话…</span>
      </div>
    )

  const workspace = (
    <section className="chat-workspace" aria-label={`${agentName} 对话工作区`}>
      <aside className="chat-history-pane">
        <div className="chat-history-heading">
          <div>
            <strong>对话</strong>
            <span>你的思考与行动</span>
          </div>
        </div>
        <Button className="chat-new-button" icon={<PlusOutlined />} onClick={() => void attempt(createSession())}>
          新对话
        </Button>
        <Input
          allowClear
          prefix={<SearchOutlined />}
          value={query}
          placeholder="搜索会话和消息"
          onChange={(event) => {
            setQuery(event.target.value)
            void attempt(loadSessions(archived, event.target.value))
          }}
        />
        <div className="chat-session-list">
          {Array.from(grouped.entries()).map(([group, items]) => (
            <div key={group}>
              <div className="chat-history-group">{group}</div>
              {items.map((session) => (
                <button key={session.id} type="button" className={`chat-session-item ${session.id === active?.id ? 'is-active' : ''}`} onClick={() => void attempt(openSession(session.id))}>
                  <strong>{session.title}</strong>
                  <span>
                    {agentLabel(session.agent)} · {session.model}
                  </span>
                </button>
              ))}
            </div>
          ))}
          {!sessions.length && <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="没有匹配的会话" />}
        </div>
        <Button
          type="text"
          className="chat-archive-toggle"
          icon={<HistoryOutlined />}
          onClick={() => {
            const next = !archived
            setArchived(next)
            void attempt(loadSessions(next, query))
          }}
        >
          {archived ? '返回最近会话' : '查看已归档会话'}
        </Button>
      </aside>
      <div className="chat-main-pane">
        <header className="chat-workspace-header">
          <Button className="chat-mobile-history" type="text" icon={<MenuOutlined />} aria-label="打开会话历史" onClick={() => setHistoryOpen(true)} />
          <div className="chat-session-heading">
            {editingTitle && active ? (
              <Input
                autoFocus
                value={active.title}
                onChange={(event) => setActive({ ...active, title: event.target.value })}
                onPressEnter={() => {
                  setEditingTitle(false)
                  void attempt(updateSession({ title: active.title }))
                }}
                onBlur={() => {
                  setEditingTitle(false)
                  void attempt(updateSession({ title: active.title }))
                }}
              />
            ) : (
              <button type="button" onClick={() => setEditingTitle(true)}>
                <strong>{active?.title || '对话'}</strong>
                <EditOutlined />
              </button>
            )}
          </div>
          <Tooltip title="导出 Markdown">
            <Button type="text" icon={<DownloadOutlined />} onClick={exportSession} />
          </Tooltip>
          <Dropdown
            menu={{
              items: menuItems,
              onClick: ({ key }) => {
                if (key === 'export') exportSession()
                if (key === 'archive') void attempt(doArchive())
                if (key === 'delete') void removeSession()
              },
            }}
          >
            <Button type="text">•••</Button>
          </Dropdown>
        </header>
        <div className="chat-message-list" ref={listRef} role="log" aria-live="polite">
          {connectionLost && <Alert type="warning" showIcon title="与服务的连接中断，可能正在重启"
            description={reconnecting ? '正在重连…' : '正在自动重连，也可以手动重连。'}
            action={<Button size="small" loading={reconnecting} onClick={() => void reconnect()}>重连</Button>} />}
          {error && <Alert type="error" showIcon closable title="对话暂时遇到问题" description={error} onClose={() => setError(undefined)} />}
          {!displayMessages.length && !currentStream && (
            <div className="chat-welcome">
              <div className="chat-welcome-mark">✳</div>
              <h1>今天想一起推进什么？</h1>
              <p>从一个问题、一份资料，或者一个想做成的结果开始。</p>
              <div className="chat-welcome-suggestions">
                {SUGGESTIONS.map(([label, text]) => (
                  <button
                    type="button"
                    key={label}
                    onClick={() => {
                      setInput(text)
                      inputRef.current?.focus()
                    }}
                  >
                    <strong>{label}</strong>
                    <span>{text}</span>
                  </button>
                ))}
              </div>
            </div>
          )}
          {displayMessages.map((item) => (
            <ChatMessageCard key={item.id} message={item} agentName={agentName} shortName={shortName} />
          ))}
              {activeRunning && active && (
            <ChatMessageCard
              message={{
                id: 'stream',
                role: 'assistant',
                text: currentStream,
                agent: active.agent,
                model: active.model,
                created_at: new Date().toISOString(),
              }}
              agentName={agentName}
              shortName={shortName}
              typing
            />
          )}
        </div>
        <div
          className="chat-composer-area"
          onDragOver={(event) => event.preventDefault()}
          onDrop={(event) => {
            event.preventDefault()
            void uploadFiles(event.dataTransfer.files)
          }}
        >
          <div className="chat-composer-card">
            {!!attachments.length && (
              <div className="chat-pending-files">
                {attachments.map((file) => (
                  <FileCard key={file.id} file={file} removable onRemove={() => void removeAttachment(file)} />
                ))}
              </div>
            )}
            <Input.TextArea
              ref={inputRef}
              disabled={switching || creating || accepting === active?.id}
              value={input}
              onChange={(event) => setInput(event.target.value)}
              autoSize={{ minRows: 2, maxRows: 8 }}
              placeholder="问一个问题，或告诉我你想推进什么…"
              onPaste={(event) => {
                if (event.clipboardData.files.length) {
                  event.preventDefault()
                  void uploadFiles(event.clipboardData.files)
                }
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                  event.preventDefault()
                  void attempt(send())
                }
              }}
            />
            <div className="chat-composer-actions">
              <div>
                <input
                  ref={fileRef}
                  hidden
                  multiple
                  type="file"
                  onChange={(event) => {
                    if (event.target.files) void uploadFiles(event.target.files)
                    event.target.value = ''
                  }}
                />
                <Tooltip title="添加图片或文件">
                  <Button loading={uploading} icon={<PaperClipOutlined />} onClick={() => fileRef.current?.click()} />
                </Tooltip>
                <Select
                  value={active?.agent}
                  className="chat-agent-select"
                  disabled={activeRunning}
                  onChange={(value) => void changeAgent(value)}
                  options={agents.map((agent) => ({
                    value: agent.id,
                    label: agent.available ? agent.name : `${agent.name} · ${agent.error || '不可用'}`,
                    disabled: !agent.available,
                  }))}
                />
                <Select
                  showSearch
                  value={active?.model}
                  className="chat-model-select"
                  disabled={activeRunning}
                  optionFilterProp="label"
                  loading={!!active && !models[active.agent]}
                  onOpenChange={(open) => {
                    if (open && active) void loadModels(active.agent, true).catch((cause) => setError(cause instanceof Error ? cause.message : String(cause)))
                  }}
                  onChange={(value) => void attempt(changeModel(value))}
                  options={modelOptions}
                />
                {!!currentModel?.reasoning_efforts?.length && (
                  <Select
                    value={active?.reasoning_effort}
                    className="chat-effort-select"
                    disabled={activeRunning}
                    onChange={(value) => void attempt(updateSession({ reasoning_effort: value }))}
                    options={currentModel.reasoning_efforts.map((value) => ({ value, label: value }))}
                  />
                )}
              </div>
              {activeRunning ? (
                <Button danger icon={<StopOutlined />} onClick={() => void attempt(stop())}>
                  停止
                </Button>
              ) : (
                <Button type="primary" icon={<SendOutlined />} disabled={!input.trim() && !attachments.length} onClick={() => void attempt(send())}>
                  发送
                </Button>
              )}
            </div>
          </div>
          <div className="chat-composer-hint">Enter 发送 · Shift + Enter 换行 · 可粘贴或拖入文件</div>
        </div>
      </div>
      <Drawer title="会话历史" placement="left" size="min(320px, 88vw)" open={historyOpen} onClose={() => setHistoryOpen(false)}>
        <Button block icon={<PlusOutlined />} onClick={() => void attempt(createSession())}>
          新对话
        </Button>
        <div className="chat-drawer-sessions">
          {sessions.map((session) => (
            <button key={session.id} onClick={() => void attempt(openSession(session.id))}>
              <strong>{session.title}</strong>
              <span>
                {agentLabel(session.agent)} · {session.model}
              </span>
            </button>
          ))}
        </div>
      </Drawer>
    </section>
  )
  return isolated && compact ? <Modal open width="95vw" footer={null} title="OKR 独立会话" onCancel={() => setExpanded(false)}>{workspace}</Modal> : workspace
}

function ChatMessageCard({ message, agentName, shortName, typing = false }: { message: ChatHistoryMessage; agentName: string; shortName: string; typing?: boolean }) {
  return (
    <article className={`chat-message-card ${message.role}`}>
      {message.role === 'assistant' && (
        <div className="chat-assistant-label">
          <span>{shortName}</span>
          <strong>{agentName}</strong>
          <small>
            {message.agent && agentLabel(message.agent)}
            {message.model && ` · ${message.model}`}
          </small>
        </div>
      )}
      <div className="chat-message-body">
        {typing && !message.text ? (
          <span className="chat-typing">
            正在思考<span>•••</span>
          </span>
        ) : message.role === 'assistant' ? (
          <MarkdownReport content={message.text} />
        ) : (
          <div className="chat-user-text">{message.text}</div>
        )}
      </div>
      {message.status === 'interrupted' && <Text type="secondary">回复已中断，以上为已保存内容。</Text>}
      {message.error && <Text type="danger">{message.error}</Text>}
      {!!message.attachments?.length && (
        <div className="chat-message-files">
          {message.attachments.map((file) => (
            <FileCard key={file.id} file={file} />
          ))}
        </div>
      )}
    </article>
  )
}
function FileCard({ file, removable, onRemove }: { file: ChatAttachment; removable?: boolean; onRemove?: () => void }) {
  const apiBase = useContext(ChatAPIContext)
  const href = appPath(`${apiBase}/attachments/${file.id}/content`)
  return (
    <div className="chat-file-card">
      <a href={href} target="_blank" rel="noreferrer">
        {file.mime_type.startsWith('image/') ? <img src={href} alt="" /> : <FileOutlined />}
        <span>
          <strong>{file.name}</strong>
          <small>{formatBytes(file.size_bytes)} · 下载</small>
        </span>
      </a>
      {removable && (
        <button type="button" aria-label={`移除 ${file.name}`} onClick={onRemove}>
          ×
        </button>
      )}
    </div>
  )
}
