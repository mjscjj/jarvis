import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Result, Spin, Typography } from 'antd'
import { LinkOutlined, LoginOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { authEvents, completeByteDanceLogin, getAuthStatus, loginWithByteDance, logoutFromJarvis, setAuthRecoveryHandler } from './api'
import type { AuthUser, AuthView } from './types'
import { DeveloperDocumentLinks } from './components/DeveloperDocuments'
import { routeFromHash } from './pageRoutes'
import { appModuleRegistry } from './modules/registry'

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

// App modules (OKR today) are open to their own visitors and run their own
// in-module Lark login. The outer ByteDance SSO gate only guards the
// principal-only surfaces, so on a module route we never start an SSO flow and
// never block on it — the module decides who gets in.
function isModuleRouteKey(key: string): boolean {
  return appModuleRegistry.some((module) => module.key === key)
}

function currentRouteIsModule(): boolean {
  return isModuleRouteKey(routeFromHash(window.location.hash, 'chat').key)
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [enabled, setEnabled] = useState(true)
  const [user, setUser] = useState<AuthUser | null>(null)
  const [pending, setPending] = useState<AuthView | null>(null)
  const [error, setError] = useState('')

  const inFlight = useRef<Promise<void> | null>(null)
  const signedOut = useRef(false)
  const mounted = useRef(false)
  const pendingRef = useRef<AuthView | null>(null)
  const userRef = useRef<AuthUser | null>(null)

  const apply = useCallback((view: AuthView) => {
    if (!mounted.current || signedOut.current) return
    setEnabled(view.enabled)
    userRef.current = view.user ?? null
    setUser(view.user ?? null)
    const next = view.status === 'pending' ? view : null
    pendingRef.current = next
    setPending(next)
  }, [])

  const recover = useCallback((forceLogin = false): Promise<void> => {
    if (inFlight.current) return inFlight.current
    if (signedOut.current || pendingRef.current) return Promise.resolve()
    // loading 表示「还没有可用身份」。已登录时被后台 401 触发的重新验证不能翻起
    // 它：AuthGate 会卸载整棵树，正在流式输出的对话和填写中的表单会一起丢掉。
    // 验证真的失败时下面清 user，届时才回到登录页。
    if (!userRef.current) setLoading(true)
    setError('')
    const operation = (async () => {
      try {
        const status = await getAuthStatus()
        if (signedOut.current || !mounted.current) return
        // On an app-module route we never start the SSO device flow: the module
        // is open and runs its own visitor login. Only principal-only surfaces
        // fall through to loginWithByteDance.
        if (!status.enabled || status.user || (!forceLogin && currentRouteIsModule())) {
          apply(status)
        } else {
          apply(await loginWithByteDance())
        }
      } catch (cause) {
        if (mounted.current && !signedOut.current) {
          userRef.current = null
          setUser(null)
          setError(cause instanceof Error ? cause.message : String(cause))
        }
      } finally {
        inFlight.current = null
        if (mounted.current) setLoading(false)
      }
    })()
    inFlight.current = operation
    return operation
  }, [apply])

  useEffect(() => {
    mounted.current = true
    setAuthRecoveryHandler(recover)
    const expired = () => { void recover() }
    authEvents.addEventListener('expired', expired)
    void recover()
    return () => {
      mounted.current = false
      setAuthRecoveryHandler(null)
      authEvents.removeEventListener('expired', expired)
    }
  }, [recover])

  useEffect(() => {
    if (!pending?.flow_id) return
    let cancelled = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const view = await completeByteDanceLogin(pending.flow_id!)
        if (cancelled || signedOut.current) return
        if (pendingRef.current?.flow_id !== pending.flow_id) return
        if (view.status === 'authenticated' && view.user) {
          apply(view)
          setError('')
          return
        }
        timer = setTimeout(poll, 2000)
      } catch (cause) {
        if (cancelled || signedOut.current) return
        if (pendingRef.current?.flow_id !== pending.flow_id) return
        pendingRef.current = null
        setPending(null)
        setError(cause instanceof Error ? cause.message : String(cause))
      }
    }
    timer = setTimeout(poll, 1500)
    return () => { cancelled = true; clearTimeout(timer) }
  }, [pending?.flow_id, apply])

  const login = useCallback(() => {
    signedOut.current = false
    pendingRef.current = null
    setPending(null)
    return recover(true)
  }, [recover])

  const logout = useCallback(async () => {
    signedOut.current = true
    pendingRef.current = null
    setPending(null)
    try {
      // Finish any recovery before invalidating its newly issued cookie.
      await inFlight.current
      await logoutFromJarvis()
      userRef.current = null
      setUser(null)
      setError('')
    } catch (cause) {
      signedOut.current = false
      throw cause
    }
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
  const [onModuleRoute, setOnModuleRoute] = useState(() => currentRouteIsModule())
  useEffect(() => {
    const sync = () => setOnModuleRoute(currentRouteIsModule())
    window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  if (onModuleRoute) return children
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
            <Button onClick={() => void login()}>重新生成授权链接</Button>
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
