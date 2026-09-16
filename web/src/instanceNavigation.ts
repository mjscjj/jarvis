import { routeFromHash } from './pageRoutes.ts'

// Preserve the entire URL state, including old share links and comment anchors.
export function okrEntryURL(currentURL: string, targetPath: string): string | undefined {
  if (!targetPath) return undefined
  const url = new URL(currentURL)
  if (routeFromHash(url.hash, 'chat').key !== 'biz-okr') return undefined
  if (url.pathname === targetPath || url.pathname === targetPath.replace(/\/$/, '')) return undefined
  url.pathname = targetPath
  return url.href
}

export function redirectOKREntry(hash = window.location.hash): boolean {
  const target = document.querySelector<HTMLMetaElement>('meta[name="jarvis-okr-entry"]')?.content || ''
  const current = new URL(window.location.href)
  current.hash = hash
  const next = okrEntryURL(current.href, target)
  if (!next) return false
  window.location.replace(next)
  return true
}

export function mainWorkbenchURL(): string | undefined {
  const path = document.querySelector<HTMLMetaElement>('meta[name="jarvis-main-workbench"]')?.content
  return path ? `${path}#/chat` : undefined
}

// Only verified login state chooses this behavior; URLs never select an identity.
export function preferredWorkbenchURL(currentURL: string, targetPath: string, preferred: boolean): string | undefined {
  if (!preferred || !targetPath) return undefined
  const url = new URL(currentURL)
  const route = routeFromHash(url.hash, 'chat')
  if (route.key === 'biz-okr') return undefined
  // Explicit runtime-object links belong to this installation. Never carry a
  // development session/task ID into an unrelated main-instance object.
  if (route.selection || (route.key === 'chat' && route.viewState.session)) return undefined
  if (url.pathname === targetPath || url.pathname === targetPath.replace(/\/$/, '')) return undefined
  url.pathname = targetPath
  if (!url.hash) url.hash = '/chat'
  return url.href
}

export function redirectPreferredWorkbench(preferred: boolean, hash = window.location.hash, replace = true): boolean {
  const target = document.querySelector<HTMLMetaElement>('meta[name="jarvis-main-workbench"]')?.content || ''
  const current = new URL(window.location.href)
  current.hash = hash
  const next = preferredWorkbenchURL(current.href, target, preferred)
  if (!next) return false
  if (replace) window.location.replace(next)
  else window.location.assign(next)
  return true
}
