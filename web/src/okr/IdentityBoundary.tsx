import { useEffect, useState, type ReactNode } from 'react'
import { beginFeishuLogin, getAuthStatus, logout } from './emily/api'
import type { AuthStatus } from './emily/types'

export default function IdentityBoundary({ children }: { children: (auth: AuthStatus, logoutUser: () => Promise<void>) => ReactNode }) {
  const [auth, setAuth] = useState<AuthStatus>()
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

  if (!auth) {
    return <div className="flex min-h-[420px] items-center justify-center text-sm text-slate-400">{error || '正在确认身份…'}{error && <button type="button" onClick={() => void loadIdentity()} className="ml-2 text-indigo-600 hover:underline">重试</button>}</div>
  }

  if (auth.configured && !auth.authenticated) {
    return (
      <div className="flex min-h-[520px] items-center justify-center px-6">
        <section className="w-full max-w-sm rounded-2xl border border-slate-200 bg-white px-8 py-9 text-center shadow-sm">
          <span className="mx-auto flex size-11 items-center justify-center rounded-xl bg-indigo-600 text-base font-semibold text-white">E</span>
          <h1 className="mt-4 text-lg font-semibold text-slate-900">登录 Emily 协作台</h1>
          <p className="mt-2 text-sm leading-6 text-slate-500">用飞书识别填写人和评论人。所有 OKR 仍然对全员可见，不增加权限限制。</p>
          <button type="button" onClick={beginFeishuLogin} className="mt-6 h-10 w-full rounded-lg bg-indigo-600 text-sm font-medium text-white hover:bg-indigo-700">使用飞书登录</button>
        </section>
      </div>
    )
  }

  const logoutUser = async () => {
    await logout()
    await loadIdentity()
  }
  return children(auth, logoutUser)
}
