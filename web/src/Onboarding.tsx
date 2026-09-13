import { useCallback, useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Alert, Button, Drawer, Input, QRCode, Spin, Typography } from 'antd'
import { LinkOutlined, LoadingOutlined } from '@ant-design/icons'
import {
  beginSetupAgentLogin, beginSetupLarkConnection, beginSetupLarkLogin,
  cancelSetupFlow, finalizeSetup, getSetupBootstrap, getSetupFlow, getSetupStatus, repairSetupLarkCredentials,
} from './api'
import type { SetupFlow, SetupStatus } from './types'
import { setupAction, setupCanEnter, setupSecretVisible } from './onboardingState'
import { SetupLarkApplication, larkApplicationURL } from './SetupLarkApplication'
import { WorldModelSetup } from './WorldModelSetup'
import jarvisIcon from './assets/jarvis-icon.png'

const restartKey = 'jarvis.onboardingRestartFrom'
const errorText = (cause: unknown) => cause instanceof Error ? cause.message : String(cause)
type FlowKind = 'connect' | 'authorize' | 'agent'

export function OnboardingGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [localReady, setLocalReady] = useState(false)
  const [repairOpen, setRepairOpen] = useState(false)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [appSecret, setAppSecret] = useState('')
  const [editingSecret, setEditingSecret] = useState(false)
  const [flow, setFlow] = useState<SetupFlow | null>(null)
  const [flowKind, setFlowKind] = useState<FlowKind | null>(null)
  const [restartFrom, setRestartFrom] = useState<string | null>(() => localStorage.getItem(restartKey))
  const running = useRef(false)
  const recoveryAttempted = useRef(false)
  const flowIDRef = useRef<string | null>(null)
  const flowBusy = useRef(false)

  const load = useCallback(async () => {
    const next = await getSetupStatus()
    const previousRuntime = localStorage.getItem(restartKey)
    if (previousRuntime && setupCanEnter(next, previousRuntime)) {
      localStorage.removeItem(restartKey)
      setRestartFrom(null)
    }
    setStatus(next)
    if (setupCanEnter(next, localStorage.getItem(restartKey))) setRepairOpen(false)
    return next
  }, [])

  const refresh = useCallback(async () => {
    setError(''); setBusy('正在检查已有配置…')
    try { await load() } catch (cause) { setError(errorText(cause)) }
    finally { setLoading(false); setBusy('') }
  }, [load])
  useEffect(() => {
    const controller = new AbortController()
    void getSetupBootstrap(controller.signal).then(next => {
      if (controller.signal.aborted) return
      // Installation/restart recovery still waits for the complete check.
      if (next.machine_configuration_ready && !localStorage.getItem(restartKey)) {
        setLocalReady(true)
        setLoading(false)
      }
      void refresh()
    }).catch(cause => {
      if (!controller.signal.aborted) { setError(errorText(cause)); setLoading(false) }
    })
    return () => controller.abort()
  }, [refresh])

  useEffect(() => {
    if (!flow?.id || flow.status !== 'pending') return
    flowIDRef.current = flow.id
    let cancelled = false
    let failures = 0
    let timer: number
    const poll = async () => {
      try {
        const next = await getSetupFlow(flow.id)
        if (cancelled) return
        if (flowIDRef.current !== flow.id) return
        if (next.status !== 'pending') {
          if (next.status === 'success') await load()
          else setError(next.error || '操作未完成，请重试')
          if (cancelled || flowIDRef.current !== flow.id) return
          setBusy(''); flowIDRef.current = null; setFlow(null); setFlowKind(null)
          return
        }
        failures = 0; setFlow(next)
      } catch (cause) {
        if (cancelled) return
        if (flowIDRef.current !== flow.id) return
        if (++failures >= 3) {
          setBusy(''); flowIDRef.current = null; setFlow(null); setFlowKind(null)
          setError(`连接进度读取失败：${errorText(cause)}。请重新检查，已有授权不会丢失。`)
          return
        }
      }
      if (!cancelled) timer = window.setTimeout(() => void poll(), 1500)
    }
    timer = window.setTimeout(() => void poll(), 1500)
    return () => { cancelled = true; window.clearTimeout(timer) }
  }, [flow?.id, flow?.status, load])

  // Installation only waits for a usable runtime, never for world modeling.
  const start = useCallback(async () => {
    if (running.current) return
    running.current = true
    setError(''); setBusy('正在准备服务…')
    try {
      let current = await load()
      if (setupCanEnter(current, localStorage.getItem(restartKey))) return
      if (setupAction(current) !== 'start') return
      let previousRuntime = localStorage.getItem(restartKey)
      if (!current.app_ready && !previousRuntime) {
        const result = await finalizeSetup(appSecret)
        previousRuntime = result.runtime_id
        localStorage.setItem(restartKey, previousRuntime)
        setRestartFrom(previousRuntime)
        setAppSecret(''); setEditingSecret(false)
      }
      if (previousRuntime) {
        setBusy('正在启动服务…')
        let resumed = false
        for (let attempt = 0; attempt < 30; attempt += 1) {
          try {
            current = await load()
            if (setupCanEnter(current, previousRuntime)) { resumed = true; break }
          } catch { /* The runtime restart temporarily interrupts HTTP. */ }
          await new Promise((resolve) => window.setTimeout(resolve, 1000))
        }
        if (!resumed) throw new Error('配置已保存，服务尚未恢复。稍后点击重试，无需重新填写。')
      }
      if (!current.configuration.machine_configuration_ready) throw new Error('本机配置尚未完成，请检查后重试。')
    } catch (cause) { setError(errorText(cause)) }
    finally { running.current = false; setBusy('') }
  }, [appSecret, load])

  useEffect(() => {
    if (!status || loading || recoveryAttempted.current || !localStorage.getItem(restartKey)) return
    recoveryAttempted.current = true
    void start()
  }, [status, loading, start])

  const beginFlow = async (kind: FlowKind) => {
    if (flowBusy.current) return
    flowBusy.current = true
    flowIDRef.current = null; setFlow(null); setFlowKind(null)
    setError(''); setBusy('正在打开连接页面…')
    try {
      const next = await (kind === 'connect' ? beginSetupLarkConnection() : kind === 'authorize' ? beginSetupLarkLogin() : beginSetupAgentLogin())
      setFlowKind(kind)
      if (next.status === 'pending') { flowIDRef.current = next.id; setFlow(next) }
      else if (next.status === 'success') { flowIDRef.current = null; setFlow(null); setFlowKind(null); await load() }
      else { flowIDRef.current = null; setFlow(null); setFlowKind(null); setError(next.error || '操作未完成') }
    } catch (cause) { setError(errorText(cause)) }
    finally { flowBusy.current = false; setBusy('') }
  }

  const repairCredentials = async () => {
    setError(''); setBusy('正在验证并更新当前应用密钥…')
    try {
      await repairSetupLarkCredentials(appSecret)
      // Keep the draft for first-run Finalize when no chat config exists yet.
      setEditingSecret(false)
      await load()
    } catch (cause) { setError(errorText(cause)) }
    finally { setBusy('') }
  }

  const cancelFlow = async () => {
    if (!flow?.id || flowBusy.current) return
    flowBusy.current = true
    setBusy('正在取消连接…')
    const current = flow
    flowIDRef.current = null; setFlow(null); setFlowKind(null)
    try { await cancelSetupFlow(current.id); await load() }
    catch (cause) { setError(errorText(cause)) }
    finally { flowBusy.current = false; setBusy('') }
  }

  if (loading) return <main className="setup-loading"><Spin size="small" /><span>正在检查已有配置…</span></main>
  const renderSetup = () => {
    if (!status) return <main className="setup-page"><section className="setup-shell"><Alert type="error" showIcon message="无法读取安装状态" description={error} /><Button onClick={() => void refresh()} loading={Boolean(busy)}>重新检查</Button></section></main>

    const action = setupAction(status)
    const locked = Boolean(busy || flow)
    const secretEditorVisible = setupSecretVisible(status, editingSecret)
    const flowURL = flow?.verification_url || flow?.output?.match(/https?:\/\/\S+/)?.[0]
    const appLabel = status.lark.app_name || status.lark.app_id
    const appURL = larkApplicationURL(status.lark.app_id || '')

    return <main className="setup-page"><section className="setup-shell setup-simple">
      <header className="setup-header"><img src={jarvisIcon} alt="" /><div><Typography.Title level={2}>开始使用 Jarvis</Typography.Title><Typography.Text type="secondary">已有配置自动复用，只需补齐缺少的连接。</Typography.Text></div></header>
      {status.lark.app_id && <section className="setup-connected" aria-label="当前飞书应用">
        <strong>正在连接的飞书助手：{status.lark.app_name || '应用名称暂未读取'}</strong>
        <span>应用 App ID：<Typography.Text code>{status.lark.app_id}</Typography.Text></span>
        <a href={appURL} target="_blank" rel="noreferrer">打开这个应用的管理页面</a>
        {status.lark.user.verified && status.lark.user.name && <span>已授权账号：{status.lark.user.name}</span>}
      </section>}
      <div className="setup-form">
        {!flow && <>
          {action === 'connect' && <Button type="primary" disabled={locked || Boolean(status.lark.error)} onClick={() => void beginFlow('connect')}>连接飞书</Button>}
          {action === 'application' && <Alert type="warning" showIcon message="当前飞书应用尚未就绪" description="请按下方检查结果补齐当前应用配置，再重新检查。无需创建另一个 Bot。" />}
          {action === 'application' && <SetupLarkApplication lark={status.lark} disabled={locked} />}
          {action === 'authorize' && <p>一次授权当前应用所需的消息、文档、日历、会议、妙记和待办权限。</p>}
          {action === 'authorize' && <Button type="primary" disabled={locked} onClick={() => void beginFlow('authorize')}>授权飞书账号</Button>}
          {action === 'agent' && <Button type="primary" disabled={locked} onClick={() => void beginFlow('agent')}>登录 Agent</Button>}
          {(action === 'start' || editingSecret) && <>
            {secretEditorVisible && <div className="setup-field">
              <strong>让这个助手在飞书里回复你</strong>
              <label htmlFor="setup-secret">请填写「{appLabel}」的应用密钥（App Secret）</label>
              <span>应用 App ID：{status.lark.app_id}</span>
              <Input.Password id="setup-secret" value={appSecret} disabled={locked} autoComplete="off" placeholder="补填当前飞书应用的密钥" onChange={(event) => { setAppSecret(event.target.value); setEditingSecret(true) }} />
              <p><a href={appURL} target="_blank" rel="noreferrer">打开「{appLabel}」的管理页面</a>，进入“凭证与基础信息”，复制 App Secret。这是上方应用的密钥，不是个人密码。</p>
            </div>}
            {action !== 'start'
              ? <Button type="primary" disabled={locked || !appSecret.trim()} onClick={() => void repairCredentials()}>验证并更新密钥</Button>
              : <Button type="primary" disabled={locked || (secretEditorVisible && !appSecret.trim())} onClick={() => void start()}>{error ? '重试并继续' : secretEditorVisible ? '验证并开始使用' : '开始使用'}</Button>}
          </>}
        </>}
        {busy && <span className="setup-running"><LoadingOutlined /> {busy}</span>}
        {flow && <>
          <Typography.Text>请在打开的页面完成操作，完成后这里会自动继续。</Typography.Text>
          {flow.user_code && <Typography.Text code>{flow.user_code}</Typography.Text>}
          {flowURL ? <><Button href={flowURL} target="_blank" rel="noreferrer" icon={<LinkOutlined />}>打开连接页面</Button><QRCode value={flowURL} size={144} /></> : <Spin size="small" />}
          {flowKind && <Button disabled={Boolean(busy)} onClick={() => void beginFlow(flowKind)}>重新生成连接</Button>}
          <Button aria-label="返回" disabled={Boolean(busy)} onClick={() => void cancelFlow()}>返回</Button>
        </>}
      </div>
      {(error || status.lark.error || status.agent.error) && <Alert type="error" showIcon message="本次操作未完成" description={error || status.lark.error || status.agent.error} />}
      {!locked && <Button type="link" onClick={() => void refresh()}>重新检查</Button>}
      <details className="setup-help"><summary>连接说明与帮助</summary>
        <p>全程使用 lark-cli 已连接的同一个飞书应用。安装时统一申请消息、文档、日历、会议、妙记及待办等内置功能所需权限；已有登录也会检查权限是否齐全。Bot 是它的聊天身份，个人授权也是授权给这个应用。</p>
        <p>若提示机器人或权限未就绪，在<a href="https://open.feishu.cn/app" target="_blank" rel="noreferrer">飞书开发者后台</a>检查当前应用：启用机器人、开通消息权限、使用长连接订阅 im.message.receive_v1 和 card.action.trigger，再发布版本。没有操作权限时请联系管理员。</p>
        <p>默认助手名称为 Jarvis，之后可在设置中修改。服务就绪后即可使用，工作背景在后台初始化；等待或失败不会挡住主界面。</p>
        <p>同一个 Bot 不应同时在其他机器运行聊天长连接；若此前装过，请先停止旧实例。完成后可在飞书给这个 Bot 发一条消息验证回复。</p>
        {status.lark.app_id && <Button disabled={locked} onClick={() => setEditingSecret(true)}>应用密钥已失效？更新这个应用的密钥</Button>}
        {status.lark.app_id && action !== 'application' && <SetupLarkApplication lark={status.lark} disabled={locked} />}
        {status.lark.app_id && (error || status.lark.error) && <Button disabled={locked} onClick={() => void beginFlow('authorize')}>重新授权当前应用</Button>}
      </details>
    </section></main>
  }

  if (!localReady && !(status && setupCanEnter(status, restartFrom))) return renderSetup()

  const warning = error || (status && !status.app_ready
    ? status.lark.error || status.agent.error || (status.lark.application_checks.some(check => !check.ready) ? '飞书应用的权限或事件检查未通过，请处理连接。' : '部分连接需要处理，相关功能可能暂不可用。')
    : '')
  return <>
    {warning && <div className="setup-connection-notice" role="status">
      <span title={warning}>连接检查：{warning}</span>
      <Button size="small" type="text" loading={Boolean(busy)} disabled={Boolean(flow)} onClick={() => void refresh()}>重新检查</Button>
      <Button size="small" type="link" onClick={() => setRepairOpen(true)}>处理连接</Button>
    </div>}
    {children}
    {status?.app_ready && <WorldModelSetup worldModelReady={status.world_model_ready} />}
    <Drawer title="连接检查与授权" open={repairOpen} onClose={() => setRepairOpen(false)} size={520} destroyOnHidden className="setup-repair-drawer">
      {renderSetup()}
    </Drawer>
  </>
}
