import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Result, Spin, Typography } from 'antd'
import { LinkOutlined, LoginOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { completeByteDanceLogin, getAuthStatus, loginWithByteDance, logoutFromJarvis } from './api'
import type { AuthUser, AuthView } from './types'
import { DeveloperDocumentLinks } from './components/DeveloperDocuments'

interface AuthContextValue {
  loading: boolean
  enabled: boolean
  user: AuthUser | null
  pending: AuthView | null
  error: string
  login: () => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [enabled, setEnabled] = useState(true)
  const [user, setUser] = useState<AuthUser | null>(null)
  const [pending, setPending] = useState<AuthView | null>(null)
  const [error, setError] = useState('')

  useEffect(() => {
    const controller = new AbortController()
    getAuthStatus(controller.signal)
      .then(async (view) => {
        if (controller.signal.aborted) return
		setEnabled(view.enabled)
		if (!view.enabled) {
			setUser(view.user ?? null)
			setPending(null)
			return
		}
        // Reuse the existing bytedcli login before asking for another click.
        const next = view.user ? view : await loginWithByteDance()
        if (controller.signal.aborted) return
		setEnabled(next.enabled)
        setUser(next.user ?? null)
        setPending(next.status === 'pending' ? next : null)
      })
      .catch((cause) => { if (!controller.signal.aborted) setError(cause instanceof Error ? cause.message : String(cause)) })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [])

  useEffect(() => {
    if (!pending?.flow_id) return
    let cancelled = false
    let timer: number | undefined

    const poll = async () => {
      try {
        const view = await completeByteDanceLogin(pending.flow_id!)
        if (cancelled) return
        if (view.status === 'authenticated' && view.user) {
          setUser(view.user)
          setPending(null)
          setError('')
          return
        }
      } catch (cause) {
        if (!cancelled) setError(cause instanceof Error ? cause.message : String(cause))
      }
      if (!cancelled) timer = window.setTimeout(poll, 2000)
    }

    timer = window.setTimeout(poll, 1500)
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [pending?.flow_id])

  const login = useCallback(async () => {
    setLoading(true)
    setError('')
    try {
      const view = await loginWithByteDance()
      if (view.status === 'authenticated' && view.user) {
        setUser(view.user)
        setPending(null)
      } else {
        setPending(view)
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setLoading(false)
    }
  }, [])

  const logout = useCallback(async () => {
    await logoutFromJarvis()
    setUser(null)
    setPending(null)
    setError('')
  }, [])

  const value = useMemo(() => ({
    loading, enabled, user, pending, error, login, logout,
  }), [loading, enabled, user, pending, error, login, logout])

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthContextValue {
  const value = useContext(AuthContext)
  if (!value) throw new Error('useAuth must be used within AuthProvider')
  return value
}

export function AuthGate({ agentName, children }: { agentName: string; children: ReactNode }) {
  const { loading, enabled, user, pending, error, login } = useAuth()
  if (loading) {
    return <div className="auth-loading"><Spin size="small" /><span>正在验证字节身份...</span></div>
  }
  if (!enabled || user) return children

  return (
    <main className="auth-page">
      <section className="auth-panel">
        <SafetyCertificateOutlined className="auth-mark" />
        <Typography.Title level={1}>{agentName}</Typography.Title>
        <Typography.Paragraph>使用字节身份登录</Typography.Paragraph>
        {pending?.verification_url ? (
          <>
            <Button type="primary" icon={<LinkOutlined />} href={pending.verification_url} target="_blank" rel="noreferrer">
              打开 SSO 授权页
            </Button>
            {pending.user_code && <Typography.Text className="auth-code">验证码：{pending.user_code}</Typography.Text>}
            <Typography.Text type="secondary">授权完成后此页面会自动进入</Typography.Text>
          </>
        ) : (
          <Button type="primary" icon={<LoginOutlined />} onClick={() => void login()}>
            使用字节身份登录
          </Button>
        )}
        {error && <Result status="error" subTitle={error} extra={<Button onClick={() => void login()}>重试</Button>} />}
        <section className="setup-documents">
          <Typography.Text type="secondary">开发文档</Typography.Text>
          <DeveloperDocumentLinks />
        </section>
      </section>
    </main>
  )
}
