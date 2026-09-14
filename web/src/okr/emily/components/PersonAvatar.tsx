import { useEffect, useSyncExternalStore } from 'react'
import { getPeopleAvatars } from '../api'

const avatars = new Map<string, string>()
const requested = new Set<string>()
const listeners = new Set<() => void>()
let version = 0
let pending: string[] = []
let timer: number | undefined
const AVATAR_BATCH_SIZE = 8
function subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } }
export function rememberPersonAvatars(people: Array<{email: string; avatarUrl: string}>) {
 for (const p of people) {if (p.avatarUrl) avatars.set(p.email,p.avatarUrl)}
 version++; for (const listener of listeners) listener()
}
async function flush() {
 timer = undefined
 const emails = pending.splice(0, AVATAR_BATCH_SIZE)
 try {
  rememberPersonAvatars(await getPeopleAvatars(emails))
 } catch (error) { console.warn('飞书头像读取失败', error); for (const email of emails) requested.delete(email) }
 if (pending.length > 0 && timer === undefined) timer = window.setTimeout(() => void flush(), 0)
}
export function usePersonAvatar(_name: string, email?: string, ownUrl?: string) {
 useSyncExternalStore(subscribe, () => version)
 useEffect(() => {
  if (ownUrl !== undefined || !email?.includes('@') || requested.has(email)) return
  requested.add(email); pending.push(email)
  if (timer === undefined) timer = window.setTimeout(() => void flush(), 80)
 }, [email, ownUrl])
 return ownUrl ?? (email ? avatars.get(email) ?? '' : '')
}
export function PersonAvatar({ name, email, ownUrl, size = 'size-3.5 text-[8px]', tone = 'bg-slate-300' }: {
 name: string; email?: string; ownUrl?: string; size?: string; tone?: string
}) {
 const url = usePersonAvatar(name, email, ownUrl)
 const shape = `inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full ${size}`
 if (url) return <img src={url} alt={name} title={name} loading="lazy" className={`${shape} object-cover`} />
 return <span className={`${shape} font-semibold text-white ${tone}`}>{name.trim().slice(0, 1) || '?'}</span>
}
