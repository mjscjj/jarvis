import { useEffect, useState } from 'react'
import { Alert, Button, Modal, Progress, Typography } from 'antd'
import { relaunch } from '@tauri-apps/plugin-process'
import { check } from '@tauri-apps/plugin-updater'
import type { Update } from '@tauri-apps/plugin-updater'

import { isTauriRuntime } from './tauri'

const { Paragraph, Text } = Typography

type Phase = 'available' | 'downloading' | 'installing' | 'failed'

function formatBytes(bytes: number) {
  const megabytes = bytes / 1024 / 1024
  return `${megabytes.toFixed(1)} MB`
}

function downloadPercent(downloaded: number, total: number) {
  if (total <= 0) return 0
  return Math.min(100, Math.round((downloaded / total) * 100))
}

export default function AppUpdate() {
  const [update, setUpdate] = useState<Update | null>(null)
  const [phase, setPhase] = useState<Phase>('available')
  const [downloaded, setDownloaded] = useState(0)
  const [total, setTotal] = useState(0)
  const [error, setError] = useState('')

  useEffect(() => {
    if (!isTauriRuntime()) return
    let active = true
    void check()
      .then((result) => {
        if (!active || !result) return
        setUpdate(result)
        setPhase('available')
      })
      .catch((cause) => {
        // The version probe runs unattended; a failed check must not block the app.
        console.error('Failed to check for updates', cause)
      })
    return () => {
      active = false
    }
  }, [])

  if (!update) return null

  const busy = phase === 'downloading' || phase === 'installing'

  const dismiss = () => {
    if (busy) return
    void update.close().catch(() => {})
    setUpdate(null)
  }

  const install = async () => {
    setPhase('downloading')
    setError('')
    setDownloaded(0)
    setTotal(0)
    try {
      await update.downloadAndInstall((event) => {
        if (event.event === 'Started') {
          setTotal(event.data.contentLength ?? 0)
        } else if (event.event === 'Progress') {
          setDownloaded((value) => value + event.data.chunkLength)
        } else if (event.event === 'Finished') {
          setPhase('installing')
        }
      })
      await relaunch()
    } catch (cause) {
      // The user asked for this, so the failure has to be visible instead of silent.
      setPhase('failed')
      setError(cause instanceof Error ? cause.message : String(cause))
    }
  }

  const footer =
    phase === 'failed'
      ? [
          <Button key="later" onClick={dismiss}>稍后再说</Button>,
          <Button key="retry" type="primary" onClick={() => void install()}>重试</Button>,
        ]
      : [
          <Button key="later" disabled={busy} onClick={dismiss}>稍后再说</Button>,
          <Button key="install" type="primary" loading={busy} onClick={() => void install()}>
            立即升级
          </Button>,
        ]

  return (
    <Modal
      open
      title={`有新版本 ${update.version}`}
      footer={footer}
      closable={!busy}
      maskClosable={false}
      keyboard={!busy}
      onCancel={dismiss}
    >
      <Paragraph>
        当前版本 <Text code>{update.currentVersion}</Text> ，可升级到 <Text code>{update.version}</Text> 。
      </Paragraph>
      {update.body ? <Paragraph type="secondary">{update.body}</Paragraph> : null}
      <Paragraph type="secondary">
        升级会下载完整安装包并重启 Jarvis，正在执行的任务会被中断；配置和数据保留。
      </Paragraph>
      {phase === 'downloading' ? (
        <Progress
          percent={downloadPercent(downloaded, total)}
          status="active"
          format={(percent) =>
            total > 0 ? `${percent}%` : formatBytes(downloaded)
          }
        />
      ) : null}
      {phase === 'installing' ? <Paragraph>正在安装并准备重启…</Paragraph> : null}
      {phase === 'failed' ? <Alert type="error" showIcon message="升级失败" description={error} /> : null}
    </Modal>
  )
}
