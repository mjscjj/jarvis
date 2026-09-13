import { lazy, Suspense, useCallback, useEffect, useState } from 'react'
import { Avatar, Badge, Button, Divider, Drawer, Input, Layout, Menu, Modal, Popover, Result, Spin, Tooltip, Typography, message } from 'antd'
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
  ApiOutlined,
  CheckOutlined,
  CloseOutlined,
  DownOutlined,
  EditOutlined,
  InfoCircleOutlined,
  LogoutOutlined,
} from '@ant-design/icons'
import { AppUpdateProvider } from './AppUpdate'
import { AgentIdentityProvider, useAgentIdentity } from './agentIdentity'
import { AuthGate, AuthProvider, useAuth } from './auth'
import { OnboardingGate } from './Onboarding'
import { PageContextProvider, usePageContext } from './pageContext'
import { useLocalStorage } from './hooks/useLocalStorage'
import { useRuntimeFailureCount } from './hooks/useRuntimeFailureCount'
import { useExecutingTaskCount } from './hooks/useExecutingTaskCount'
import { listPluginInstallations, shutdownJarvis } from './api'
import type { Plugin } from './types'
import { AgentActivityIcon } from './components/AgentActivityIcon'

const { Sider, Content } = Layout
const { Title } = Typography

const Delegations = lazy(() => import('./Delegations'))
const Tasks = lazy(() => import('./Tasks'))
const Progress = lazy(() => import('./Progress'))
const Background = lazy(() => import('./Background'))
const Settings = lazy(() => import('./Background').then((module) => ({ default: module.Settings })))
const AgentSettings = lazy(() => import('./AgentSettings'))
const Todos = lazy(() => import('./Todos'))
const ScheduledTasks = lazy(() => import('./ScheduledTasks'))
const Debug = lazy(() => import('./Debug'))
const Chat = lazy(() => import('./Chat'))
const Plugins = lazy(() => import('./Plugins'))

const DEFAULT_KEY = 'chat'

const SIDER_WIDTH = 184
const SIDER_COLLAPSED_WIDTH = 64
const pageLabels: Record<string, string> = {
  chat: '对话',
  overview: '工作台',
  tasks: '任务',
  progress: '工作台',
  background: '世界',
  todos: '线索',
  'scheduled-tasks': '任务',
  plugins: '插件',
  agents: '工作设定',
  settings: '系统设置',
  debug: '运行状态',
}

function AppShell() {
  const { name: agentName, rename: renameAgent } = useAgentIdentity()
  const { user, logout } = useAuth()
  const { context, navigate } = usePageContext()
  const runtimeFailures = useRuntimeFailureCount()
  const executingTasks = useExecutingTaskCount()
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const [managementOpen, setManagementOpen] = useState(() => ['todos', 'agents', 'settings', 'debug'].includes(context.active_key))
  const [pluginsOpen, setPluginsOpen] = useLocalStorage('jarvis.pluginsOpen', true)
  const [enabledPlugins, setEnabledPlugins] = useState<Array<Pick<Plugin, 'id' | 'name' | 'kind' | 'enabled'>>>([])
  const [pluginsLoaded, setPluginsLoaded] = useState(false)
  const [mobileSystemOpen, setMobileSystemOpen] = useState(false)
  const [agentMenuOpen, setAgentMenuOpen] = useState(false)
  const [accountMenuOpen, setAccountMenuOpen] = useState(false)
  const [shuttingDown, setShuttingDown] = useState(false)
  const [editingName, setEditingName] = useState(false)
  const [nameDraft, setNameDraft] = useState(agentName)
  const [savingName, setSavingName] = useState(false)
  const [modal, modalContext] = Modal.useModal()
  const [messageApi, messageContext] = message.useMessage()

  let managementIcon: React.ReactNode = <SettingOutlined />
  if (runtimeFailures.count && runtimeFailures.count > 0) {
    managementIcon = <Badge dot status="error">{managementIcon}</Badge>
  } else if (runtimeFailures.error) {
    managementIcon = <Tooltip title={`运行状态读取失败：${runtimeFailures.error}`}><Badge status="error" dot>{managementIcon}</Badge></Tooltip>
  }

  const refreshPlugins = useCallback(async () => {
    try {
      const result = await listPluginInstallations()
      setEnabledPlugins(result.items.filter((item) => item.enabled))
    } catch {
      // The plugin page owns visible API errors; navigation keeps its last good state.
    } finally {
      setPluginsLoaded(true)
    }
  }, [])

  useEffect(() => {
    void refreshPlugins()
    const onChanged = () => {
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
  const delegationsEnabled = pluginsLoaded
    ? enabledPlugins.some((plugin) => plugin.id === 'my-delegations')
    : null

  const activityLabel = executingTasks.error
    ? '任务状态读取失败'
    : executingTasks.count === undefined
      ? '正在读取任务状态'
      : executingTasks.count > 0
        ? `正在执行 ${executingTasks.count} 个任务`
        : '当前空闲'
  const accountName = user?.username || user?.email || '当前用户'
  const accountInitial = Array.from(accountName.trim())[0]?.toUpperCase() || '我'

  const menuProps: MenuProps['items'] = [
    { key: 'chat', label: '对话', icon: <MessageOutlined /> },
    { key: 'overview', label: '工作台', icon: <HomeOutlined /> },
    { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
    { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
    pluginMenu,
    { type: 'divider' },
    {
      key: 'management',
      label: '系统',
      icon: managementIcon,
      children: [
        { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
        { key: 'agents', label: '工作设定', icon: <EditOutlined /> },
        { key: 'settings', label: '系统设置', icon: <SettingOutlined /> },
        { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
      ],
    },
  ]

  const pages: Record<string, React.ReactNode> = {
    chat: null,
    overview: <Progress />,
    todos: <Todos refreshKey={0} />,
    tasks: context.view_state.mode === 'delegated' && context.selection?.kind !== 'task' && delegationsEnabled !== false
      ? delegationsEnabled === null ? <Spin /> : <Delegations />
      : <Tasks delegationsEnabled={delegationsEnabled === true} />,
    'scheduled-tasks': <ScheduledTasks delegationsEnabled={delegationsEnabled === true} />,
    plugins: <Plugins />,
    background: <Background />,
    agents: <AgentSettings />,
    settings: <Settings onShutdown={confirmShutdown} />,
    progress: <Progress />,
    debug: <Debug />,
  }

  const goTo = (key: string) => {
    setAgentMenuOpen(false)
    setAccountMenuOpen(false)
    setMobileSystemOpen(false)
    if (['todos', 'agents', 'settings', 'debug'].includes(key)) setManagementOpen(true)
    if (key.startsWith('plugin:')) {
      navigate('plugins', { plugin: key.slice('plugin:'.length) })
      return
    }
    navigate(key)
  }

  function confirmShutdown() {
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
    setAccountMenuOpen(false)
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
      className="app-shell"
      style={{ '--sider-width': `${siderWidth}px` } as React.CSSProperties}
    >
      {modalContext}
      {messageContext}
      <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className={`sider-brand ${siderCollapsed ? 'is-collapsed' : ''}`}>
          <Popover
            trigger="click"
            placement="bottomLeft"
            open={agentMenuOpen}
            onOpenChange={(open) => {
              setAgentMenuOpen(open)
              if (!open) cancelNameEdit()
            }}
            content={(
              <div className="agent-menu">
                <div className="agent-menu-heading">
                  <strong>{agentName}</strong>
                  <Badge status={executingTasks.error ? 'error' : executingTasks.count === undefined ? 'processing' : executingTasks.count > 0 ? 'processing' : 'success'} text={activityLabel} />
                </div>
                {editingName ? (
                  <div className="agent-menu-name-editor">
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
                  </div>
                ) : (
                  <Button type="text" block onClick={() => {
                    setNameDraft(agentName)
                    setEditingName(true)
                  }}>修改名称</Button>
                )}
                <Divider />
                {Boolean(executingTasks.count) && (
                  <Button type="text" block icon={<PlayCircleOutlined />} onClick={() => goTo('tasks')}>查看运行中的任务</Button>
                )}
                <Button type="text" block icon={<EditOutlined />} onClick={() => goTo('agents')}>工作设定</Button>
                <Button type="text" block icon={<InfoCircleOutlined />} onClick={() => {
                  setAgentMenuOpen(false)
                  setManagementOpen(true)
                  navigate('settings', { view: 'about' })
                }}>关于与更新</Button>
              </div>
            )}
          >
            <AgentActivityIcon name={agentName} {...executingTasks} tooltipDisabled={agentMenuOpen} />
          </Popover>
          {!siderCollapsed && (
            <div className="sider-brand-copy">
              <div className="sider-name-row"><Title level={4}>{agentName}</Title></div>
              <div className="sider-agent-caption">你的主动式 Agent</div>
            </div>
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
            // Collapsing the whole sidebar also emits empty keys; preserve the user's section choices.
            if (siderCollapsed) return
            setManagementOpen(keys.includes('management'))
            if (enabledPlugins.length > 0) {
              setPluginsOpen(keys.includes('plugin-group'))
            }
          }}
          items={menuProps}
          onClick={({ key }) => goTo(key)}
          className="app-menu"
        />
        <div className={`sider-footer ${siderCollapsed ? 'is-collapsed' : ''}`}>
          <Popover
            trigger="click"
            placement="topLeft"
            open={accountMenuOpen}
            onOpenChange={setAccountMenuOpen}
            content={(
              <div className="account-menu">
                <div className="account-menu-profile">
                  <Avatar size={36}>{accountInitial}</Avatar>
                  <div>
                    <strong>{accountName}</strong>
                    <span>{user?.email}</span>
                  </div>
                </div>
                <Badge status="success" text="已通过字节身份登录" />
                <Divider />
                <Button type="text" block icon={<LogoutOutlined />} onClick={() => void handleLogout()}>退出登录</Button>
              </div>
            )}
          >
            <Button type="text" className="sider-account-trigger" aria-label={`${accountName}，打开账号菜单`}>
              <Avatar size={26}>{accountInitial}</Avatar>
              {!siderCollapsed && <><span className="sider-account-name">{accountName}</span><DownOutlined /></>}
            </Button>
          </Popover>
        </div>
        <Tooltip title={siderCollapsed ? '展开侧边栏' : '收起侧边栏'} placement="right">
          <Button
            type="text"
            className="sider-collapse-btn"
            aria-label={siderCollapsed ? '展开侧边栏' : '收起侧边栏'}
            icon={siderCollapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={() => setSiderCollapsed((value) => !value)}
          />
        </Tooltip>
      </Sider>
      <header className="mobile-topbar">
        <strong>{pageLabels[context.active_key] || agentName}</strong>
        <Button type="text" icon={<MoreOutlined />} aria-label="打开系统导航" onClick={() => setMobileSystemOpen(true)} />
      </header>
      <Layout>
        <div className={`app-main ${context.active_key === 'chat' ? 'is-chat-page' : ''}`}>
          <Content className={`app-content ${context.active_key === 'chat' ? 'is-chat-page' : ''}`}>
            <Suspense fallback={<div className="page-loading"><Spin size="small" /><span>正在加载…</span></div>}>
              {pages[context.active_key]}
            </Suspense>
            <Suspense fallback={null}>
              <Chat compact={context.active_key !== 'chat'} />
            </Suspense>
          </Content>
        </div>
      </Layout>
      <nav className="mobile-bottom-nav" aria-label="主要导航">
        {[
          { key: 'chat', label: '对话', icon: <MessageOutlined /> },
          { key: 'overview', label: '工作台', icon: <HomeOutlined /> },
          { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
          { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
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
            <Avatar size={32}>{accountInitial}</Avatar>
            <div>
              <strong>{accountName}</strong>
              <span>{user?.email}</span>
            </div>
          </div>
          {[
            { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
            { key: 'agents', label: '工作设定', icon: <EditOutlined /> },
            { key: 'settings', label: '系统设置', icon: <SettingOutlined /> },
            { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
          ].map((item) => (
            <Button key={item.key} type={context.active_key === item.key ? 'primary' : 'text'} icon={item.icon} onClick={() => goTo(item.key)}>
              {item.label}
            </Button>
          ))}
          <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>退出登录</Button>
        </div>
      </Drawer>
    </Layout>
  )
}

export default function App() {
  return (
    // The update prompt is independent of login and onboarding state.
    <AppUpdateProvider>
      <AgentIdentityProvider>
        <AuthProvider>
          <AuthenticatedApp />
        </AuthProvider>
      </AgentIdentityProvider>
    </AppUpdateProvider>
  )
}

function AuthenticatedApp() {
  const { name } = useAgentIdentity()
  return (
    <AuthGate agentName={name}>
      <OnboardingGate>
        <PageContextProvider initialKey={DEFAULT_KEY}>
          <AppShell />
        </PageContextProvider>
      </OnboardingGate>
    </AuthGate>
  )
}
