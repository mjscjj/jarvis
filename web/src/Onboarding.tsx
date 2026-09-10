import { useCallback, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import {
  ApiOutlined,
  CheckCircleFilled,
  CloudSyncOutlined,
  LinkOutlined,
  LoadingOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { Alert, Button, Input, Spin, Steps, Typography } from 'antd'
import {
  beginSetupAgentLogin,
  beginSetupLarkLogin,
  bindSetupLarkApp,
  bootstrapSetupWorldModel,
  finalizeSetup,
  getSetupFlow,
  getSetupStatus,
  getTask,
  loginWithByteDance,
} from './api'
import type { SetupFlow, SetupStatus } from './types'
import jarvisIcon from './assets/jarvis-icon.png'
import { DeveloperDocumentLinks } from './components/DeveloperDocuments'

type FlowKind = 'lark' | 'agent'

export function OnboardingGate({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SetupStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [appId, setAppId] = useState('')
  const [appSecret, setAppSecret] = useState('')
  const [agentName, setAgentName] = useState('Jarvis')
  const [flow, setFlow] = useState<SetupFlow | null>(null)
  const [flowKind, setFlowKind] = useState<FlowKind | null>(null)
  const [taskId, setTaskId] = useState<number | null>(() => {
    const value = Number(window.localStorage.getItem('jarvis.onboardingTaskId'))
    return Number.isSafeInteger(value) && value > 0 ? value : null
  })

  const load = useCallback(async () => {
    try {
      const next = await getSetupStatus()
      setStatus(next)
      if (next.lark.app_id) setAppId(next.lark.app_id)
      setError('')
      return next
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      return null
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    if (!flow?.id || flow.status !== 'pending') return
    let cancelled = false
    const timer = window.setInterval(async () => {
      try {
        const next = await getSetupFlow(flow.id)
        if (cancelled) return
        setFlow(next)
        if (next.status === 'success') {
          window.clearInterval(timer)
          await load()
          setBusy('')
        } else if (next.status === 'failed') {
          window.clearInterval(timer)
          setBusy('')
          setError(next.error || '授权失败')
        }
      } catch {
        // The service may be restarting; keep polling the current flow.
      }
    }, 1500)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [flow?.id, flow?.status, load])

  useEffect(() => {
    if (!taskId) return
    let cancelled = false
    const timer = window.setInterval(async () => {
      try {
        const task = await getTask(taskId)
        if (cancelled) return
        if (task.status === 'done') {
          window.clearInterval(timer)
          window.localStorage.removeItem('jarvis.onboardingTaskId')
          setTaskId(null)
          await load()
        } else if (task.status === 'failed') {
          window.clearInterval(timer)
          setTaskId(null)
          window.localStorage.removeItem('jarvis.onboardingTaskId')
          setError(task.summary || '世界模型初始化失败')
        }
      } catch {
        // A runtime restart briefly makes the API unavailable.
      }
    }, 2500)
    return () => {
      cancelled = true
      window.clearInterval(timer)
    }
  }, [taskId, load])

  const step = useMemo(() => {
    if (!status) return 0
    if (status.lark.bot.status !== 'ready') return 0
    if (status.lark.user.status !== 'ready') return 1
    if (!status.agent.authenticated) return 2
    if (!status.configuration.machine_configuration_ready) return 3
    return 4
  }, [status])

  if (loading || !status) {
    return <main className="setup-loading"><Spin size="small" /><span>正在检查本机环境...</span></main>
  }
  if (status.completed) return children

  const bind = async () => {
    setBusy('bind')
    setError('')
    try {
      const next = await bindSetupLarkApp(appId, appSecret)
      setStatus((current) => current ? { ...current, lark: next } : current)
      await load()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const beginFlow = async (kind: FlowKind) => {
    setBusy(kind)
    setFlowKind(kind)
    setFlow(null)
    setError('')
    try {
      const next = kind === 'lark' ? await beginSetupLarkLogin() : await beginSetupAgentLogin()
      setFlow(next)
      if (next.status === 'success') {
        await load()
        setBusy('')
      }
    } catch (cause) {
      setBusy('')
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }

  const finalize = async () => {
    setBusy('finalize')
    setError('')
    try {
      await finalizeSetup(agentName.trim(), appId, appSecret)
      for (let attempt = 0; attempt < 30; attempt += 1) {
        await new Promise((resolve) => window.setTimeout(resolve, 1000))
        try {
          await loginWithByteDance()
          const next = await getSetupStatus()
          setStatus(next)
          if (next.configuration.machine_configuration_ready) break
        } catch {
          // The Go runtime is restarting.
        }
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setBusy('')
    }
  }

  const bootstrap = async () => {
    setBusy('bootstrap')
    setError('')
    try {
      const result = await bootstrapSetupWorldModel()
      setTaskId(result.task_id)
      window.localStorage.setItem('jarvis.onboardingTaskId', String(result.task_id))
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setBusy('')
    }
  }

  const flowURL = flow?.verification_url || flow?.output?.match(/https?:\/\/\S+/)?.[0]

  return (
    <main className="setup-page">
      <section className="setup-shell">
        <header className="setup-header">
          <img src={jarvisIcon} alt="" />
          <div>
            <Typography.Title level={2}>初始化 Jarvis</Typography.Title>
            <Typography.Text type="secondary">连接当前身份和飞书工作空间</Typography.Text>
          </div>
        </header>
        <Steps
          current={step}
          size="small"
          items={[
            { title: 'Bot' },
            { title: '飞书授权' },
            { title: 'Agent' },
            { title: '身份' },
            { title: '世界模型' },
          ]}
        />

        <div className="setup-body">
          <SetupRow
            icon={<ApiOutlined />}
            title="飞书 Bot"
            done={status.lark.bot.status === 'ready'}
            summary={status.lark.app_name || status.lark.app_id || '绑定飞书自建应用'}
          >
            {status.lark.bot.status !== 'ready' && (
              <div className="setup-fields">
                <Input value={appId} onChange={(event) => setAppId(event.target.value)} placeholder="App ID（cli_...）" />
                <Input.Password value={appSecret} onChange={(event) => setAppSecret(event.target.value)} placeholder="App Secret" />
                <Button type="primary" loading={busy === 'bind'} disabled={!appId.trim() || !appSecret.trim()} onClick={() => void bind()}>
                  绑定并验证
                </Button>
              </div>
            )}
          </SetupRow>

          <SetupRow
            icon={<SafetyCertificateOutlined />}
            title="飞书用户授权"
            done={status.lark.user.status === 'ready'}
            summary={status.lark.user.name || '按最小业务范围授权'}
          >
            {status.lark.bot.status === 'ready' && status.lark.user.status !== 'ready' && (
              <Button loading={busy === 'lark'} onClick={() => void beginFlow('lark')}>开始授权</Button>
            )}
          </SetupRow>

          <SetupRow
            icon={<RobotOutlined />}
            title="Trae Agent"
            done={status.agent.authenticated}
            summary={status.agent.version || '登录任务执行引擎'}
          >
            {!status.agent.authenticated && (
              <Button loading={busy === 'agent'} onClick={() => void beginFlow('agent')}>登录 Agent</Button>
            )}
          </SetupRow>

          <SetupRow
            icon={<CloudSyncOutlined />}
            title="本机身份"
            done={status.configuration.machine_configuration_ready}
            summary={status.configuration.machine_configuration_ready ? '配置已写入' : '设置助手名称并启用服务'}
          >
            {status.lark.bot.status === 'ready' &&
              status.lark.user.status === 'ready' &&
              status.agent.authenticated &&
              !status.configuration.machine_configuration_ready && (
                <div className="setup-fields">
                  <Input value={agentName} maxLength={40} onChange={(event) => setAgentName(event.target.value)} placeholder="助手名称" />
                  {!appSecret && <Input.Password value={appSecret} onChange={(event) => setAppSecret(event.target.value)} placeholder="再次输入 App Secret 以绑定消息通道" />}
                  <Button type="primary" loading={busy === 'finalize'} disabled={!agentName.trim() || !appSecret.trim()} onClick={() => void finalize()}>
                    保存并启动服务
                  </Button>
                </div>
              )}
          </SetupRow>

          <SetupRow
            icon={<CloudSyncOutlined />}
            title="世界模型"
            done={status.world_model_ready}
            summary={status.world_model_ready ? '初始化完成' : taskId ? '正在读取必要上下文' : '建立初始的人、事、物和群'}
          >
            {status.configuration.machine_configuration_ready && !status.world_model_ready && !taskId && (
              <Button type="primary" loading={busy === 'bootstrap'} onClick={() => void bootstrap()}>
                开始初始化
              </Button>
            )}
            {taskId && <span className="setup-running"><LoadingOutlined /> 正在初始化，完成后自动进入</span>}
          </SetupRow>
        </div>

        {flow && flow.status === 'pending' && (
          <Alert
            type="info"
            showIcon
            message={flowKind === 'lark' ? '等待飞书授权' : '等待 Agent 登录'}
            description={(
              <div className="setup-auth-flow">
                {flow.user_code && <Typography.Text code>{flow.user_code}</Typography.Text>}
                {flowURL && <Button href={flowURL} target="_blank" icon={<LinkOutlined />}>打开授权页面</Button>}
                {flow.output && <pre className="setup-auth-output">{flow.output}</pre>}
                {!flowURL && <Typography.Text type="secondary">请在系统浏览器中完成登录</Typography.Text>}
              </div>
            )}
          />
        )}
        {error && <Alert type="error" showIcon message="初始化未完成" description={error} />}
        <section className="setup-documents">
          <Typography.Text type="secondary">开发文档</Typography.Text>
          <DeveloperDocumentLinks />
        </section>
      </section>
    </main>
  )
}

function SetupRow({
  icon,
  title,
  done,
  summary,
  children,
}: {
  icon: ReactNode
  title: string
  done: boolean
  summary: string
  children?: ReactNode
}) {
  return (
    <section className={`setup-row${done ? ' is-done' : ''}`}>
      <span className="setup-row-icon">{done ? <CheckCircleFilled /> : icon}</span>
      <div className="setup-row-content">
        <div className="setup-row-heading">
          <strong>{title}</strong>
          <Typography.Text type="secondary">{summary}</Typography.Text>
        </div>
        {children}
      </div>
    </section>
  )
}
