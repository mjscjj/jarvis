import { useCallback, useEffect, useState } from 'react'
import { Alert, Card, Flex, Switch, Tag, Typography } from 'antd'
import { listAppModules, updateAppModule } from './api'
import type { AppModule } from './types'

const { Text } = Typography

interface AppModulesProps {
  moduleKeys?: readonly string[]
  title?: string
  description?: string
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

export default function AppModules({
  moduleKeys,
  title = '功能模块',
  description = '模块代码内置在 Jarvis 中；开关在服务重启后完整生效，关闭不会删除已有业务数据。',
}: AppModulesProps) {
  const [items, setItems] = useState<AppModule[]>([])
  const [loading, setLoading] = useState(true)
  const [savingKey, setSavingKey] = useState<string>()
  const [error, setError] = useState<string>()

  const reload = useCallback(() => {
    const controller = new AbortController()
    setLoading(true)
    listAppModules(controller.signal)
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => { if (!controller.signal.aborted) setLoading(false) })
    return () => controller.abort()
  }, [])

  useEffect(reload, [reload])

  const toggle = async (item: AppModule, isEnabled: boolean) => {
    setSavingKey(item.key)
    try {
      const updated = await updateAppModule(item.key, { is_enabled: isEnabled })
      setItems((current) => current.map((candidate) => candidate.key === updated.key ? updated : candidate))
      window.dispatchEvent(new Event('jarvis:app-modules-changed'))
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingKey(undefined)
    }
  }

  const visibleItems = moduleKeys
    ? items.filter((item) => moduleKeys.includes(item.key))
    : items

  return (
    <section className="app-modules-settings">
      <Flex vertical gap={4} className="section-heading">
        <Text strong>{title}</Text>
        <Text type="secondary">{description}</Text>
      </Flex>
      {error && <Alert type="error" showIcon title="模块配置读取失败" description={error} />}
      <div className="app-module-grid">
        {visibleItems.map((item) => {
          const blockedBy = items.filter((candidate) => candidate.configured_enabled && candidate.requires.includes(item.key))
          const missingRequirements = item.requires.filter((key) => !items.find((candidate) => candidate.key === key)?.configured_enabled)
          return (
          <Card key={item.key} loading={loading} size="small" className="app-module-card">
            <Flex justify="space-between" align="center" gap={16}>
              <Flex vertical gap={4}>
                <Flex align="center" gap={8}>
                  <Text strong>{item.name}</Text><Tag>{item.key}</Tag>
                  <Tag color={item.is_enabled ? 'green' : 'default'}>{item.is_enabled ? '当前已启用' : '当前已停用'}</Tag>
                  {item.restart_required && <Tag color="gold">等待重启</Tag>}
                </Flex>
                <Text type="secondary">{item.description}</Text>
                {item.requires.length > 0 && <Text type="secondary">依赖：{item.requires.join('、')}</Text>}
                {blockedBy.length > 0 && <Text type="warning">先停用：{blockedBy.map((candidate) => candidate.name).join('、')}</Text>}
              </Flex>
              <Switch
                checked={item.configured_enabled}
                loading={savingKey === item.key}
                disabled={(item.configured_enabled && blockedBy.length > 0) || (!item.configured_enabled && missingRequirements.length > 0)}
                checkedChildren="启用"
                unCheckedChildren="停用"
                onChange={(checked) => toggle(item, checked)}
              />
            </Flex>
          </Card>
        )})}
      </div>
    </section>
  )
}
