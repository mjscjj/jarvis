import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Button, Result, Spin, Typography } from 'antd'
import { LinkOutlined, LoginOutlined, SafetyCertificateOutlined } from '@ant-design/icons'
import { APIRequestError, authEvents, completeByteDanceLogin, getAuthStatus, loginWithByteDance, logoutFromJarvis, setAuthRecoveryHandler } from './api'
import type { AuthUser, AuthView } from './types'
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

// Modules own visitor login; principal authentication must not block their pages.
function isModuleRouteKey(key: string): boolean {
  return appModuleRegistry.some((module) => module.key === key)
}

function currentRouteIsModule(): boolean {
  return isModuleRouteKey(routeFromHash(window.location.hash, 'chat').key)
}

function canRetryLogin(cause: unknown): boolean {
  return !(cause instanceof APIRequestError) || cause.status >= 500
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
  const retryTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const identityRevision = useRef(0)
  const okrIdentity = useRef<string | undefined>(undefined)

  const apply = useCallback((view: AuthView) => {
    if (!mounted.current || signedOut.current) return
    setEnabled(view.enabled)
    userRef.current = view.user ?? null
    setUser(view.user ?? null)
    const next = view.status === 'pending' ? view : null
    pendingRef.current = next
    setPending(next)
    setError('')
  }, [])

  const recover = useCallback((forceLogin = false): Promise<void> => {
    if (inFlight.current) return inFlight.current
    if (signedOut.current || pendingRef.current) return Promise.resolve()
    clearTimeout(retryTimer.current)
    retryTimer.current = undefined
    // loading 表示「还没有可用身份」。已登录时被后台 401 触发的重新验证不能翻起
    // 它：AuthGate 会卸载整棵树，正在流式输出的对话和填写中的表单会一起丢掉。
    // 验证真的失败时下面清 user，届时才回到登录页。
    if (!userRef.current) setLoading(true)
    const revision = identityRevision.current
    const operation = (async () => {
      try {
        const status = await getAuthStatus()
        if (revision !== identityRevision.current || signedOut.current || !mounted.current) return
        // On an app-module route we never start the SSO device flow: the module
        // is open and runs its own visitor login. Only principal-only surfaces
        // fall through to loginWithByteDance.
        if (!status.enabled || status.user || (!forceLogin && currentRouteIsModule())) {
          apply(status)
        } else {
          const view = await loginWithByteDance()
          if (revision === identityRevision.current) apply(view)
        }
      } catch (cause) {
        if (revision === identityRevision.current && mounted.current && !signedOut.current) {
          userRef.current = null
          setUser(null)
          const message = cause instanceof Error ? cause.message : String(cause)
          setError(canRetryLogin(cause) ? `登录服务暂时不可用，正在自动重试。${message}` : message)
          if (canRetryLogin(cause)) {
            retryTimer.current = setTimeout(() => { void recover(forceLogin) }, 2000)
          }
        }
      } finally {
        inFlight.current = null
        if (revision === identityRevision.current && mounted.current) setLoading(false)
      }
    })()
    inFlight.current = operation
    return operation
  }, [apply])

  useEffect(() => {
    mounted.current = true
    setAuthRecoveryHandler(recover)
    const expired = () => { void recover() }
    const routeChanged = () => { if (!currentRouteIsModule()) void recover() }
    const okrChanged = (event: Event) => {
      const auth = (event as CustomEvent<{ authenticated: boolean; user?: { unionId?: string; openId: string } }>).detail
      const key = auth?.authenticated ? auth.user?.unionId || auth.user?.openId || '' : ''
      if (okrIdentity.current === key) return
      const initialized = okrIdentity.current !== undefined
      okrIdentity.current = key
      if (!initialized && inFlight.current) return
      identityRevision.current++
      clearTimeout(retryTimer.current)
      pendingRef.current = null
      userRef.current = null
      setPending(null)
      setUser(null)
      setLoading(true)
      if (!key && signedOut.current) {
        setLoading(false)
        return
      }
      signedOut.current = false
      const revision = identityRevision.current
      void (async () => {
        await inFlight.current
        if (revision === identityRevision.current && mounted.current) await recover()
      })()
    }
    authEvents.addEventListener('expired', expired)
    window.addEventListener('hashchange', routeChanged)
    window.addEventListener('jarvis:okr-auth-changed', okrChanged)
    void recover()
    return () => {
      mounted.current = false
      clearTimeout(retryTimer.current)
      setAuthRecoveryHandler(null)
      authEvents.removeEventListener('expired', expired)
      window.removeEventListener('hashchange', routeChanged)
      window.removeEventListener('jarvis:okr-auth-changed', okrChanged)
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
        setError('')
        if (view.status === 'pending' && view.flow_id && view.flow_id !== pending.flow_id) {
          apply(view)
          return
        }
        if (view.status === 'authenticated' && view.user) {
          apply(view)
          setError('')
          return
        }
        timer = setTimeout(poll, 2000)
      } catch (cause) {
        if (cancelled || signedOut.current) return
        if (pendingRef.current?.flow_id !== pending.flow_id) return
        if (canRetryLogin(cause)) {
          setError('连接暂时中断，正在自动恢复登录…')
          timer = setTimeout(poll, 2000)
        } else {
          pendingRef.current = null
          setPending(null)
          setError(cause instanceof Error ? cause.message : String(cause))
        }
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
    clearTimeout(retryTimer.current)
    retryTimer.current = undefined
    setLoading(false)
    pendingRef.current = null
    setPending(null)
    try {
      // Finish any recovery before invalidating its newly issued cookie.
      await inFlight.current
      await logoutFromJarvis()
      userRef.current = null
      setUser(null)
      setError('')
      window.dispatchEvent(new Event('jarvis:signed-out'))
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
  if (loading && !error) {
    return <div className="auth-loading"><Spin size="small" /><span>正在连接字节登录…</span></div>
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
          <Button type="primary" icon={<LoginOutlined />} loading={loading} onClick={() => void login()}>
            使用字节身份登录
          </Button>
        )}
        {error && <Result status="error" title="暂时无法完成登录" subTitle={error} />}
      </section>
    </main>
  )
}
