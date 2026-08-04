import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Input, Popconfirm, Space, Spin, Typography, message } from 'antd'
import { ReloadOutlined, SaveOutlined } from '@ant-design/icons'
import { getSharedMemory, updateSharedMemory } from './api'
import PageHeader from './components/PageHeader'
import type { SharedMemory as SharedMemoryView } from './types'

const { Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

const SUBTITLE = '这段文本会作为可信背景注入所有 Agent（M3 抽取 / M5 执行 / 对话）的提示词，请谨慎编辑；可能含凭据。'

export default function SharedMemory() {
  const [view, setView] = useState<SharedMemoryView>()
  const [content, setContent] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()

  const load = useCallback((signal?: AbortSignal) => {
    setLoading(true)
    setError(undefined)
    getSharedMemory(signal)
      .then((data) => {
        setView(data)
        setContent(data.content)
      })
      .catch((cause) => {
        if (signal?.aborted) return
        setError(errorText(cause))
      })
      .finally(() => {
        if (!signal?.aborted) setLoading(false)
      })
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    load(controller.signal)
    return () => controller.abort()
  }, [load])

  const handleSave = useCallback(() => {
    setSaving(true)
    updateSharedMemory(content)
      .then((data) => {
        setView(data)
        setContent(data.content)
        message.success('共享记忆已保存')
      })
      .catch((cause) => message.error(`保存失败：${errorText(cause)}`))
      .finally(() => setSaving(false))
  }, [content])

  return (
    <div>
      <PageHeader title="共享记忆" subtitle={SUBTITLE}>
        <Popconfirm
          title="重新加载？"
          description="将用服务器最新内容覆盖当前未保存的编辑。"
          okText="重新加载"
          cancelText="取消"
          onConfirm={() => load()}
        >
          <Button icon={<ReloadOutlined />} disabled={loading || saving}>重新加载</Button>
        </Popconfirm>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={loading} onClick={handleSave}>
          保存
        </Button>
      </PageHeader>

      {error && (
        <Alert type="error" showIcon title="加载共享记忆失败" description={error} style={{ marginBottom: 16 }} closable onClose={() => setError(undefined)} />
      )}

      <Spin spinning={loading}>
        <Input.TextArea
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="这里维护踩过的坑、关键约定、凭据等可信背景……"
          autoSize={{ minRows: 18, maxRows: 40 }}
          style={{ fontFamily: 'var(--font-mono, monospace)', fontSize: 13 }}
        />
        <Space style={{ marginTop: 8 }} size="middle">
          <Text type="secondary" style={{ fontSize: 12 }}>
            {view?.saved ? `本地文件：${view.path} · 修改时间：${view.modified_at}` : '共享记忆文件不存在'}
          </Text>
        </Space>
      </Spin>
    </div>
  )
}
