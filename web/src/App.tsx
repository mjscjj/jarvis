import { lazy, Suspense, useEffect, useState } from 'react'
import { Alert, Avatar, Badge, Button, Divider, Drawer, Input, Layout, Menu, Modal, Popover, Result, Spin, Tooltip, Typography, message } from 'antd'
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
import { listAppModules, listPluginInstallations, shutdownJarvis } from './api'
import { appModuleRegistry } from './modules/registry'
import type { AppModuleChildDefinition, AppModuleDefinition } from './modules/registry'
import { isWeeklyShareViewState } from './okr/emily/share'
import { getAuthStatus as getOKRAuthStatus } from './okr/emily/api'
import type { AuthUser as OKRAuthUser } from './okr/emily/types'
import { useExecutingTaskCount } from './hooks/useExecutingTaskCount'
import type { Plugin } from './types'
import { AgentActivityIcon } from './components/AgentActivityIcon'
import { mainWorkbenchURL, redirectPreferredWorkbench } from './instanceNavigation'
import { pageHash } from './pageRoutes'

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
const EMPTY_MODULE_ENABLEMENT: Readonly<Record<string, boolean>> = {}

function moduleChildMenuKey(moduleKey: string, childKey: string): string {
  return `${moduleKey}:${childKey}`
}

function enabledModuleChildren(
  module: AppModuleDefinition,
  moduleEnablement: Readonly<Record<string, boolean>>,
  managementAccess: boolean,
): readonly AppModuleChildDefinition[] {
  return module.children?.filter((child) => (
    (!child.requiresModule || moduleEnablement[child.requiresModule] === true) &&
    (!child.requiresManagementAccess || managementAccess)
  )) ?? []
}

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
  ...Object.fromEntries(appModuleRegistry.map((module) => [module.key, module.label])),
}

function AppShell() {
  const { name: agentName, rename: renameAgent } = useAgentIdentity()
  const { loading: authLoading, enabled: authEnabled, user, logout, preferMainWorkbench } = useAuth()
  const { context, navigate } = usePageContext()
  const weeklyShare = context.active_key === 'biz-okr' && isWeeklyShareViewState(context.view_state)
  const principalDataEnabled = !authEnabled || user !== null
  const mainWorkbench = mainWorkbenchURL()
  const runtimeFailures = useRuntimeFailureCount(principalDataEnabled && !preferMainWorkbench)
  const executingTasks = useExecutingTaskCount(principalDataEnabled && !preferMainWorkbench)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH
  // Preserve an existing main-branch plugin choice when initializing the
  // unified section preference. New users start with every section closed.
  const [legacyPluginsOpen] = useLocalStorage('jarvis.pluginsOpen', false)
  const [openMenuKeys, setOpenMenuKeys] = useLocalStorage<string[]>('jarvis.openMenuKeys', legacyPluginsOpen ? ['plugin-group'] : [])
  const [pluginsLoaded, setPluginsLoaded] = useState(false)
  const [mobileSystemOpen, setMobileSystemOpen] = useState(false)
  const [mobileModuleKey, setMobileModuleKey] = useState<string>()
  const [moduleEnablement, setModuleEnablement] = useState<Record<string, boolean>>()
  const [okrManagementAccess, setOKRManagementAccess] = useState(false)
  const [okrUser, setOKRUser] = useState<OKRAuthUser>()
  const [moduleLoadError, setModuleLoadError] = useState<string>()
  const [enabledPlugins, setEnabledPlugins] = useState<Array<Pick<Plugin, 'id' | 'name' | 'kind' | 'enabled'>>>([])
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

  const enabledModules = appModuleRegistry.filter((module) => moduleEnablement?.[module.key])
  const resolvedModuleEnablement = moduleEnablement ?? EMPTY_MODULE_ENABLEMENT
  const moduleNavigationTargets = enabledModules.flatMap((module) => (
    enabledModuleChildren(module, resolvedModuleEnablement, okrManagementAccess).map((child) => ({
      menuKey: moduleChildMenuKey(module.key, child.key),
      module,
      child,
    }))
  ))
  const activeModule = enabledModules.find((module) => module.key === context.active_key)
  const activeModuleChildren = activeModule ? enabledModuleChildren(activeModule, resolvedModuleEnablement, okrManagementAccess) : []
  const activeModuleChild = activeModuleChildren.find((child) => (
    Object.entries(child.viewState).every(([key, value]) => context.view_state[key] === value)
  )) ?? activeModuleChildren[0]
  const selectedMenuKey = activeModule && activeModuleChild
    ? moduleChildMenuKey(activeModule.key, activeModuleChild.key)
    : context.active_key
  const currentPageLabel = activeModule && activeModuleChild
    ? `${activeModule.label} · ${activeModuleChild.label}`
    : pageLabels[context.active_key] || 'Jarvis'

  useEffect(() => {
    setEnabledPlugins([])
    setPluginsLoaded(false)
    if (!principalDataEnabled || preferMainWorkbench) return
    let request: AbortController | undefined
    const refreshPlugins = async () => {
      request?.abort()
      const controller = new AbortController()
      request = controller
      try {
        const result = await listPluginInstallations(controller.signal)
        if (!controller.signal.aborted) setEnabledPlugins(result.items.filter((item) => item.enabled))
      } catch {
        // The plugin page owns visible API errors; navigation keeps its last good state.
      } finally {
        if (!controller.signal.aborted) setPluginsLoaded(true)
      }
    }
    void refreshPlugins()
    const onChanged = () => {
      setOpenMenuKeys((keys) => keys.includes('plugin-group') ? keys : [...keys, 'plugin-group'])
      void refreshPlugins()
    }
    window.addEventListener('jarvis:plugins-changed', onChanged)
    return () => {
      request?.abort()
      window.removeEventListener('jarvis:plugins-changed', onChanged)
    }
  }, [principalDataEnabled, preferMainWorkbench, setOpenMenuKeys])

  const enabledPluginPages = [
    ...(moduleEnablement?.okr ? [{ id: 'okr', name: 'OKR 插件' }] : []),
    ...enabledPlugins.map((plugin) => ({ id: plugin.id, name: plugin.name })),
  ]
  const pluginMenu: NonNullable<MenuProps['items']>[number] = enabledPluginPages.length > 0
    ? {
        key: 'plugin-group',
        label: '插件',
        icon: <ApiOutlined />,
        children: [
          { key: 'plugins', label: '插件管理' },
          ...enabledPluginPages.map((plugin) => ({ key: `plugin:${plugin.id}`, label: plugin.name })),
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
    ...enabledModules.map((module) => {
      const children = enabledModuleChildren(module, resolvedModuleEnablement, okrManagementAccess)
      if (children.length === 0) return { key: module.key, label: module.label, icon: module.icon }
      return {
        key: module.key,
        label: module.label,
        icon: module.icon,
        children: children.map((child, index) => ({
          key: moduleChildMenuKey(module.key, child.key),
          label: child.label,
          className: index > 0 && children[index - 1].group !== child.group ? 'app-menu-child-group-start' : undefined,
        })),
      }
    }),
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
    ...Object.fromEntries(enabledModules.map((module) => [
      module.key,
      <module.Page key={module.key} moduleEnablement={resolvedModuleEnablement} />,
    ])),
  }

  useEffect(() => {
    let controller = new AbortController()
    const reload = () => {
      controller.abort()
      controller = new AbortController()
      listAppModules(controller.signal)
        .then((result) => {
          setModuleEnablement(Object.fromEntries(result.items.map((item) => [item.key, item.is_enabled])))
          setModuleLoadError(undefined)
        })
        .catch((cause: unknown) => {
          if (!(cause instanceof DOMException && cause.name === 'AbortError')) {
            setModuleLoadError(cause instanceof Error ? cause.message : String(cause))
          }
        })
    }
    reload()
    window.addEventListener('jarvis:app-modules-changed', reload)
    return () => {
      controller.abort()
      window.removeEventListener('jarvis:app-modules-changed', reload)
    }
  }, [])

  useEffect(() => {
    const refresh = () => {
      void getOKRAuthStatus()
        .then((auth) => {
          setOKRManagementAccess(auth.managementAccess)
          setOKRUser(auth.authenticated ? auth.user : undefined)
        })
        .catch(() => {
          setOKRManagementAccess(false)
          setOKRUser(undefined)
        })
    }
    refresh()
    window.addEventListener('jarvis:okr-auth-changed', refresh)
    return () => window.removeEventListener('jarvis:okr-auth-changed', refresh)
  }, [])

  useEffect(() => {
    if (!moduleEnablement) return
    const registeredActiveModule = appModuleRegistry.find((module) => module.key === context.active_key)
    if (registeredActiveModule && !moduleEnablement[registeredActiveModule.key]) navigate('overview')
  }, [context.active_key, moduleEnablement, navigate])

  const goTo = (key: string) => {
    setAgentMenuOpen(false)
    setAccountMenuOpen(false)
    setMobileSystemOpen(false)
    setMobileModuleKey(undefined)
    const target = moduleNavigationTargets.find((item) => item.menuKey === key)
    if (target) {
	  if (redirectPreferredWorkbench(preferMainWorkbench, pageHash(target.module.key, null, target.child.viewState), false)) return
      navigate(target.module.key, target.child.viewState)
      return
    }
    const mainKey = key.startsWith('plugin:') ? 'plugins' : key
    const mainState: Record<string, string> = key.startsWith('plugin:') ? { plugin: key.slice('plugin:'.length) } : {}
    if (redirectPreferredWorkbench(preferMainWorkbench, pageHash(mainKey, null, mainState), false)) return
    if (key.startsWith('plugin:')) {
      navigate('plugins', { plugin: key.slice('plugin:'.length) })
      return
    }
    navigate(key)
  }

  function confirmShutdown() {
    modal.confirm({
      title: `退出 ${agentName}？`,
      content: '这会停止当前 Jarvis 实例的服务，正在执行的任务也会被中断。',
      okText: '退出当前实例',
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
          subTitle="当前 Jarvis 实例正在停止，可以关闭此页面。"
        />
      </>
    )
  }

  const mobileModule = enabledModules.find((module) => module.key === mobileModuleKey)
  const mobileModuleChildren = mobileModule ? enabledModuleChildren(mobileModule, resolvedModuleEnablement, okrManagementAccess) : []
  const mobileNavItems: Array<{
    key: string
    label: string
    icon: React.ReactNode
    children?: readonly AppModuleChildDefinition[]
  }> = [
    { key: 'chat', label: '对话', icon: <MessageOutlined /> },
    { key: 'overview', label: '工作台', icon: <HomeOutlined /> },
    { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
    ...enabledModules.map((module) => ({
      key: module.key,
      label: module.label,
      icon: module.icon,
      children: enabledModuleChildren(module, resolvedModuleEnablement, okrManagementAccess),
    })),
    { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
  ]
  const primaryNavigationKey = context.active_key === 'progress'
    ? 'overview'
    : context.active_key === 'scheduled-tasks'
      ? 'tasks'
      : selectedMenuKey
  const mobileNavigationKey = context.active_key === 'progress'
    ? 'overview'
    : context.active_key === 'scheduled-tasks'
      ? 'tasks'
      : context.active_key

  return (
    <Layout
      className="app-shell"
      style={{
        '--sider-width': weeklyShare ? '0px' : `${siderWidth}px`,
      } as React.CSSProperties}
    >
      {modalContext}
      {messageContext}
      {!weeklyShare && <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className={`sider-brand ${siderCollapsed ? 'is-collapsed' : ''}`}>
          <Popover
            trigger="click"
            placement="bottomLeft"
            open={!preferMainWorkbench && agentMenuOpen}
            onOpenChange={(open) => {
              if (preferMainWorkbench) return
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
                  navigate('settings', { view: 'about' })
                }}>关于与更新</Button>
              </div>
            )}
          >
            <AgentActivityIcon name={preferMainWorkbench ? '工作台' : agentName} enabled={principalDataEnabled && !preferMainWorkbench} {...executingTasks} tooltipDisabled={agentMenuOpen} />
          </Popover>
          {!siderCollapsed && (
            <div className="sider-brand-copy">
              <div className="sider-name-row"><Title level={4}>{preferMainWorkbench ? '工作台' : agentName}</Title></div>
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
          openKeys={siderCollapsed ? [] : openMenuKeys}
          onOpenChange={(keys) => { if (!siderCollapsed) setOpenMenuKeys(keys.map(String)) }}
          items={menuProps}
          onClick={({ key }) => goTo(key)}
          className="app-menu"
        />
        <div className={`sider-footer ${siderCollapsed ? 'is-collapsed' : ''}`}>
          {mainWorkbench && !preferMainWorkbench && <a className="workbench-return-link" href={mainWorkbench}>返回主工作台</a>}
          {authEnabled && user && <Popover
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
                <Badge status="success" text="已登录当前实例" />
                <Divider />
                <Button type="text" block icon={<LogoutOutlined />} onClick={() => void handleLogout()}>退出登录</Button>
              </div>
            )}
          >
            <Button type="text" className="sider-account-trigger" aria-label={`${accountName}，打开账号菜单`}>
              <Avatar size={26}>{accountInitial}</Avatar>
              {!siderCollapsed && <><span className="sider-account-name">{accountName}</span><DownOutlined /></>}
            </Button>
          </Popover>}
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
      </Sider>}
      {!weeklyShare && <header className="mobile-topbar">
        <strong>{currentPageLabel}</strong>
        <Button type="text" icon={<MoreOutlined />} aria-label="打开系统导航" onClick={() => setMobileSystemOpen(true)} />
      </header>}
      <Layout>
        <div className={`app-main ${context.active_key === 'chat' ? 'is-chat-page' : ''}`}>
          <Content className={`app-content ${context.active_key === 'chat' ? 'is-chat-page' : ''}`}>
            {moduleLoadError && <Alert className="app-module-load-error" type="error" showIcon title="功能模块配置读取失败" description={moduleLoadError} />}
            <Suspense fallback={<div className="page-loading"><Spin size="small" /><span>正在加载…</span></div>}>
              {pages[context.active_key]}
            </Suspense>
            <Suspense fallback={null}>
              {principalDataEnabled &&
                <Chat key={`principal-chat:${user?.username ?? 'local'}`} compact={context.active_key !== 'chat'} expandInPlace={preferMainWorkbench} />}
              {!authLoading && okrUser && !principalDataEnabled &&
                <Chat key={`okr-chat:${okrUser.unionId || okrUser.openId}`} compact isolated hidden={context.active_key !== 'biz-okr'} />}
            </Suspense>
          </Content>
        </div>
      </Layout>
      {!weeklyShare && <nav className="mobile-bottom-nav" aria-label="主要导航" style={{ gridTemplateColumns: `repeat(${mobileNavItems.length}, 1fr)` }}>
        {mobileNavItems.map((item) => (
          <button
            key={item.key}
            type="button"
            className={mobileNavigationKey === item.key ? 'is-active' : ''}
            onClick={() => item.children?.length ? setMobileModuleKey(item.key) : goTo(item.key)}
          >
            {item.icon}<span>{item.label}</span>
          </button>
        ))}
      </nav>}
      {!weeklyShare && <Drawer
        className="mobile-module-drawer"
        title={mobileModule?.label}
        placement="bottom"
        height="auto"
        open={Boolean(mobileModule)}
        onClose={() => setMobileModuleKey(undefined)}
      >
        <div className="mobile-system-links">
          {mobileModuleChildren.map((child) => {
            const menuKey = moduleChildMenuKey(mobileModule!.key, child.key)
            return (
              <Button key={child.key} type={selectedMenuKey === menuKey ? 'primary' : 'text'} onClick={() => goTo(menuKey)}>
                {child.label}
              </Button>
            )
          })}
        </div>
      </Drawer>}
      {!weeklyShare && <Drawer
        className="mobile-system-drawer"
        title="系统"
        placement="right"
        size="min(360px, 100vw)"
        open={mobileSystemOpen}
        onClose={() => setMobileSystemOpen(false)}
      >
        <div className="mobile-system-links">
          {mainWorkbench && !preferMainWorkbench && <a className="workbench-return-link" href={mainWorkbench}>返回主工作台</a>}
          {authEnabled && user && <div className="mobile-account">
            <Avatar size={32}>{accountInitial}</Avatar>
            <div>
              <strong>{accountName}</strong>
              <span>{user?.email}</span>
            </div>
          </div>}
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
          {authEnabled && user && <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>退出登录</Button>}
        </div>
      </Drawer>}
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
      <PageContextProvider initialKey={DEFAULT_KEY}>
        <WorkspaceEntry />
      </PageContextProvider>
    </AuthGate>
  )
}

function WorkspaceEntry() {
  const { context } = usePageContext()
  // Modules own visitor setup. Personal installation and world modeling only
  // run on the principal's workbench, including when navigating within the SPA.
  if (appModuleRegistry.some(module => module.key === context.active_key)) return <AppShell />
  return <OnboardingGate><AppShell /></OnboardingGate>
}
