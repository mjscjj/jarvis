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
  listPlugins,
  triggerPlugin,
  updatePlugin,
  listAppModules,
  updateAppModule,
} from './api'
import PageHeader from './components/PageHeader'
import { usePageContext } from './pageContext'
import type { AppModule, Plugin, PluginAuthorization, PluginState } from './types'
import OKRPluginPage from './okr/OKRPluginPage'

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

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

const defaultOncallSearchTerms = ['oncall', '值班']
const defaultMeegoLookbackDays = 30

type CatalogItem =
  | { kind: 'okr'; id: 'okr'; module: AppModule }
  | { kind: 'collector'; id: string; plugin: Plugin }

function meegoLookbackDays(item: Plugin): number {
  const configured = item.config.lookback_days
  return typeof configured === 'number' && Number.isInteger(configured) && configured > 0
    ? configured
    : defaultMeegoLookbackDays
}

function MeegoLookbackConfig({
  item,
  onUpdated,
  onError,
}: {
  item: Plugin
  onUpdated: (plugin: Plugin) => void
  onError: (error: string) => void
}) {
  const [days, setDays] = useState(() => meegoLookbackDays(item))
  const [saving, setSaving] = useState(false)
  const savedDays = meegoLookbackDays(item)

  useEffect(() => {
    setDays(meegoLookbackDays(item))
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
          <Title level={4}>创建时间范围</Title>
          <Text type="secondary">只采集最近这些天内创建、且仍未完成的 Meego 工作项。</Text>
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
        onChange={(value) => setDays(value ?? defaultMeegoLookbackDays)}
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
  const [modules, setModules] = useState<AppModule[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState<string>()
  const [error, setError] = useState<string>()
  const [flows, setFlows] = useState<Record<string, PluginAuthorization>>({})
  const [messageApi, messageContext] = message.useMessage()

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const [pluginResult, moduleResult] = await Promise.all([listPlugins(), listAppModules()])
      setItems(pluginResult.items)
      setModules(moduleResult.items)
      setError(undefined)
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const okrModule = modules.find((item) => item.key === 'okr')
  const selectedOKR = context.view_state.plugin === 'okr' && okrModule?.is_enabled
  const selectedPlugin = items.find(
    (item) => item.enabled && item.id === context.view_state.plugin,
  )

  useEffect(() => {
    if (!loading && context.view_state.plugin && !selectedPlugin && !selectedOKR) setViewState({})
  }, [context.view_state.plugin, loading, selectedOKR, selectedPlugin, setViewState])

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

  const toggleOKR = async (item: AppModule, enabled: boolean) => {
    setBusy(item.key)
    try {
      const updated = await updateAppModule(item.key, { is_enabled: enabled })
      setModules((current) => current.map((entry) => entry.key === updated.key ? updated : entry))
      window.dispatchEvent(new Event('jarvis:app-modules-changed'))
      if (updated.restart_required) messageApi.info('OKR 插件配置已保存，重启服务后生效')
      else messageApi.success(`OKR 插件已${enabled ? '开启' : '关闭'}`)
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

  const catalogItems = useMemo<CatalogItem[]>(() => [
    ...(okrModule ? [{ kind: 'okr' as const, id: 'okr' as const, module: okrModule }] : []),
    ...items.map((plugin): CatalogItem => ({ kind: 'collector', id: plugin.id, plugin })),
  ], [items, okrModule])

  const columns = useMemo(() => [
    {
      title: '插件',
      key: 'plugin',
      render: (_: unknown, item: CatalogItem) => (
        <Space direction="vertical" size={0}>
          <Text strong>{item.kind === 'okr' ? 'OKR' : item.plugin.name}</Text>
          <Text type="secondary">{item.kind === 'okr' ? item.module.description : item.plugin.description}</Text>
        </Space>
      ),
    },
    {
      title: '类型',
      key: 'kind',
      width: 120,
      render: (_: unknown, item: CatalogItem) => <Tag>{item.kind === 'okr' ? '业务能力' : '数据来源'}</Tag>,
    },
    {
      title: '状态',
      key: 'state',
      width: 110,
      render: (_: unknown, item: CatalogItem) => item.kind === 'okr' ? (
        <Space size={4} wrap>
          <Tag color={item.module.is_enabled ? 'success' : 'default'}>{item.module.is_enabled ? '运行正常' : '已关闭'}</Tag>
          {item.module.restart_required && <Tag color="gold">等待重启</Tag>}
        </Space>
      ) : <Tag color={stateColors[item.plugin.state]}>{stateLabels[item.plugin.state]}</Tag>,
    },
    {
      title: '数据',
      key: 'data',
      width: 100,
      render: (_: unknown, item: CatalogItem) => item.kind === 'okr' ? 'O / KR / 进展' : `${item.plugin.clue_count} 条线索`,
    },
    {
      title: '最近活动',
      key: 'activity',
      width: 180,
      render: (_: unknown, item: CatalogItem) => item.kind === 'okr' ? '业务数据实时读取' : formatTime(item.plugin.last_finished_at),
    },
    {
      title: '操作',
      key: 'actions',
      width: 260,
      render: (_: unknown, item: CatalogItem) => item.kind === 'okr' ? (
        <Space>
          <Button disabled={!item.module.is_enabled} onClick={() => setViewState({ plugin: 'okr', plugin_tab: 'structure' })}>打开</Button>
          <Switch
            checked={item.module.configured_enabled}
            loading={busy === item.id}
            disabled={modules.some((candidate) => candidate.configured_enabled && candidate.requires.includes('okr'))}
            aria-label={`${item.module.configured_enabled ? '关闭' : '开启'} OKR`}
            onChange={(checked) => void toggleOKR(item.module, checked)}
          />
        </Space>
      ) : (
        <Space>
          {item.plugin.enabled && item.plugin.authorization.status !== 'authorized' && (
            <Button
              icon={<SafetyCertificateOutlined />}
              loading={busy === item.plugin.id}
              onClick={() => void authorize(item.plugin)}
            >
              授权
            </Button>
          )}
          <Button
            icon={<SyncOutlined />}
            disabled={!item.plugin.enabled || item.plugin.authorization.status !== 'authorized'}
            loading={busy === item.plugin.id}
            onClick={() => void trigger(item.plugin)}
          >
            立即同步
          </Button>
          <Switch
            checked={item.plugin.enabled}
            loading={busy === item.plugin.id}
            aria-label={`${item.plugin.enabled ? '关闭' : '开启'} ${item.plugin.name}`}
            onChange={(checked) => void toggle(item.plugin, checked)}
          />
        </Space>
      ),
    },
  ], [busy, modules, setViewState])

  const managementTable = (
    <section>
        <Table<CatalogItem>
          rowKey="id"
          loading={loading}
          dataSource={catalogItems}
          columns={columns}
          pagination={false}
          scroll={{ x: 900 }}
        />
    </section>
  )

  const pluginTab = (item: Plugin) => (
    <div className="plugin-detail">
      <div className="plugin-detail-heading">
        <div>
          <Title level={3}>{item.name}</Title>
          <Text type="secondary">{item.description}</Text>
        </div>
        <Space>
          <Tag color={stateColors[item.state]}>{stateLabels[item.state]}</Tag>
          {item.authorization.status !== 'authorized' && (
            <Button icon={<SafetyCertificateOutlined />} loading={busy === item.id} onClick={() => void authorize(item)}>
              授权
            </Button>
          )}
          <Button
            icon={<SyncOutlined />}
            disabled={item.authorization.status !== 'authorized'}
            loading={busy === item.id}
            onClick={() => void trigger(item)}
          >
            立即同步
          </Button>
        </Space>
      </div>
      {item.last_error && <Alert type="error" showIcon message={item.last_error} />}
      {item.id === 'meego' && (
        <MeegoLookbackConfig
          item={item}
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
      <Descriptions size="small" column={2}>
        <Descriptions.Item label="数据来源">{item.source}</Descriptions.Item>
        <Descriptions.Item label="采集周期">每 {item.interval_minutes} 分钟</Descriptions.Item>
        <Descriptions.Item label="Skill">{item.collector_skill}</Descriptions.Item>
        <Descriptions.Item label="下次同步">{formatTime(item.next_run_at)}</Descriptions.Item>
        <Descriptions.Item label="权限" span={2}>{item.permissions.join('、')}</Descriptions.Item>
      </Descriptions>
    </div>
  )

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {messageContext}
      <PageHeader title="插件" subtitle="管理 OKR 通用能力插件与外部数据源插件">
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
      {selectedOKR ? <OKRPluginPage /> : selectedPlugin ? pluginTab(selectedPlugin) : managementTable}
    </div>
  )
}
