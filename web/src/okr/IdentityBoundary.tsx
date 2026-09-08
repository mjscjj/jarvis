import { useEffect, useState, type ReactNode } from 'react'
import { beginFeishuLogin, getAuthStatus, logout, pollFeishuLogin } from './emily/api'
import type { AuthStatus, FeishuDeviceLogin } from './emily/types'

interface ActiveLogin extends FeishuDeviceLogin {
  retryAfterSeconds: number
}

const ACCOUNT_POSITION_KEY = 'jarvis-okr-account-position'

export default function IdentityBoundary({ children }: { children: ReactNode }) {
  const [auth, setAuth] = useState<AuthStatus>()
  const [activeLogin, setActiveLogin] = useState<ActiveLogin>()
  const [startingLogin, setStartingLogin] = useState(false)
  const [error, setError] = useState('')

  const loadIdentity = async () => {
    setError('')
    try {
      setAuth(await getAuthStatus())
    } catch (reason) {
      setAuth(undefined)
      setError(reason instanceof Error ? reason.message : '无法读取登录状态')
    }
  }

  useEffect(() => { void loadIdentity() }, [])

  useEffect(() => {
    if (!auth?.authenticated || !auth.expiresAt) return
    const expiresAt = new Date(auth.expiresAt).getTime()
    if (!Number.isFinite(expiresAt)) {
      setAuth(undefined)
      setError('登录到期时间无效')
      return
    }
    const delay = expiresAt - Date.now() + 1000
    if (delay <= 0) {
      void loadIdentity()
      return
    }
    const timer = window.setTimeout(() => void loadIdentity(), delay)
    return () => window.clearTimeout(timer)
  }, [auth?.authenticated, auth?.expiresAt])

  useEffect(() => {
    if (!activeLogin) return
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const result = await pollFeishuLogin(activeLogin.loginId)
          if (result.status === 'completed') {
            setActiveLogin(undefined)
            await loadIdentity()
            return
          }
          if (result.status === 'denied') {
            setActiveLogin(undefined)
            setError('飞书授权已取消，请重新登录')
            return
          }
          if (result.status === 'expired') {
            setActiveLogin(undefined)
            setError('飞书授权已过期，请重新登录')
            return
          }
          setActiveLogin(current => current ? {
            ...current,
            retryAfterSeconds: result.retryAfterSeconds ?? current.pollIntervalSeconds,
          } : undefined)
        } catch (reason) {
          setActiveLogin(undefined)
          setError(reason instanceof Error ? reason.message : '检查飞书授权结果失败')
        }
      })()
    }, Math.max(1, activeLogin.retryAfterSeconds) * 1000)
    return () => window.clearTimeout(timer)
  }, [activeLogin])

  useEffect(() => {
    // Older builds persisted a draggable absolute position. The account status
    // is fixed again, so remove that stale browser state once and let layout
    // remain the single source of truth for its placement.
    window.localStorage.removeItem(ACCOUNT_POSITION_KEY)
  }, [])

  const startLogin = async () => {
    setError('')
    setStartingLogin(true)
    const popup = window.open('', '_blank')
    try {
      const login = await beginFeishuLogin()
      setActiveLogin({ ...login, retryAfterSeconds: login.pollIntervalSeconds })
      if (popup) {
        popup.opener = null
        popup.location.assign(login.verificationUrl)
      }
    } catch (reason) {
      popup?.close()
      setError(reason instanceof Error ? reason.message : '发起飞书登录失败')
    } finally {
      setStartingLogin(false)
    }
  }

  const logoutUser = async () => {
    await logout()
    await loadIdentity()
  }

  if (!auth) {
    return <div className="flex min-h-[420px] items-center justify-center text-sm text-slate-400">{error || '正在确认身份…'}{error && <button type="button" onClick={() => void loadIdentity()} className="ml-2 text-indigo-600 hover:underline">重试</button>}</div>
  }

  if (auth.configured && !auth.authenticated) {
    return (
      <div className="flex min-h-[520px] items-center justify-center px-6">
        <section className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white px-8 py-9 text-center shadow-sm">
          <span className="mx-auto flex size-11 items-center justify-center rounded-xl bg-indigo-600 text-base font-semibold text-white">O</span>
          <h1 className="mt-4 text-lg font-semibold text-slate-900">登录 OKR</h1>
          <p className="mt-2 text-sm leading-6 text-slate-500">完成一次飞书登录后即可使用 OKR、周报和 Review。</p>
          {activeLogin ? (
            <div className="mt-6 rounded-xl border border-indigo-100 bg-indigo-50 px-4 py-4 text-left">
              <p className="text-sm font-medium text-slate-800">请在飞书授权页确认登录</p>
              {activeLogin.userCode && <p className="mt-2 text-xs text-slate-500">验证码：<span className="font-mono font-semibold text-slate-800">{activeLogin.userCode}</span></p>}
              <a href={activeLogin.verificationUrl} target="_blank" rel="noreferrer" className="mt-3 inline-flex text-sm font-medium text-indigo-600 hover:underline">打开飞书授权页</a>
              <p className="mt-3 text-xs text-slate-400">正在等待授权结果…</p>
            </div>
          ) : (
            <button type="button" onClick={() => void startLogin()} disabled={startingLogin} className="mt-6 h-10 w-full rounded-lg bg-indigo-600 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-wait disabled:opacity-60">{startingLogin ? '正在发起登录…' : '使用飞书登录'}</button>
          )}
          {error && <p className="mt-4 text-sm text-rose-600">{error}</p>}
        </section>
      </div>
    )
  }

  return (
    <div className="relative">
      {children}
      {auth.configured && <div className="fixed bottom-5 right-20 z-[60] flex items-center gap-2 rounded-full border border-slate-200 bg-white py-1.5 pr-3 pl-1.5 text-[11px] text-slate-500 shadow-md">{auth.user?.avatarUrl ? <img src={auth.user.avatarUrl} alt={auth.user.name} className="size-5 shrink-0 rounded-full object-cover" /> : <span className="flex size-5 shrink-0 items-center justify-center rounded-full bg-indigo-400 text-[9px] font-semibold text-white">{auth.user?.name?.slice(0, 1)}</span>}<span>{auth.user?.name}</span><button type="button" onClick={() => void logoutUser()} className="text-slate-400 hover:text-slate-700">退出</button></div>}
    </div>
  )
}
