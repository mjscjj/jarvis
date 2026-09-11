import { openUrl } from '@tauri-apps/plugin-opener'

import { isTauriRuntime } from './tauri'

export function installExternalLinkHandler() {
  if (!isTauriRuntime()) return () => {}

  const handleClick = (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0) return
    const target = event.target
    if (!(target instanceof Element)) return

    const anchor = target.closest<HTMLAnchorElement>('a[target="_blank"]')
    if (!anchor?.href) return

    const url = new URL(anchor.href)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return

    event.preventDefault()
    void openUrl(url.href).catch((error) => {
      console.error('Failed to open external URL', error)
    })
  }

  document.addEventListener('click', handleClick, true)
  return () => document.removeEventListener('click', handleClick, true)
}
