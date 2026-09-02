import { useEffect, useSyncExternalStore } from 'react'
import { getPeopleAvatars } from '../api'

// Feishu只按姓名/邮箱查得到头像，查不到 open_id，所以这里两种键都存：
// 有 open_id 的负责人按 open_id 精确命中，只有名字的（例如评论作者）按名字命中。
const byOpenId = new Map<string, string>()
const byName = new Map<string, string>()
const ambiguousNames = new Set<string>()
const requested = new Set<string>()
const listeners = new Set<() => void>()

let version = 0
let pending: string[] = []
let timer: number | undefined

function publish() {
  version += 1
  for (const listener of listeners) listener()
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

async function flush() {
  timer = undefined
  const names = pending
  pending = []
  if (names.length === 0) return
  try {
    for (const person of await getPeopleAvatars(names)) {
      if (person.openId) byOpenId.set(person.openId, person.avatarUrl)
      if (!person.name || ambiguousNames.has(person.name)) continue
      const known = byName.get(person.name)
      if (known === undefined) {
        byName.set(person.name, person.avatarUrl)
      } else if (known !== person.avatarUrl) {
        // 同名的两个人：没有 open_id 就分不清是谁，宁可退回首字母也不挂错脸。
        byName.delete(person.name)
        ambiguousNames.add(person.name)
      }
    }
    publish()
  } catch (error) {
    // 头像取不到就退回首字母，但原因要留在控制台，不静默吞掉。
    console.warn('飞书头像读取失败', names, error)
  }
}

function requestAvatar(name: string) {
  const clean = name.trim()
  if (!clean || requested.has(clean)) return
  requested.add(clean)
  pending.push(clean)
  if (timer === undefined) timer = window.setTimeout(() => void flush(), 80)
}

export function usePersonAvatar(name: string, openId?: string) {
  useSyncExternalStore(subscribe, () => version)
  useEffect(() => {
    requestAvatar(name)
  }, [name])
  if (openId) {
    const hit = byOpenId.get(openId)
    if (hit) return hit
  }
  return byName.get(name.trim()) ?? ''
}

// PersonAvatar 在头像取到之前（以及取不到时）显示姓名首字母，尺寸和配色由调用方给。
export function PersonAvatar({ name, openId, size = 'size-3.5 text-[8px]', tone = 'bg-slate-300' }: {
  name: string
  openId?: string
  size?: string
  tone?: string
}) {
  const url = usePersonAvatar(name, openId)
  const shape = `inline-flex shrink-0 items-center justify-center overflow-hidden rounded-full ${size}`
  if (url) return <img src={url} alt={name} title={name} loading="lazy" className={`${shape} object-cover`} />
  return <span className={`${shape} font-semibold text-white ${tone}`}>{name.trim().slice(0, 1) || '?'}</span>
}
