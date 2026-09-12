import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import { Alert, Button, Modal, Progress, Typography } from 'antd'
import { getVersion } from '@tauri-apps/api/app'
import { relaunch } from '@tauri-apps/plugin-process'
import { check } from '@tauri-apps/plugin-updater'
import type { DownloadEvent, Update } from '@tauri-apps/plugin-updater'

import { isTauriRuntime } from './tauri'
import { downloadPercent, formatBytes } from './updateStatus'
import type { UpdateStatus } from './updateStatus'

const { Paragraph, Text } = Typography

type ProgressHandler = (event: DownloadEvent) => void

export interface AppUpdateValue {
  supported: boolean
  currentVersion: string
  status: UpdateStatus
  update: Update | null
  error: string
  checkedAt: number | null
  promptOpen: boolean
  check: () => Promise<void>
  openPrompt: () => void
}

const Context = createContext<AppUpdateValue | null>(null)

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

// Download progress arrives per HTTP chunk, so it stays local to the prompt
// instead of re-rendering the whole tree through the context.
function UpdatePrompt({
  update,
  status,
  error,
  onInstall,
  onDismiss,
}: {
  update: Update
  status: UpdateStatus
  error: string
  onInstall: (onProgress: ProgressHandler) => Promise<void>
  onDismiss: () => void
}) {
  const [downloaded, setDownloaded] = useState(0)
  const [total, setTotal] = useState(0)
  const [downloadFinished, setDownloadFinished] = useState(false)
  const busy = status === 'installing'

  const install = () => {
    setDownloaded(0)
    setTotal(0)
    setDownloadFinished(false)
    void onInstall((event) => {
      if (event.event === 'Started') setTotal(event.data.contentLength ?? 0)
      else if (event.event === 'Progress') setDownloaded((value) => value + event.data.chunkLength)
      else if (event.event === 'Finished') setDownloadFinished(true)
    })
  }

  const footer =
    status === 'install-failed'
      ? [
          <Button key="later" onClick={onDismiss}>稍后再说</Button>,
          <Button key="retry" type="primary" onClick={install}>重试</Button>,
        ]
      : [
          <Button key="later" disabled={busy} onClick={onDismiss}>稍后再说</Button>,
          <Button key="install" type="primary" loading={busy} onClick={install}>立即升级</Button>,
        ]

  return (
    <Modal
      open
      title={`有新版本 ${update.version}`}
      footer={footer}
      closable={!busy}
      maskClosable={false}
      keyboard={!busy}
      onCancel={onDismiss}
    >
      <Paragraph>
        当前版本 <Text code>{update.currentVersion}</Text> ，可升级到 <Text code>{update.version}</Text> 。
      </Paragraph>
      {update.body ? <Paragraph type="secondary">{update.body}</Paragraph> : null}
      <Paragraph type="secondary">
        升级会下载完整安装包并重启 Jarvis，正在执行的任务会被中断；配置和数据保留。
      </Paragraph>
      {busy && !downloadFinished ? (
        <Progress
          percent={downloadPercent(downloaded, total)}
          status="active"
          format={(percent) => (total > 0 ? `${percent}%` : formatBytes(downloaded))}
        />
      ) : null}
      {busy && downloadFinished ? <Paragraph>正在安装并准备重启…</Paragraph> : null}
      {status === 'install-failed' ? (
        <Alert type="error" showIcon title="升级失败" description={error} />
      ) : null}
    </Modal>
  )
}

export function AppUpdateProvider({ children }: { children: ReactNode }) {
  const supported = isTauriRuntime()
  const [currentVersion, setCurrentVersion] = useState('')
  const [status, setStatus] = useState<UpdateStatus>('idle')
  const [update, setUpdate] = useState<Update | null>(null)
  const [error, setError] = useState('')
  const [checkedAt, setCheckedAt] = useState<number | null>(null)
  const [promptOpen, setPromptOpen] = useState(false)
  const updateRef = useRef<Update | null>(null)

  const runCheck = useCallback(async (promptWhenAvailable: boolean) => {
    setStatus('checking')
    setError('')
    try {
      const result = await check()
      // Update extends Resource: close() releases the Rust handle and makes the
      // package uninstallable, so only the replaced result may be closed.
      const previous = updateRef.current
      if (previous && previous !== result) void previous.close().catch(() => {})
      updateRef.current = result
      setUpdate(result)
      setCheckedAt(Date.now())
      setStatus(result ? 'available' : 'latest')
      if (result && promptWhenAvailable) setPromptOpen(true)
    } catch (cause) {
      setCheckedAt(Date.now())
      setStatus('check-failed')
      setError(errorText(cause))
    }
  }, [])

  const install = useCallback(async (onProgress: ProgressHandler) => {
    const target = updateRef.current
    if (!target) throw new Error('install called without a checked update')
    setStatus('installing')
    setError('')
    try {
      await target.downloadAndInstall(onProgress)
      await relaunch()
    } catch (cause) {
      setStatus('install-failed')
      setError(errorText(cause))
    }
  }, [])

  useEffect(() => {
    if (!supported) return
    void getVersion().then(setCurrentVersion)
    // The startup probe is unattended: a failed check only records its status.
    void runCheck(true)
  }, [supported, runCheck])

  const value = useMemo<AppUpdateValue>(
    () => ({
      supported,
      currentVersion,
      status,
      update,
      error,
      checkedAt,
      promptOpen,
      check: () => runCheck(false),
      openPrompt: () => setPromptOpen(true),
    }),
    [supported, currentVersion, status, update, error, checkedAt, promptOpen, runCheck],
  )

  return (
    <Context.Provider value={value}>
      {children}
      {promptOpen && update ? (
        <UpdatePrompt
          update={update}
          status={status}
          error={error}
          onInstall={install}
          onDismiss={() => setPromptOpen(false)}
        />
      ) : null}
    </Context.Provider>
  )
}

export function useAppUpdate(): AppUpdateValue {
  const value = useContext(Context)
  if (!value) throw new Error('useAppUpdate must be used within AppUpdateProvider')
  return value
}
