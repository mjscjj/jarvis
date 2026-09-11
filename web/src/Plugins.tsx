import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Descriptions,
  InputNumber,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Typography,
  message,
} from 'antd'
import {
  ApiOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SaveOutlined,
  SyncOutlined,
} from '@ant-design/icons'
import {
  authorizePlugin,
  completePluginAuthorization,
  getPlugin,
  listPlugins,
  triggerPlugin,
  updatePlugin,
} from './api'
import PageHeader from './components/PageHeader'
import { usePageContext } from './pageContext'
import type { Plugin, PluginAuthorization, PluginState } from './types'

const { Text, Title } = Typography

const stateLabels: Record<PluginState, string> = {
  disabled: '已关闭',
  needs_auth: '需要授权',
  ready: '运行正常',
  running: '同步中',
  failed: '运行异常',
}

const stateColors: Record<PluginState, string> = {
  disabled: 'default',
  needs_auth: 'warning',
  ready: 'success',
  running: 'processing',
  failed: 'error',
}

function formatTime(value: string | null): string {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚未运行'
}

function pluginStateLabel(item: Plugin): string {
  if (item.kind === 'capability') return item.enabled ? '已启用' : '已关闭'
  if (item.enabled && item.authorization.status === 'pending') return '检查授权'
  return stateLabels[item.state]
}

function pluginStateColor(item: Plugin): string {
  if (item.kind === 'capability') return item.enabled ? 'success' : 'default'
  if (item.enabled && item.authorization.status === 'pending') return 'processing'
  return stateColors[item.state]
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

const defaultOncallSearchTerms = ['oncall', '值班']

const lookbackPlugins: Record<string, { fallbackDays: number; title: string; hint: string }> = {
  meego: {
    fallbackDays: 30,
    title: '创建时间范围',
    hint: '只采集最近这些天内创建、且仍未完成的 Meego 工作项。',
  },
  codebase: {
    fallbackDays: 3,
    title: '更新时间范围',
    hint: '只采集最近这些天内有更新的开放 MR；更早的历史 MR 不再重复投递。',
  },
}

function lookbackDays(item: Plugin, fallbackDays: number): number {
  const configured = item.config.lookback_days
  return typeof configured === 'number' && Number.isInteger(configured) && configured > 0
    ? configured
    : fallbackDays
}

function LookbackConfig({
  item,
  fallbackDays,
  title,
  hint,
  onUpdated,
  onError,
}: {
  item: Plugin
  fallbackDays: number
  title: string
  hint: string
  onUpdated: (plugin: Plugin) => void
  onError: (error: string) => void
}) {
  const [days, setDays] = useState(() => lookbackDays(item, fallbackDays))
  const [saving, setSaving] = useState(false)
  const savedDays = lookbackDays(item, fallbackDays)

  useEffect(() => {
    setDays(lookbackDays(item, fallbackDays))
  }, [item.revision])

  const save = async () => {
    setSaving(true)
    try {
      const updated = await updatePlugin(item.id, item.enabled, item.revision, {
        ...item.config,
        lookback_days: days,
      })
      onUpdated(updated)
    } catch (cause) {
      onError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="plugin-config-section">
      <div className="plugin-config-heading">
        <div>
          <Title level={4}>{title}</Title>
          <Text type="secondary">{hint}</Text>
        </div>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={days === savedDays} onClick={() => void save()}>
          保存规则
        </Button>
      </div>
      <InputNumber
        min={1}
        max={3650}
        precision={0}
        value={days}
        addonAfter="天"
        onChange={(value) => setDays(value ?? fallbackDays)}
      />
    </section>
  )
}

function oncallSearchTerms(item: Plugin): string[] {
  const configured = item.config.search_terms
  if (!Array.isArray(configured)) return defaultOncallSearchTerms
  return configured.filter((value): value is string => typeof value === 'string')
}

function OncallSearchConfig({
  item,
  onUpdated,
  onError,
}: {
  item: Plugin
  onUpdated: (plugin: Plugin) => void
  onError: (error: string) => void
}) {
  const [terms, setTerms] = useState(() => oncallSearchTerms(item))
  const [saving, setSaving] = useState(false)
  const savedTerms = oncallSearchTerms(item)
  const dirty = JSON.stringify(terms) !== JSON.stringify(savedTerms)

  useEffect(() => {
    setTerms(oncallSearchTerms(item))
  }, [item.revision])

  const save = async () => {
    setSaving(true)
    try {
      const updated = await updatePlugin(item.id, item.enabled, item.revision, {
        ...item.config,
        search_terms: terms,
      })
      onUpdated(updated)
    } catch (cause) {
      onError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  return (
    <section className="plugin-config-section">
      <div className="plugin-config-heading">
        <div>
          <Title level={4}>群名检索规则</Title>
          <Text type="secondary">匹配群名称或描述，用于发现你已加入的 Oncall 群。</Text>
        </div>
        <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={!dirty} onClick={() => void save()}>
          保存规则
        </Button>
      </div>
      <Select
        mode="tags"
        value={terms}
        tokenSeparators={[',', '，']}
        placeholder="输入群名关键词，如 SRE 告警"
        maxCount={20}
        style={{ width: '100%' }}
        onChange={(values) => {
          const normalized = values
            .map((value) => value.trim().slice(0, 60))
            .filter((value, index, all) => value && all.findIndex((candidate) => candidate.toLocaleLowerCase() === value.toLocaleLowerCase()) === index)
          setTerms(normalized)
        }}
      />
    </section>
  )
}

export default function Plugins() {
  const { context, setViewState } = usePageContext()
  const [items, setItems] = useState<Plugin[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string>()
  const [error, setError] = useState<string>()
  const [flows, setFlows] = useState<Record<string, PluginAuthorization>>({})
  const [messageApi, messageContext] = message.useMessage()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const result = await listPlugins()
      setItems(result.items)
      setError(undefined)
      const enabledCollectors = result.items.filter((item) => item.kind === 'collector' && item.enabled)
      void Promise.all(enabledCollectors.map(async (item) => {
        try {
          const detail = await getPlugin(item.id)
          setItems((current) => current.map((entry) => entry.id === detail.id ? detail : entry))
        } catch (cause) {
          setError(errorText(cause))
        }
      }))
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const selectedPlugin = items.find(
    (item) => item.enabled && item.id === context.view_state.plugin,
  )

  useEffect(() => {
    if (!loading && context.view_state.plugin && !selectedPlugin) setViewState({})
  }, [context.view_state.plugin, loading, selectedPlugin, setViewState])

  useEffect(() => {
    const active = Object.entries(flows).filter(([, flow]) => flow.status === 'pending' && flow.flow_id)
    if (active.length === 0) return
    const timer = window.setTimeout(() => {
      void Promise.all(active.map(async ([pluginId, flow]) => {
        try {
          const result = await completePluginAuthorization(pluginId, flow.flow_id!)
          setFlows((current) => ({ ...current, [pluginId]: result.authorization }))
          if (result.plugin) {
            setItems((current) => current.map((item) => item.id === result.plugin!.id ? result.plugin! : item))
            messageApi.success(`${result.plugin.name} 授权完成`)
          } else if (result.authorization.status === 'failed' && result.authorization.error) {
            setError(result.authorization.error)
          }
        } catch (cause) {
          setError(errorText(cause))
        }
      }))
    }, 3000)
    return () => window.clearTimeout(timer)
  }, [flows, messageApi])

  const toggle = async (item: Plugin, enabled: boolean) => {
    setBusy(item.id)
    try {
      const updated = await updatePlugin(item.id, enabled, item.revision)
      setItems((current) => current.map((entry) => entry.id === updated.id ? updated : entry))
      window.dispatchEvent(new Event('jarvis:plugins-changed'))
      if (enabled) setViewState({ plugin: updated.id })
      else if (context.view_state.plugin === updated.id) setViewState({})
      if (enabled && updated.state === 'needs_auth') messageApi.info(`${updated.name} 已开启，完成授权后开始同步`)
      else messageApi.success(`${updated.name} 已${enabled ? '开启' : '关闭'}`)
      setError(undefined)
    } catch (cause) {
      setError(errorText(cause))
      await load()
    } finally {
      setBusy(undefined)
    }
  }

  const authorize = async (item: Plugin) => {
    setBusy(item.id)
    try {
      const authorization = await authorizePlugin(item.id)
      setFlows((current) => ({ ...current, [item.id]: authorization }))
      if (authorization.status === 'authorized') {
        await load()
        messageApi.success(`${item.name} 已授权`)
      } else if (authorization.verification_url) {
        window.open(authorization.verification_url, '_blank', 'noopener,noreferrer')
      } else if (authorization.error) {
        setError(authorization.error)
      }
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setBusy(undefined)
    }
  }

  const trigger = async (item: Plugin) => {
    setBusy(item.id)
    try {
      const updated = await triggerPlugin(item.id)
      setItems((current) => current.map((entry) => entry.id === updated.id ? updated : entry))
      messageApi.success(`${item.name} 同步任务已提交`)
      setError(undefined)
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setBusy(undefined)
    }
  }

  const applyUpdate = (updated: Plugin) => {
    setItems((current) => current.map((entry) => entry.id === updated.id ? updated : entry))
    setError(undefined)
    messageApi.success(
      updated.enabled && updated.authorization.status === 'authorized'
        ? `${updated.name} 规则已保存并提交同步`
        : `${updated.name} 规则已保存`,
    )
  }

  const columns = useMemo(() => [
    {
      title: '插件',
      key: 'plugin',
      render: (_: unknown, item: Plugin) => (
        <Space direction="vertical" size={0}>
          <Text strong>{item.name}</Text>
          <Text type="secondary">{item.description}</Text>
        </Space>
      ),
    },
    {
      title: '状态',
      width: 110,
      render: (_: unknown, item: Plugin) => <Tag color={pluginStateColor(item)}>{pluginStateLabel(item)}</Tag>,
    },
    {
      title: '线索',
      dataIndex: 'clue_count',
      width: 80,
      render: (count: number, item: Plugin) => item.kind === 'collector' ? `${count} 条` : '—',
    },
    {
      title: '最近同步',
      dataIndex: 'last_finished_at',
      width: 180,
      render: (value: string | null, item: Plugin) => item.kind === 'collector' ? formatTime(value) : '—',
    },
    {
      title: '操作',
      key: 'actions',
      width: 260,
      render: (_: unknown, item: Plugin) => (
        <Space>
          {item.kind === 'collector' && item.enabled && item.authorization.status !== 'authorized' && (
            <Button
              icon={<SafetyCertificateOutlined />}
              loading={busy === item.id}
              onClick={() => void authorize(item)}
            >
              授权
            </Button>
          )}
          {item.kind === 'collector' && (
            <Button
              icon={<SyncOutlined />}
              disabled={!item.enabled || item.authorization.status !== 'authorized'}
              loading={busy === item.id}
              onClick={() => void trigger(item)}
            >
              立即同步
            </Button>
          )}
          <Switch
            checked={item.enabled}
            loading={busy === item.id}
            aria-label={`${item.enabled ? '关闭' : '开启'} ${item.name}`}
            onChange={(checked) => void toggle(item, checked)}
          />
        </Space>
      ),
    },
  ], [busy])

  const managementTable = (
    <Table<Plugin>
      rowKey="id"
      loading={loading}
      dataSource={items}
      columns={columns}
      pagination={false}
      scroll={{ x: 900 }}
    />
  )

  const pluginTab = (item: Plugin) => (
    <div className="plugin-detail">
      <div className="plugin-detail-heading">
        <div>
          <Title level={3}>{item.name}</Title>
          <Text type="secondary">{item.description}</Text>
        </div>
        <Space>
          <Tag color={pluginStateColor(item)}>{pluginStateLabel(item)}</Tag>
          {item.kind === 'collector' && item.authorization.status !== 'authorized' && (
            <Button icon={<SafetyCertificateOutlined />} loading={busy === item.id} onClick={() => void authorize(item)}>
              授权
            </Button>
          )}
          {item.kind === 'collector' && (
            <Button
              icon={<SyncOutlined />}
              disabled={item.authorization.status !== 'authorized'}
              loading={busy === item.id}
              onClick={() => void trigger(item)}
            >
              立即同步
            </Button>
          )}
        </Space>
      </div>
      {item.last_error && <Alert type="error" showIcon message={item.last_error} />}
      {lookbackPlugins[item.id] && (
        <LookbackConfig
          item={item}
          fallbackDays={lookbackPlugins[item.id].fallbackDays}
          title={lookbackPlugins[item.id].title}
          hint={lookbackPlugins[item.id].hint}
          onUpdated={applyUpdate}
          onError={(message) => {
            setError(message)
            void load()
          }}
        />
      )}
      {item.id === 'oncall' && (
        <OncallSearchConfig
          item={item}
          onUpdated={applyUpdate}
          onError={(message) => {
            setError(message)
            void load()
          }}
        />
      )}
      {item.kind === 'collector' ? (
        <Descriptions size="small" column={2}>
          <Descriptions.Item label="数据来源">{item.source}</Descriptions.Item>
          <Descriptions.Item label="采集周期">每 {item.interval_minutes} 分钟</Descriptions.Item>
          <Descriptions.Item label="Skill">{item.collector_skill}</Descriptions.Item>
          <Descriptions.Item label="下次同步">{formatTime(item.next_run_at)}</Descriptions.Item>
          <Descriptions.Item label="权限" span={2}>{item.permissions.join('、')}</Descriptions.Item>
        </Descriptions>
      ) : (
        <Descriptions size="small" column={1}>
          <Descriptions.Item label="工作方式">复用现有消息、Todo、Task 与 M3/M5，不建立独立采集链路</Descriptions.Item>
          <Descriptions.Item label="阶段规则">{item.skills.join('、')}</Descriptions.Item>
          <Descriptions.Item label="数据位置">现有 Task 的冻结 source_payload 与执行历史</Descriptions.Item>
        </Descriptions>
      )}
    </div>
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {messageContext}
      <PageHeader title="插件" subtitle="按需启用外部来源与工作能力；关闭后保留已有历史">
        <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void load()}>刷新</Button>
      </PageHeader>
      {error && <Alert type="error" showIcon closable message={error} onClose={() => setError(undefined)} />}
      {Object.entries(flows).map(([pluginId, flow]) => flow.status === 'pending' && (
        <Alert
          key={pluginId}
          type="info"
          showIcon
          icon={<ApiOutlined />}
          message={`${items.find((item) => item.id === pluginId)?.name ?? pluginId} 等待授权`}
          description={
            <Space>
              {flow.user_code && <Text code>{flow.user_code}</Text>}
              {flow.verification_url && (
                <Button type="link" href={flow.verification_url} target="_blank">打开授权页面</Button>
              )}
            </Space>
          }
        />
      ))}
      {selectedPlugin ? pluginTab(selectedPlugin) : managementTable}
    </div>
  )
}
