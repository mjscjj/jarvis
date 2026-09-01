import { useEffect, useState, type ReactNode } from 'react'
import { beginFeishuLogin, getAuthStatus, logout, pollFeishuLogin } from './emily/api'
import type { AuthStatus, FeishuDeviceLogin } from './emily/types'

interface ActiveLogin extends FeishuDeviceLogin {
  retryAfterSeconds: number
}

const initialAuth: AuthStatus = { authenticated: false, configured: true }

export default function IdentityBoundary({ children }: {
  children: (auth: AuthStatus, logoutUser: () => Promise<void>, loginUser: () => Promise<void>) => ReactNode
}) {
  const [auth, setAuth] = useState<AuthStatus>(initialAuth)
  const [loginOpen, setLoginOpen] = useState(false)
  const [activeLogin, setActiveLogin] = useState<ActiveLogin>()
  const [startingLogin, setStartingLogin] = useState(false)
  const [error, setError] = useState('')

  const loadIdentity = async () => {
    setError('')
    try {
      setAuth(await getAuthStatus())
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '无法读取登录状态')
    }
  }

  useEffect(() => { void loadIdentity() }, [])

  useEffect(() => {
    if (!activeLogin) return
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const result = await pollFeishuLogin(activeLogin.loginId)
          if (result.status === 'completed') {
            setActiveLogin(undefined)
            setLoginOpen(false)
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

  const startLogin = async () => {
    setLoginOpen(true)
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

  return (
    <>
      {children(auth, logoutUser, startLogin)}
      {error && !loginOpen && (
        <div className="fixed bottom-4 left-1/2 z-[70] flex -translate-x-1/2 items-center gap-2 rounded-lg border border-rose-200 bg-white px-3 py-2 text-xs text-rose-600 shadow-lg">
          <span>{error}</span>
          <button type="button" onClick={() => void loadIdentity()} className="text-indigo-600 hover:underline">重试</button>
        </div>
      )}
      {loginOpen && (
        <div className="fixed inset-0 z-[80] flex items-center justify-center bg-slate-900/30 px-6" role="dialog" aria-modal="true" aria-label="飞书登录">
          <section className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white px-8 py-8 text-center shadow-xl">
            <span className="mx-auto flex size-11 items-center justify-center rounded-xl bg-indigo-600 text-base font-semibold text-white">E</span>
            <h1 className="mt-4 text-lg font-semibold text-slate-900">用飞书发表评论</h1>
            <p className="mt-2 text-sm leading-6 text-slate-500">登录只用于记录评论人，不影响 OKR、周报和 Review 的其他操作。</p>
            {activeLogin ? (
              <div className="mt-5 rounded-xl border border-indigo-100 bg-indigo-50 px-4 py-4 text-left">
                <p className="text-sm font-medium text-slate-800">请在飞书授权页确认登录</p>
                {activeLogin.userCode && <p className="mt-2 text-xs text-slate-500">验证码：<span className="font-mono font-semibold text-slate-800">{activeLogin.userCode}</span></p>}
                <a href={activeLogin.verificationUrl} target="_blank" rel="noreferrer" className="mt-3 inline-flex text-sm font-medium text-indigo-600 hover:underline">打开飞书授权页</a>
                <p className="mt-3 text-xs text-slate-400">正在等待授权结果…</p>
              </div>
            ) : (
              <button type="button" onClick={() => void startLogin()} disabled={startingLogin} className="mt-5 h-10 w-full rounded-lg bg-indigo-600 text-sm font-medium text-white hover:bg-indigo-700 disabled:cursor-wait disabled:opacity-60">{startingLogin ? '正在发起登录…' : '重新发起飞书登录'}</button>
            )}
            {error && <p className="mt-4 text-sm text-rose-600">{error}</p>}
            <button type="button" onClick={() => setLoginOpen(false)} className="mt-4 text-sm text-slate-400 hover:text-slate-700">暂不登录</button>
          </section>
        </div>
      )}
    </>
  )
}
