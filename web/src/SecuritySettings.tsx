import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Alert,
  Button,
  Card,
  Flex,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  AuditOutlined,
  FileProtectOutlined,
  MessageOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { getSecuritySettings, listSecurityAuditEvents, updateSecuritySettings } from './api'
import type {
  AccessAuditActorKind,
  AccessAuditEvent,
  AccessAuditOperation,
  SecuritySettingsView,
} from './types'
import './styles/security-settings.css'
import { usePageContext } from './pageContext'

const { Text, Title } = Typography

const actorLabels: Record<AccessAuditActorKind, string> = {
  principal: '本人',
  other_user: '其他已识别用户',
  local_agent: '本机 Agent',
  unknown_remote: '未识别远程请求',
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function statusColor(status: number): string {
  if (status >= 500) return 'red'
  if (status >= 400) return 'orange'
  return 'green'
}

export default function SecuritySettings() {
  const { navigate } = usePageContext()
  const [view, setView] = useState<SecuritySettingsView>()
  const [draftP2PEnabled, setDraftP2PEnabled] = useState(false)
  const [draftAutoP2PTopN, setDraftAutoP2PTopN] = useState(0)
  const [lastAutoP2PTopN, setLastAutoP2PTopN] = useState(20)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [success, setSuccess] = useState<string>()
  const [events, setEvents] = useState<AccessAuditEvent[]>([])
  const [auditLoading, setAuditLoading] = useState(true)
  const [auditError, setAuditError] = useState<string>()
  const [days, setDays] = useState<1 | 7 | 30>(7)
  const [actorKind, setActorKind] = useState<AccessAuditActorKind>()
  const [operation, setOperation] = useState<AccessAuditOperation>()
  const [route, setRoute] = useState('')
  const [resource, setResource] = useState('')

  const reloadSettings = useCallback(() => {
    const controller = new AbortController()
    setLoading(true)
    getSecuritySettings(controller.signal)
      .then((result) => {
        setView(result)
        setDraftP2PEnabled(result.settings.p2p_scan_enabled)
        setDraftAutoP2PTopN(result.settings.auto_related_p2p_top_n)
        if (result.settings.auto_related_p2p_top_n > 0) setLastAutoP2PTopN(result.settings.auto_related_p2p_top_n)
        setError(undefined)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [])

  const reloadAudit = useCallback(() => {
    const controller = new AbortController()
    setAuditLoading(true)
    listSecurityAuditEvents({ days, actorKind, operation, route, resource }, controller.signal)
      .then((result) => {
        setEvents(result.items)
        setAuditError(undefined)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setAuditError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setAuditLoading(false)
      })
    return () => controller.abort()
  }, [actorKind, days, operation, resource, route])

  useEffect(reloadSettings, [reloadSettings])
  useEffect(reloadAudit, [reloadAudit])

  const save = async () => {
    setSaving(true)
    try {
      const updated = await updateSecuritySettings({
        p2p_scan_enabled: draftP2PEnabled,
        auto_related_p2p_top_n: draftAutoP2PTopN,
      })
      setView(updated)
      if (updated.settings.auto_related_p2p_top_n > 0) setLastAutoP2PTopN(updated.settings.auto_related_p2p_top_n)
      setError(undefined)
      setSuccess('安全设置已保存')
      reloadAudit()
    } catch (cause: unknown) {
      setSuccess(undefined)
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  const columns = useMemo<TableColumnsType<AccessAuditEvent>>(() => [
    {
      title: '时间',
      dataIndex: 'occurred_at',
      width: 170,
      render: (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false }),
    },
    {
      title: '访问者',
      width: 180,
      render: (_, event) => (
        <div>
          <Text strong>{actorLabels[event.actor_kind] || event.actor_kind}</Text>
          <div><Text type="secondary" ellipsis style={{ maxWidth: 170 }}>{event.actor_id}</Text></div>
        </div>
      ),
    },
    {
      title: '操作',
      dataIndex: 'operation',
      width: 90,
      render: (value: AccessAuditOperation) => (
        <Tag color={value === 'read' ? 'blue' : 'gold'}>{value === 'read' ? '读取' : '写入'}</Tag>
      ),
    },
    {
      title: '资源',
      width: 260,
      render: (_, event) => (
        <div>
          <Text code>{event.route}</Text>
          {event.resource_id && <div><Text type="secondary">ID {event.resource_id}</Text></div>}
        </div>
      ),
    },
    {
      title: '结果',
      dataIndex: 'status_code',
      width: 90,
      render: (value: number) => <Tag color={statusColor(value)}>{value}</Tag>,
    },
    {
      title: '来源',
      dataIndex: 'remote_address',
      width: 170,
      render: (value: string) => <Text type="secondary">{value || '—'}</Text>,
    },
  ], [])

  if (loading && !view) {
    return (
      <div className="security-page">
        <div className="security-settings-loading"><Spin /></div>
      </div>
    )
  }

  const dirty = view ? draftP2PEnabled !== view.settings.p2p_scan_enabled ||
    draftAutoP2PTopN !== view.settings.auto_related_p2p_top_n : false

  return (
    <div className="security-page">
      <div className="security-settings">
      {view?.restart_required && (
        <Alert
          type="warning"
          showIcon
          title="安全设置已保存，重启主服务后生效"
          description="重启前仍按当前进程中的旧设置运行。"
        />
      )}
      {success && <Alert type="success" showIcon title={success} closable onClose={() => setSuccess(undefined)} />}
      {error && <Alert type="error" showIcon title="安全设置操作失败" description={error} closable onClose={() => setError(undefined)} />}

      <div className="security-policy-grid">
        <Card className="security-policy-card" variant="borderless">
          <Flex justify="space-between" align="flex-start" gap={20}>
            <Space align="start" size={14}>
              <span className="security-policy-icon"><MessageOutlined /></span>
              <div>
                <Title level={4}>单聊消息扫描</Title>
                <Text type="secondary">
                  控制 Jarvis 是否主动发现和增量扫描飞书单聊。关闭后群聊和话题不受影响，历史数据保留。
                </Text>
              </div>
            </Space>
            <Switch
              checked={draftP2PEnabled}
              onChange={setDraftP2PEnabled}
              checkedChildren="允许"
              unCheckedChildren="禁止"
            />
          </Flex>
          <Flex className="security-policy-actions" justify="flex-end">
            <Button type="primary" disabled={!dirty} loading={saving} onClick={save}>保存单聊设置</Button>
          </Flex>
        </Card>

        <Card className="security-policy-card" variant="borderless">
          <Flex justify="space-between" align="flex-start" gap={20}>
            <Space align="start" size={14}>
              <span className="security-policy-icon"><SafetyCertificateOutlined /></span>
              <div>
                <Title level={4}>自动纳入活跃单聊</Title>
                <Text type="secondary">关闭后不再按活跃度自动监听单聊；人工固定监听的单聊不受影响。</Text>
                {draftAutoP2PTopN > 0 && (
                  <div style={{ marginTop: 12 }}>
                    <Text type="secondary" style={{ marginRight: 8 }}>自动监听数量</Text>
                    <InputNumber
                      min={1}
                      max={500}
                      value={draftAutoP2PTopN}
                      onChange={(value) => {
                        const next = value ?? 1
                        setDraftAutoP2PTopN(next)
                        setLastAutoP2PTopN(next)
                      }}
                    />
                  </div>
                )}
              </div>
            </Space>
            <Switch
              checked={draftAutoP2PTopN > 0}
              onChange={(enabled) => setDraftAutoP2PTopN(enabled ? lastAutoP2PTopN : 0)}
              checkedChildren="开启"
              unCheckedChildren="关闭"
            />
          </Flex>
          <Flex className="security-policy-actions" justify="flex-end">
            <Button type="primary" disabled={!dirty} loading={saving} onClick={save}>保存单聊设置</Button>
          </Flex>
        </Card>

        <Card className="security-policy-card" variant="borderless">
          <Flex justify="space-between" align="center" gap={20}>
            <Space align="start" size={14}>
              <span className="security-policy-icon"><FileProtectOutlined /></span>
              <div>
                <Title level={4}>会话排除名单</Title>
                <Text type="secondary">在世界会话列表中批量排除单聊、群聊或话题。历史数据保留，后台不再采集新消息。</Text>
              </div>
            </Space>
            <Button onClick={() => navigate('background', { view: 'groups', capture: 'excluded' })}>管理排除名单</Button>
          </Flex>
        </Card>

        <Card className="security-policy-card is-pending" variant="borderless">
          <Flex justify="space-between" align="flex-start" gap={20}>
            <Space align="start" size={14}>
              <span className="security-policy-icon"><FileProtectOutlined /></span>
              <div>
                <Title level={4}>L4 敏感文档读取</Title>
                <Text type="secondary">只控制 Jarvis 是否读取内容，不修改或降低飞书文档密级。</Text>
              </div>
            </Space>
            <Tag color="default">尚不可强制</Tag>
          </Flex>
        </Card>
      </div>

      <Card className="security-audit-card" variant="borderless">
        <Flex className="security-audit-header" justify="space-between" align="flex-start" gap={16} wrap>
          <Space align="start" size={14}>
            <span className="security-policy-icon"><AuditOutlined /></span>
            <div>
              <Title level={4}>项目数据访问审计</Title>
              <Text type="secondary">仅记录访问元数据，不保存请求正文、响应正文、消息内容或文档内容；固定保留 30 天。</Text>
            </div>
          </Space>
          <Flex gap={8} wrap>
            <Select
              value={days}
              onChange={setDays}
              options={[
                { value: 1, label: '最近 24 小时' },
                { value: 7, label: '最近 7 天' },
                { value: 30, label: '最近 30 天' },
              ]}
              style={{ width: 120 }}
            />
            <Select
              allowClear
              placeholder="全部身份"
              value={actorKind}
              onChange={setActorKind}
              options={Object.entries(actorLabels).map(([value, label]) => ({ value, label }))}
              style={{ width: 160 }}
            />
            <Select
              allowClear
              placeholder="全部操作"
              value={operation}
              onChange={setOperation}
              options={[
                { value: 'read', label: '读取' },
                { value: 'write', label: '写入' },
              ]}
              style={{ width: 120 }}
            />
            <Input
              allowClear
              aria-label="按路由筛选"
              placeholder="路由，如 /api/tasks"
              value={route}
              onChange={(event) => setRoute(event.target.value)}
              style={{ width: 190 }}
            />
            <Input
              allowClear
              aria-label="按资源筛选"
              placeholder="资源类型或 ID"
              value={resource}
              onChange={(event) => setResource(event.target.value)}
              style={{ width: 150 }}
            />
            <Button icon={<ReloadOutlined />} onClick={reloadAudit} loading={auditLoading}>刷新</Button>
          </Flex>
        </Flex>
        <Alert
          className="security-audit-boundary"
          type="info"
          showIcon
          icon={<SafetyCertificateOutlined />}
          title="当前身份识别边界"
          description="通过当前服务器字节身份完成登录的浏览器记为本人；无 Cookie 的本机调用记为本机 Agent；未登录远程请求会被拒绝并记为未识别远程请求。来源 IP 仅保留脱敏网段。"
        />
        {auditError && <Alert type="error" showIcon title="读取审计记录失败" description={auditError} />}
        <Table<AccessAuditEvent>
          rowKey="id"
          columns={columns}
          dataSource={events}
          loading={auditLoading}
          pagination={false}
          scroll={{ x: 900 }}
          locale={{ emptyText: '当前筛选范围内暂无访问记录' }}
        />
      </Card>
      </div>
    </div>
  )
}
