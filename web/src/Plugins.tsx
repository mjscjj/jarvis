import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Descriptions,
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
  SyncOutlined,
} from '@ant-design/icons'
import {
  authorizePlugin,
  completePluginAuthorization,
  listPlugins,
  triggerPlugin,
  updatePlugin,
} from './api'
import PageHeader from './components/PageHeader'
import type { Plugin, PluginAuthorization, PluginState } from './types'

const { Text } = Typography

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

export default function Plugins() {
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
    } catch (cause) {
      setError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

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
      dataIndex: 'state',
      width: 110,
      render: (state: PluginState) => <Tag color={stateColors[state]}>{stateLabels[state]}</Tag>,
    },
    {
      title: '线索',
      dataIndex: 'clue_count',
      width: 80,
      render: (count: number) => `${count} 条`,
    },
    {
      title: '最近同步',
      dataIndex: 'last_finished_at',
      width: 180,
      render: formatTime,
    },
    {
      title: '操作',
      key: 'actions',
      width: 260,
      render: (_: unknown, item: Plugin) => (
        <Space>
          {item.enabled && item.authorization.status !== 'authorized' && (
            <Button
              icon={<SafetyCertificateOutlined />}
              loading={busy === item.id}
              onClick={() => void authorize(item)}
            >
              授权
            </Button>
          )}
          <Button
            icon={<SyncOutlined />}
            disabled={!item.enabled || item.authorization.status !== 'authorized'}
            loading={busy === item.id}
            onClick={() => void trigger(item)}
          >
            立即同步
          </Button>
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

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {messageContext}
      <PageHeader title="插件" subtitle="按需连接外部工作系统；关闭后不再采集新线索">
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
      <Table<Plugin>
        rowKey="id"
        loading={loading}
        dataSource={items}
        columns={columns}
        pagination={false}
        scroll={{ x: 900 }}
        expandable={{
          expandedRowRender: (item) => (
            <Descriptions size="small" column={2}>
              <Descriptions.Item label="数据来源">{item.source}</Descriptions.Item>
              <Descriptions.Item label="采集周期">每 {item.interval_minutes} 分钟</Descriptions.Item>
              <Descriptions.Item label="Skill">{item.collector_skill}</Descriptions.Item>
              <Descriptions.Item label="下次同步">{formatTime(item.next_run_at)}</Descriptions.Item>
              <Descriptions.Item label="权限" span={2}>{item.permissions.join('、')}</Descriptions.Item>
              {item.last_error && <Descriptions.Item label="最近错误" span={2}>{item.last_error}</Descriptions.Item>}
            </Descriptions>
          ),
        }}
      />
    </div>
  )
}
