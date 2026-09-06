import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, Drawer, Input, Layout, Menu, Modal, Result, Spin, Tooltip, Typography, message } from 'antd'
import type { MenuProps } from 'antd'
import {
  HomeOutlined,
  CheckCircleOutlined,
  PlayCircleOutlined,
  SettingOutlined,
  ToolOutlined,
  MessageOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  DatabaseOutlined,
  MoreOutlined,
  PoweroffOutlined,
  RobotOutlined,
  ApiOutlined,
  CheckOutlined,
  CloseOutlined,
  EditOutlined,
  LogoutOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { AgentIdentityProvider, useAgentIdentity } from './agentIdentity'
import { AuthGate, AuthProvider, useAuth } from './auth'
import { PageContextProvider, usePageContext } from './pageContext'
import { useLocalStorage } from './hooks/useLocalStorage'
import { useRuntimeFailureCount } from './hooks/useRuntimeFailureCount'
import { listPlugins, shutdownJarvis } from './api'
import type { Plugin } from './types'

const { Sider, Content } = Layout
const { Title } = Typography

const Tasks = lazy(() => import('./Tasks'))
const Progress = lazy(() => import('./Progress'))
const Background = lazy(() => import('./Background'))
const AgentSettings = lazy(() => import('./AgentSettings'))
const Settings = lazy(() => import('./Background').then((module) => ({ default: module.Settings })))
const Todos = lazy(() => import('./Todos'))
const ScheduledTasks = lazy(() => import('./ScheduledTasks'))
const Debug = lazy(() => import('./Debug'))
const Chat = lazy(() => import('./Chat'))
const Plugins = lazy(() => import('./Plugins'))

const DEFAULT_KEY = 'overview'

const SIDER_WIDTH = 184
const SIDER_COLLAPSED_WIDTH = 64
const pageLabels: Record<string, string> = {
  overview: '工作台',
  tasks: '任务',
  progress: '工作台',
  background: '世界',
  agents: '工作设定',
  todos: '线索',
  'scheduled-tasks': '任务',
  plugins: '插件',
  settings: '系统设置',
  debug: '运行状态',
}

function AppShell() {
  const { name: agentName, shortName: agentShortName, rename: renameAgent } = useAgentIdentity()
  const { user, logout } = useAuth()
  const { context, navigate } = usePageContext()
  const runtimeFailures = useRuntimeFailureCount()
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOverlayOpen', false)
  const [chatLoaded, setChatLoaded] = useState(chatOpen)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const [managementOpen, setManagementOpen] = useState(true)
  const [pluginsOpen, setPluginsOpen] = useState(true)
  const [enabledPlugins, setEnabledPlugins] = useState<Plugin[]>([])
  const [mobileSystemOpen, setMobileSystemOpen] = useState(false)
  const [shuttingDown, setShuttingDown] = useState(false)
  const [editingName, setEditingName] = useState(false)
  const [nameDraft, setNameDraft] = useState(agentName)
  const [savingName, setSavingName] = useState(false)
  const [modal, modalContext] = Modal.useModal()
  const [messageApi, messageContext] = message.useMessage()
  const chatRef = useRef<HTMLElement>(null)
  const chatToggleRef = useRef<HTMLButtonElement>(null)
  const chatWasOpen = useRef(chatOpen)

  let managementIcon: React.ReactNode = <SettingOutlined />
  if (runtimeFailures.count && runtimeFailures.count > 0) {
    managementIcon = <Badge dot status="error">{managementIcon}</Badge>
  } else if (runtimeFailures.error) {
    managementIcon = <Tooltip title={`运行状态读取失败：${runtimeFailures.error}`}><Badge status="error" dot>{managementIcon}</Badge></Tooltip>
  }

  const refreshPlugins = useCallback(async () => {
    try {
      const result = await listPlugins()
      setEnabledPlugins(result.items.filter((item) => item.enabled))
    } catch {
      // The plugin page owns visible API errors; navigation keeps its last good state.
    }
  }, [])

  useEffect(() => {
    void refreshPlugins()
    const onChanged = () => {
      setPluginsOpen(true)
      void refreshPlugins()
    }
    window.addEventListener('jarvis:plugins-changed', onChanged)
    return () => window.removeEventListener('jarvis:plugins-changed', onChanged)
  }, [refreshPlugins])

  const pluginMenu: NonNullable<MenuProps['items']>[number] = enabledPlugins.length > 0
    ? {
        key: 'plugin-group',
        label: '插件',
        icon: <ApiOutlined />,
        children: [
          { key: 'plugins', label: '插件管理' },
          ...enabledPlugins.map((plugin) => ({ key: `plugin:${plugin.id}`, label: plugin.name })),
        ],
      }
    : { key: 'plugins', label: '插件', icon: <ApiOutlined /> }

  const menuProps: MenuProps['items'] = [
    { key: 'overview', label: '工作台', icon: <HomeOutlined /> },
    { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
    { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
    pluginMenu,
    { key: 'agents', label: '工作设定', icon: <RobotOutlined /> },
    { type: 'divider' },
    {
      key: 'management',
      label: '系统',
      icon: managementIcon,
      children: [
        { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
        { key: 'settings', label: '系统设置', icon: <SettingOutlined /> },
        { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
      ],
    },
  ]

  const pages: Record<string, React.ReactNode> = {
    overview: <Progress />,
    todos: <Todos refreshKey={0} />,
    tasks: <Tasks />,
    'scheduled-tasks': <ScheduledTasks />,
    plugins: <Plugins />,
    background: <Background />,
    agents: <AgentSettings />,
    settings: <Settings />,
    progress: <Progress />,
    debug: <Debug />,
  }

  useEffect(() => {
    if (chatOpen) setChatLoaded(true)
  }, [chatOpen])

  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setChatOpen(false)
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [setChatOpen])

  useEffect(() => {
    if (chatOpen) {
      window.requestAnimationFrame(() => {
        const target = chatRef.current?.querySelector<HTMLElement>('textarea, button, [href], [tabindex]:not([tabindex="-1"])')
        target?.focus()
      })
    } else if (chatWasOpen.current) {
      chatToggleRef.current?.focus()
    }
    chatWasOpen.current = chatOpen
  }, [chatOpen])

  const handleChatKeyDown = (event: React.KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'Tab') return
    const focusable = Array.from(chatRef.current?.querySelectorAll<HTMLElement>(
      'button:not([disabled]), textarea:not([disabled]), input:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
    ) ?? []).filter((element) => element.offsetParent !== null)
    if (focusable.length === 0) return
    const first = focusable[0]
    const last = focusable[focusable.length - 1]
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault()
      first.focus()
    }
  }

  const goTo = (key: string) => {
    setMobileSystemOpen(false)
    if (key.startsWith('plugin:')) {
      navigate('plugins', { plugin: key.slice('plugin:'.length) })
      return
    }
    navigate(key)
  }

  const confirmShutdown = () => {
    modal.confirm({
      title: `退出 ${agentName}？`,
      content: '这会停止 Jarvis Server、Qdrant、CC Connect 和开发 Web 服务，正在执行的任务也会被中断。',
      okText: '退出并停止服务',
      cancelText: '取消',
      okButtonProps: { danger: true },
      onOk: async () => {
        try {
          await shutdownJarvis()
          setMobileSystemOpen(false)
          setShuttingDown(true)
        } catch (cause) {
          messageApi.error(cause instanceof Error ? cause.message : String(cause))
          throw cause
        }
      },
    })
  }

  const cancelNameEdit = () => {
    setNameDraft(agentName)
    setEditingName(false)
  }

  const saveName = async () => {
    const next = nameDraft.trim()
    if (!next || next === agentName) {
      cancelNameEdit()
      return
    }
    setSavingName(true)
    try {
      const result = await renameAgent(next)
      setEditingName(false)
      messageApi.success(result.restartRequired ? '名称已保存，重启服务后将应用到所有 Agent' : '名称已更新')
    } catch (cause) {
      messageApi.error(cause instanceof Error ? cause.message : String(cause))
    } finally {
      setSavingName(false)
    }
  }

  const handleLogout = async () => {
    try {
      await logout()
    } catch (cause) {
      messageApi.error(cause instanceof Error ? cause.message : String(cause))
    }
  }

  if (shuttingDown) {
    return (
      <>
        {modalContext}
        {messageContext}
        <Result
          status="success"
          title={`${agentName} 已退出`}
          subTitle="Jarvis Server、Qdrant、CC Connect 和开发 Web 服务正在停止，可以关闭此页面。"
        />
      </>
    )
  }

  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH
  const primaryNavigationKey = context.active_key === 'progress'
    ? 'overview'
    : context.active_key === 'scheduled-tasks'
      ? 'tasks'
      : context.active_key

  return (
    <Layout
      className={`app-shell ${chatOpen ? 'chat-is-open' : ''}`}
      style={{ '--sider-width': `${siderWidth}px` } as React.CSSProperties}
    >
      {modalContext}
      {messageContext}
      <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className="sider-brand">
          {!siderCollapsed && <div className="sider-tagline">主动式任务分身</div>}
          {siderCollapsed ? (
            <Title level={4}>{agentShortName}</Title>
          ) : (
            <>
              <div className="sider-name-row">
                {editingName ? (
                  <>
                    <Input
                      size="small"
                      value={nameDraft}
                      maxLength={32}
                      autoFocus
                      aria-label="机器人名称"
                      onChange={(event) => setNameDraft(event.target.value)}
                      onPressEnter={() => void saveName()}
                      onKeyDown={(event) => {
                        if (event.key === 'Escape') cancelNameEdit()
                      }}
                    />
                    <Tooltip title="保存名称">
                      <Button type="text" size="small" icon={<CheckOutlined />} loading={savingName} onClick={() => void saveName()} />
                    </Tooltip>
                    <Tooltip title="取消">
                      <Button type="text" size="small" icon={<CloseOutlined />} disabled={savingName} onClick={cancelNameEdit} />
                    </Tooltip>
                  </>
                ) : (
                  <>
                    <Tooltip title={agentName}>
                      <Title level={4}>{agentName}</Title>
                    </Tooltip>
                    <Tooltip title="修改机器人名称">
                      <Button
                        type="text"
                        size="small"
                        icon={<EditOutlined />}
                        aria-label="修改机器人名称"
                        onClick={() => {
                          setNameDraft(agentName)
                          setEditingName(true)
                        }}
                      />
                    </Tooltip>
                  </>
                )}
              </div>
              <div className="sider-agent-caption">你的主动式 Agent</div>
            </>
          )}
        </div>
        <Menu
          mode="inline"
          inlineCollapsed={siderCollapsed}
          selectedKeys={[
            context.active_key === 'plugins' && context.view_state.plugin
              ? `plugin:${context.view_state.plugin}`
              : primaryNavigationKey,
          ]}
          openKeys={siderCollapsed ? [] : [
            ...(managementOpen ? ['management'] : []),
            ...(pluginsOpen && enabledPlugins.length > 0 ? ['plugin-group'] : []),
          ]}
          onOpenChange={(keys) => {
            setManagementOpen(keys.includes('management'))
            setPluginsOpen(keys.includes('plugin-group'))
          }}
          items={menuProps}
          onClick={({ key }) => goTo(key)}
          className="app-menu"
        />
        <div className={`sider-footer ${siderCollapsed ? 'is-collapsed' : ''}`}>
          <div className="sider-account">
            {!siderCollapsed && (
              <>
                <UserOutlined />
                <div className="sider-account-copy">
                  <strong>{user?.username}</strong>
                  <span>{user?.email}</span>
                </div>
              </>
            )}
            <Tooltip title={siderCollapsed ? `${user?.username ?? '当前用户'} · 退出登录` : '退出登录'} placement="right">
              <Button type="text" icon={<LogoutOutlined />} aria-label="退出登录" onClick={() => void handleLogout()} />
            </Tooltip>
          </div>
          <Tooltip title="退出并停止所有服务" placement="right">
            <Button className="sider-shutdown-btn" type="text" danger icon={<PoweroffOutlined />} aria-label={`退出 ${agentName}`} onClick={confirmShutdown}>
              {!siderCollapsed && '停止服务'}
            </Button>
          </Tooltip>
          <Tooltip title={siderCollapsed ? '展开侧边栏' : '收起侧边栏'} placement="right">
            <Button
              type="text"
              className="sider-collapse-btn"
              icon={siderCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => setSiderCollapsed((value) => !value)}
            />
          </Tooltip>
        </div>
      </Sider>
      <header className="mobile-topbar">
        <strong>{pageLabels[context.active_key] || agentName}</strong>
        <Button type="text" icon={<MoreOutlined />} aria-label="打开系统导航" onClick={() => setMobileSystemOpen(true)} />
      </header>
      <Layout>
        <div className="app-main">
          <Content className="app-content">
            <Suspense fallback={<div className="page-loading"><Spin size="small" /><span>正在加载…</span></div>}>
              {pages[context.active_key]}
            </Suspense>
          </Content>
          <aside
            ref={chatRef}
            className={`chat-overlay ${chatOpen ? 'is-open' : ''}`}
            aria-hidden={!chatOpen}
            inert={chatOpen ? undefined : true}
            onKeyDown={handleChatKeyDown}
          >
            {chatLoaded && (
              <Suspense fallback={<div className="page-loading"><Spin size="small" /><span>正在打开对话…</span></div>}>
                <Chat open={chatOpen} onClose={() => setChatOpen(false)} />
              </Suspense>
            )}
          </aside>
        </div>
      </Layout>
      <Tooltip title={chatOpen ? '收起对话' : '打开对话'}>
        <Button
          type="primary"
          shape="circle"
          size="large"
          icon={<MessageOutlined />}
          className={`chat-toggle ${chatOpen ? 'chat-open' : ''}`}
          ref={chatToggleRef}
          aria-label={chatOpen ? `关闭 ${agentName} 对话` : `打开 ${agentName} 对话`}
          onClick={() => setChatOpen((open) => !open)}
        />
      </Tooltip>
      <nav className="mobile-bottom-nav" aria-label="主要导航">
        {[
          { key: 'overview', label: '工作台', icon: <HomeOutlined /> },
          { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
          { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
          { key: 'agents', label: 'Agent', icon: <RobotOutlined /> },
        ].map((item) => (
          <button key={item.key} type="button" className={primaryNavigationKey === item.key ? 'is-active' : ''} onClick={() => goTo(item.key)}>
            {item.icon}<span>{item.label}</span>
          </button>
        ))}
      </nav>
      <Drawer
        className="mobile-system-drawer"
        title="系统"
        placement="right"
        size="min(360px, 100vw)"
        open={mobileSystemOpen}
        onClose={() => setMobileSystemOpen(false)}
      >
        <div className="mobile-system-links">
          <div className="mobile-account">
            <UserOutlined />
            <div>
              <strong>{user?.username}</strong>
              <span>{user?.email}</span>
            </div>
          </div>
          {[
            { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
            { key: 'settings', label: '系统设置', icon: <SettingOutlined /> },
            { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
          ].map((item) => (
            <Button key={item.key} type={context.active_key === item.key ? 'primary' : 'text'} icon={item.icon} onClick={() => goTo(item.key)}>
              {item.label}
            </Button>
          ))}
          <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>退出登录</Button>
          <Button danger icon={<PoweroffOutlined />} onClick={confirmShutdown}>退出并停止服务</Button>
        </div>
      </Drawer>
    </Layout>
  )
}

export default function App() {
  return (
    <AgentIdentityProvider>
      <AuthProvider>
        <AuthenticatedApp />
      </AuthProvider>
    </AgentIdentityProvider>
  )
}

function AuthenticatedApp() {
  const { name } = useAgentIdentity()
  return (
    <AuthGate agentName={name}>
      <PageContextProvider initialKey={DEFAULT_KEY}>
        <AppShell />
      </PageContextProvider>
    </AuthGate>
  )
}
