// One variable decides what the update UI shows. Keeping the outcome of the
// check and the outcome of the install in separate states is what let a stale
// failure reopen with the next available update.
export type UpdateStatus =
  | 'idle'
  | 'checking'
  | 'latest'
  | 'available'
  | 'check-failed'
  | 'installing'
  | 'install-failed'

export interface UpdateStatusLine {
  // 'text' stays inline; the other tones map to an Alert type.
  tone: 'text' | 'success' | 'info' | 'error'
  title: string
}

export function formatBytes(bytes: number) {
  const megabytes = bytes / 1024 / 1024
  return `${megabytes.toFixed(1)} MB`
}

export function downloadPercent(downloaded: number, total: number) {
  if (total <= 0) return 0
  return Math.min(100, Math.round((downloaded / total) * 100))
}

export function updateStatusLine(status: UpdateStatus, version: string): UpdateStatusLine | null {
  switch (status) {
    case 'idle':
      return null
    case 'checking':
      return { tone: 'text', title: '正在检查更新…' }
    case 'latest':
      return { tone: 'success', title: '已是最新版本' }
    case 'available':
      return { tone: 'info', title: `发现新版本 ${version}` }
    case 'check-failed':
      return { tone: 'error', title: '检查更新失败' }
    case 'installing':
      return { tone: 'text', title: '正在升级，请在升级窗口中查看进度' }
    case 'install-failed':
      return { tone: 'error', title: '升级失败' }
  }
}
