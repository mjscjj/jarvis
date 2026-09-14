import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { ArrowUpOutlined, CheckOutlined, CloseOutlined, DownOutlined, ExpandAltOutlined, MessageOutlined, MoreOutlined, PlusOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Input, Popover, Select, Spin, Tooltip } from 'antd'
import type { TextAreaRef } from 'antd/es/input/TextArea'
import type { ChatAgent, ChatAttachment, ChatModel, ChatSession } from '../types'
import '../styles/chat-dock.css'

interface ChatDockProps {
  active: ChatSession | null
  sessions: ChatSession[]
  agents: ChatAgent[]
  models: ChatModel[]
  input: string
  onInput: (value: string) => void
  attachments: ChatAttachment[]
  uploading: boolean
  loading: boolean
  busy: boolean
  running: boolean
  error?: string
  onDismissError: () => void
  connectionLost: boolean
  reconnecting: boolean
  onReconnect: () => void
  replyText: string
  replyContent: ReactNode
  onSend: () => void
  onStop: () => void
  onNew: () => void
  onOpenSession: (id: string) => void
  onOpenHistory: () => void
  onRefreshSessions: () => void
  onAgent: (value: string) => void
  onModel: (value: string) => void
  onRefreshModels: () => void
  onEffort: (value: string) => void
  onUpload: (files: FileList | File[]) => void
  onRemoveAttachment: (file: ChatAttachment) => void
}

// Presentation only: both chat surfaces are driven by the same mounted Chat.
export default function ChatDock(props: ChatDockProps) {
  const [expanded, setExpanded] = useState(true)
  const [panel, setPanel] = useState<'sessions' | 'agent' | 'model' | 'settings' | 'reply' | null>(null)
  const dockRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<TextAreaRef>(null)
  const fileRef = useRef<HTMLInputElement>(null)
  const { active, models, running, busy, loading, uploading } = props
  const disabled = loading || busy || !active
  const settingsDisabled = disabled || running || uploading
  const model = models.find((item) => item.id === active?.model)
  const modelName = model?.name || active?.model || '选择模型'
  const agentName = props.agents.find((agent) => agent.id === active?.agent)?.name || active?.agent || 'Agent'
  const sessionID = active?.id

  useEffect(() => { setPanel(null) }, [sessionID])
  useEffect(() => {
    const dock = dockRef.current
    const shell = dock?.closest<HTMLElement>('.app-shell')
    if (!dock || !shell) return
    const observer = new ResizeObserver(() => shell.style.setProperty('--chat-dock-height', `${dock.offsetHeight}px`))
    observer.observe(dock)
    return () => { observer.disconnect(); shell.style.removeProperty('--chat-dock-height') }
  }, [])

  const popover = (name: NonNullable<typeof panel>) => ({
    trigger: 'click' as const,
    classNames: { root: 'chat-dock-popover' },
    open: panel === name,
    onOpenChange: (open: boolean) => {
      setPanel(open ? name : null)
      if (open && name === 'sessions') props.onRefreshSessions()
      if (open && name === 'model') props.onRefreshModels()
    },
  })
  const agentPicker = <div className="chat-dock-agents">
    <div className="chat-dock-panel-caption">选择 Agent</div>
    {props.agents.map((agent) => <button type="button" key={agent.id} className="chat-dock-agent-option"
      aria-pressed={active?.agent === agent.id} disabled={settingsDisabled || !agent.available}
      onClick={() => { setPanel(null); props.onAgent(agent.id) }}>
      <span>{agent.name}</span>{!agent.available ? <small>不可用</small> : active?.agent === agent.id && <CheckOutlined />}
    </button>)}
    <div className="chat-dock-panel-caption">切换 Agent 后，模型随之更新</div>
  </div>
  const sessions = <div className="chat-dock-sessions">
    <Button type="text" block icon={<PlusOutlined />} disabled={disabled || uploading} onClick={() => { setPanel(null); props.onNew() }}>新对话</Button>
    <div className="chat-dock-panel-caption">最近会话</div>
    {props.sessions.filter((item) => !item.archived).slice(0, 6).map((session) => <button
      type="button" className="chat-dock-session" key={session.id} disabled={busy || uploading}
      onClick={() => { setPanel(null); props.onOpenSession(session.id) }}
      aria-current={active?.id === session.id ? 'true' : undefined}
    ><span>{session.title}</span>{active?.id === session.id && <CheckOutlined />}</button>)}
    <Button type="text" block icon={<ExpandAltOutlined />} onClick={props.onOpenHistory}>全部会话与历史</Button>
  </div>
  const settings = <div className="chat-dock-settings">
    {!!model?.reasoning_efforts?.length && <label>推理强度<Select aria-label="底部对话推理强度" value={active?.reasoning_effort} disabled={settingsDisabled}
      options={model.reasoning_efforts.map((value) => ({ value, label: value }))} onChange={props.onEffort} /></label>}
  </div>
  const reply = <div className="chat-dock-reply">
    <div className="chat-dock-reply-heading"><span>{running ? '正在回复' : '最新回复'}</span><Button type="text" size="small" aria-label="关闭回复全文" icon={<CloseOutlined />} onClick={() => setPanel(null)} /></div>
    <div className="chat-dock-reply-body">{props.replyContent}</div>
    <Button type="link" size="small" icon={<ExpandAltOutlined />} onClick={props.onOpenHistory}>在对话页继续</Button>
  </div>

  return <section className={`chat-dock ${expanded ? 'is-expanded' : 'is-collapsed'} ${running ? 'is-running' : ''}`} aria-label="底部快捷对话">
    <div className="chat-dock-inner" ref={dockRef} onKeyDown={(event) => {
      if (event.key === 'Escape') { setPanel(null); event.stopPropagation() }
    }}>
      {props.connectionLost && <Alert className="chat-dock-error" type="warning" showIcon title="连接中断，可能正在重启"
        description={props.reconnecting ? '正在重连…' : '正在自动重连，也可手动重连。'}
        action={<Button size="small" loading={props.reconnecting} onClick={props.onReconnect}>重连</Button>} />}
      {props.error && <Alert className="chat-dock-error" type="error" showIcon closable title="对话遇到问题" description={props.error} onClose={props.onDismissError} />}
      {!!props.replyText && <Popover {...popover('reply')} placement="top" content={reply}>
        <button type="button" className="chat-dock-preview" aria-label="查看最新回复全文">
          <span className="chat-dock-orb" aria-hidden="true" />
          <span className="chat-dock-preview-text">{props.replyText.replace(/\s+/g, ' ')}</span>
          <ExpandAltOutlined />
        </button>
      </Popover>}
      <span className="chat-dock-live" role="status">{running ? '正在回复，可打开全文查看' : ''}</span>
      {!expanded ? <button type="button" className="chat-dock-handle" aria-label="展开底部对话输入" aria-expanded="false" onClick={() => {
        setExpanded(true)
        requestAnimationFrame(() => inputRef.current?.focus({ cursor: 'end' }))
      }}><span /></button> :
        <div className="chat-dock-composer" onDragOver={(event) => event.preventDefault()} onDrop={(event) => {
          event.preventDefault()
          if (!disabled && !uploading) props.onUpload(event.dataTransfer.files)
        }}>
          {!!props.attachments.length && <div className="chat-dock-files">{props.attachments.map((file) => <span key={file.id}>
            <span title={file.name}>{file.name}</span><button type="button" disabled={disabled || uploading} aria-label={`移除 ${file.name}`} onClick={() => props.onRemoveAttachment(file)}><CloseOutlined /></button>
          </span>)}</div>}
          <div className="chat-dock-entry">
            <Input.TextArea ref={inputRef} value={props.input} onChange={(event) => props.onInput(event.target.value)}
              aria-label="底部对话输入" autoSize={{ minRows: 1, maxRows: 4 }} disabled={disabled}
              placeholder={loading ? '正在加载对话…' : running ? '可先输入下一条，回复结束后发送…' : '问一句，或交代一件事…'}
              onPaste={(event) => {
                if (event.clipboardData.files.length && !disabled && !uploading) { event.preventDefault(); props.onUpload(event.clipboardData.files) }
              }}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); if (!running && !uploading && !disabled) props.onSend() }
              }} />
            {running ? <button type="button" className="chat-dock-send" aria-label="停止回复" onClick={props.onStop}><StopOutlined /></button> :
              <button type="button" className="chat-dock-send" aria-label="发送消息" title="Enter 发送 · Shift+Enter 换行" disabled={disabled || uploading || (!props.input.trim() && !props.attachments.length)} onClick={props.onSend}><ArrowUpOutlined /></button>}
          </div>
          <div className="chat-dock-toolbar">
            <input type="file" hidden multiple ref={fileRef} onChange={(event) => { if (event.target.files) props.onUpload(event.target.files); event.target.value = '' }} />
            <Tooltip title="添加图片或文件"><button type="button" className="chat-dock-icon" aria-label="添加图片或文件" disabled={disabled || uploading} onClick={() => fileRef.current?.click()}>{uploading ? <Spin size="small" /> : <PlusOutlined />}</button></Tooltip>
            <Popover {...popover('sessions')} placement="topLeft" content={sessions}>
              <button type="button" className="chat-dock-session-trigger" aria-label="切换会话" title={active?.title || '切换会话'} disabled={loading || busy || uploading}><MessageOutlined /><span>{active?.title || '会话'}</span><DownOutlined /></button>
            </Popover>
            <div className="chat-dock-toolbar-spacer" />
            <Popover {...popover('agent')} placement="topRight" content={agentPicker}>
              <button type="button" className="chat-dock-agent-trigger" aria-label={`切换 Agent：${agentName}`} title={`当前 Agent：${agentName}`} disabled={settingsDisabled}>
                <span>{agentName}</span><DownOutlined />
              </button>
            </Popover>
            <Popover {...popover('model')} placement="topRight" content={<div className="chat-dock-model-picker"><div className="chat-dock-panel-caption">模型</div><Select
              aria-label="底部对话模型" showSearch optionFilterProp="label" value={active?.model} loading={!models.length}
              disabled={settingsDisabled} options={models.map((item) => ({ value: item.id, label: item.name }))}
              onChange={(value) => { props.onModel(value); setPanel(null) }} /></div>}>
              <button type="button" className="chat-dock-model-trigger" aria-label={`切换模型：${modelName}`} title={modelName} disabled={settingsDisabled}><span>{modelName}</span><DownOutlined /></button>
            </Popover>
            {!!model?.reasoning_efforts?.length && <Popover {...popover('settings')} placement="topRight" content={settings}><button type="button" className="chat-dock-icon" aria-label="更多对话设置" disabled={disabled}><MoreOutlined /></button></Popover>}
            <Tooltip title="收起输入"><button type="button" className="chat-dock-icon" aria-label="收起底部对话输入" onClick={() => { setPanel(null); setExpanded(false) }}><DownOutlined /></button></Tooltip>
          </div>
        </div>}
    </div>
  </section>
}
