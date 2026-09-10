import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Alert, Button, Input, QRCode, Spin, Typography } from 'antd'
import { LinkOutlined, LoadingOutlined } from '@ant-design/icons'
import {
  beginSetupAgentLogin, beginSetupLarkConnection, beginSetupLarkLogin, bootstrapSetupWorldModel,
  cancelSetupFlow, finalizeSetup, getSetupFlow, getSetupStatus, getTask, loginWithByteDance, resumeTask,
} from './api'
import type { SetupFlow, SetupStatus, Task } from './types'
import { setupAction, setupTaskStopped } from './onboardingState'
import { questionOf } from './tasks/taskPresentation'
import { DeveloperDocumentLinks } from './components/DeveloperDocuments'
import jarvisIcon from './assets/jarvis-icon.png'

const taskKey = 'jarvis.onboardingTaskId'
const restartKey = 'jarvis.onboardingRestartFrom'
const errorText = (cause: unknown) => cause instanceof Error ? cause.message : String(cause)

export function OnboardingGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [appSecret, setAppSecret] = useState('')
  const [editingSecret, setEditingSecret] = useState(false)
  const [flow, setFlow] = useState<SetupFlow | null>(null)
  const [pausedTask, setPausedTask] = useState<Task | null>(null)
  const [taskResponse, setTaskResponse] = useState('')
  const [taskId, setTaskId] = useState<number | null>(() => {
    const value = Number(localStorage.getItem(taskKey))
    return Number.isSafeInteger(value) && value > 0 ? value : null
  })
  const running = useRef(false)
  const recoveryAttempted = useRef(false)

  const load = useCallback(async () => {
    const next = await getSetupStatus()
    if (next.completed) { localStorage.removeItem(taskKey); localStorage.removeItem(restartKey) }
    setStatus(next)
    return next
  }, [])

  const refresh = useCallback(async () => {
    setError(''); setBusy('正在检查已有配置…')
    try { await load() } catch (cause) { setError(errorText(cause)) }
    finally { setLoading(false); setBusy('') }
  }, [load])
  useEffect(() => { void refresh() }, [refresh])

  useEffect(() => {
    if (!flow?.id || flow.status !== 'pending') return
    let cancelled = false
    let failures = 0
    let timer: number
    const poll = async () => {
      try {
        const next = await getSetupFlow(flow.id)
        if (cancelled) return
        if (next.status !== 'pending') {
          if (next.status === 'success') await load()
          else setError(next.error || '操作未完成，请重试')
          setBusy(''); setFlow(null)
          return
        }
        failures = 0; setFlow(next)
      } catch (cause) {
        if (cancelled) return
        if (++failures >= 3) {
          setBusy(''); setFlow(null)
          setError(`连接进度读取失败：${errorText(cause)}。请重新检查，已有授权不会丢失。`)
          return
        }
      }
      if (!cancelled) timer = window.setTimeout(() => void poll(), 1500)
    }
    timer = window.setTimeout(() => void poll(), 1500)
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [flow?.id, flow?.status, load])

  useEffect(() => {
    if (!taskId) return
    let cancelled = false
    let failures = 0
    let timer: number
    const poll = async () => {
      try {
        const task = await getTask(taskId)
        if (cancelled) return
        failures = 0
        if (setupTaskStopped(task.status)) {
          setBusy(''); setTaskId(null)
          if (task.status === 'needs_human') {
            setPausedTask(task); setError('')
          } else {
            localStorage.removeItem(taskKey)
            if (task.status === 'done') {
              setError('')
              try { await load() } catch (cause) { setError(`初始化已完成，请重新检查：${errorText(cause)}`) }
            } else setError(task.summary || '初始化未完成，请重试；已经完成的配置会保留。')
          }
          return
        }
      } catch (cause) {
        if (cancelled) return
        if (++failures >= 3) {
          setTaskId(null); setBusy('')
          setError(`进度读取失败：${errorText(cause)}。点击重试继续检查原任务。`)
          return
        }
      }
      if (!cancelled) timer = window.setTimeout(() => void poll(), 2500)
    }
    void poll()
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [taskId, load])

  // One click saves configuration, waits for the restarted runtime, and starts
  // initialization. Only a runtime ID/task ID is persisted, never credentials.
  const start = useCallback(async () => {
    if (running.current) return
    running.current = true
    setError(''); setBusy('正在准备服务…')
    try {
      let current = await load()
      if (current.completed) return
      if (setupAction(current) !== 'start') return
      let restartFrom = localStorage.getItem(restartKey)
      if (!current.configuration.machine_configuration_ready && !restartFrom) {
        const result = await finalizeSetup(appSecret)
        restartFrom = result.runtime_id
        localStorage.setItem(restartKey, restartFrom)
        setAppSecret(''); setEditingSecret(false)
      }
      if (restartFrom) {
        setBusy('正在启动服务…')
        let resumed = false
        for (let attempt = 0; attempt < 30; attempt += 1) {
          try {
            await loginWithByteDance()
            current = await load()
            if (current.runtime_id !== restartFrom && current.configuration.machine_configuration_ready) { resumed = true; break }
          } catch { /* The runtime restart temporarily interrupts HTTP. */ }
          await new Promise((resolve) => window.setTimeout(resolve, 1000))
        }
        if (!resumed) throw new Error('配置已保存，服务尚未恢复。稍后点击重试，无需重新填写。')
      }
      if (!current.configuration.machine_configuration_ready) throw new Error('本机配置尚未完成，请检查后重试。')
      setBusy('正在初始化工作背景…')
      const id = (await bootstrapSetupWorldModel()).task_id
      localStorage.setItem(taskKey, String(id))
      localStorage.removeItem(restartKey)
      setTaskId(id)
    } catch (cause) { setError(errorText(cause)) }
    finally { running.current = false; setBusy('') }
  }, [appSecret, load])

  useEffect(() => {
    if (!status || loading || taskId || recoveryAttempted.current || !localStorage.getItem(restartKey)) return
    recoveryAttempted.current = true
    void start()
  }, [status, loading, taskId, start])

  const beginFlow = async (kind: 'connect' | 'authorize' | 'agent') => {
    setError(''); setBusy('正在打开连接页面…')
    try {
      const next = await (kind === 'connect' ? beginSetupLarkConnection() : kind === 'authorize' ? beginSetupLarkLogin() : beginSetupAgentLogin())
      if (next.status === 'pending') setFlow(next)
      else if (next.status === 'success') await load()
      else setError(next.error || '操作未完成')
    } catch (cause) { setError(errorText(cause)) }
    finally { setBusy('') }
  }

  const cancelFlow = async () => {
    if (!flow?.id) return
    try { await cancelSetupFlow(flow.id); setFlow(null); await load() }
    catch (cause) { setError(errorText(cause)) }
  }

  const answerTask = async () => {
    if (!pausedTask) return
    setBusy('正在继续初始化…'); setError('')
    try {
      await resumeTask(pausedTask.id, pausedTask.version, taskResponse.trim())
      setTaskId(pausedTask.id); setPausedTask(null); setTaskResponse('')
    } catch (cause) { setError(errorText(cause)) }
    finally { setBusy('') }
  }

  if (loading) return <main className="setup-loading"><Spin size="small" /><span>正在检查已有配置…</span></main>
  if (!status) return <main className="setup-page"><section className="setup-shell"><Alert type="error" showIcon message="无法读取安装状态" description={error} /><Button onClick={() => void refresh()} loading={Boolean(busy)}>重新检查</Button></section></main>
  if (status.completed) return children

  const action = setupAction(status)
  const locked = Boolean(busy || taskId || flow || pausedTask)
  const secretEditorVisible = editingSecret || (!status.configuration.machine_configuration_ready && !status.lark.credential_available)
  const flowURL = flow?.verification_url || flow?.output?.match(/https?:\/\/\S+/)?.[0]
  const appLabel = status.lark.app_name || status.lark.app_id

  return <main className="setup-page"><section className="setup-shell setup-simple">
    <header className="setup-header"><img src={jarvisIcon} alt="" /><div><Typography.Title level={2}>开始使用 Jarvis</Typography.Title><Typography.Text type="secondary">已有配置自动复用，只需补齐缺少的连接。</Typography.Text></div></header>
    {appLabel && <p className="setup-connected">飞书助手：{appLabel}{status.lark.user.name ? ` · ${status.lark.user.name}` : ''}</p>}
    <div className="setup-form">
      {!flow && !taskId && !pausedTask && <>
        {action === 'connect' && <Button type="primary" disabled={locked || Boolean(status.lark.error)} onClick={() => void beginFlow('connect')}>连接飞书</Button>}
        {action === 'repair' && <Alert type="warning" showIcon message="当前飞书应用尚未就绪" description="请检查当前应用的凭据、机器人能力及发布状态，再点击重新检查；无需创建另一个 Bot。" />}
        {action === 'authorize' && <Button type="primary" disabled={locked} onClick={() => void beginFlow('authorize')}>授权飞书账号</Button>}
        {action === 'agent' && <Button type="primary" disabled={locked} onClick={() => void beginFlow('agent')}>登录 Agent</Button>}
        {action === 'start' && <>
          {secretEditorVisible && <div className="setup-field">
            <label htmlFor="setup-secret">App Secret</label>
            <Input.Password id="setup-secret" value={appSecret} disabled={locked} autoComplete="off" placeholder="补填当前飞书应用的密钥" onChange={(event) => { setAppSecret(event.target.value); setEditingSecret(true) }} />
            <details className="setup-help"><summary>在哪里找？</summary><p>打开<a href="https://open.feishu.cn/app" target="_blank" rel="noreferrer">飞书开发者后台</a>，进入当前应用「{appLabel}」（{status.lark.app_id}）的「凭证与基础信息」，复制 App Secret。不是个人密码，也不是 Webhook。</p><p>只需填写一次，不要另建应用；验证失败会保留输入。</p></details>
          </div>}
          <Button type="primary" disabled={locked || (secretEditorVisible && !appSecret.trim())} onClick={() => void start()}>{error ? '重试并继续' : '开始使用'}</Button>
        </>}
      </>}
      {busy && <span className="setup-running"><LoadingOutlined /> {busy}</span>}
      {taskId && <span className="setup-running"><LoadingOutlined /> 正在初始化，完成后自动进入</span>}
      {flow && <>
        <Typography.Text>请在打开的页面完成操作，完成后这里会自动继续。</Typography.Text>
        {flow.user_code && <Typography.Text code>{flow.user_code}</Typography.Text>}
        {flowURL ? <><Button href={flowURL} target="_blank" rel="noreferrer" icon={<LinkOutlined />}>打开连接页面</Button><QRCode value={flowURL} size={144} /></> : <Spin size="small" />}
        <Button aria-label="返回" onClick={() => void cancelFlow()}>返回</Button>
      </>}
      {pausedTask && <><Alert type="info" showIcon message={questionOf(pausedTask)?.title || '初始化需要你的补充'} description={questionOf(pausedTask)?.body || pausedTask.summary} /><Input.TextArea aria-label="补充说明" value={taskResponse} onChange={(event) => setTaskResponse(event.target.value)} /><Button type="primary" disabled={Boolean(busy) || !taskResponse.trim()} onClick={() => void answerTask()}>回复并继续</Button></>}
    </div>
    {(error || status.lark.error || status.agent.error) && <Alert type="error" showIcon message="本次操作未完成" description={error || status.lark.error || status.agent.error} />}
    {!locked && <Button type="link" onClick={() => void refresh()}>重新检查</Button>}
    <details className="setup-help"><summary>连接说明与帮助</summary>
      <p>全程使用 lark-cli 已连接的同一个飞书应用。Bot 是它的聊天身份；个人授权也是授权给这个应用，不是第二个 Bot。已完成的授权和配置自动跳过。</p>
      <p>若提示机器人或权限未就绪，在<a href="https://open.feishu.cn/app" target="_blank" rel="noreferrer">飞书开发者后台</a>检查当前应用：启用机器人、开通消息权限、使用长连接订阅 im.message.receive_v1 和 card.action.trigger，再发布版本。没有操作权限时请联系管理员。</p>
      <p>默认助手名称为 Jarvis，之后可在设置中修改。点击开始使用后，会自动保存配置、启动服务并初始化工作背景。</p>
      <p>同一个 Bot 不应同时在其他机器运行聊天长连接；若此前装过，请先停止旧实例。完成后可在飞书给这个 Bot 发一条消息验证回复。</p>
      {action === 'start' && status.lark.credential_available && !status.configuration.machine_configuration_ready && <Button disabled={locked} onClick={() => setEditingSecret(true)}>补填当前应用密钥</Button>}
      {status.lark.app_id && (error || status.lark.error) && <Button disabled={locked} onClick={() => void beginFlow('authorize')}>重新授权当前应用</Button>}
    </details>
    <details className="setup-help setup-documents"><summary>开发文档</summary><DeveloperDocumentLinks /></details>
  </section></main>
}
