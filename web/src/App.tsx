import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { Alert, Badge, Button, Drawer, Input, Layout, Menu, Modal, Result, Spin, Tooltip, Typography, message } from 'antd'
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
  ApiOutlined,
  CheckOutlined,
  CloseOutlined,
  EditOutlined,
  LogoutOutlined,
  SafetyCertificateOutlined,
  UserOutlined,
} from '@ant-design/icons'
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
import type { Plugin } from './types'
import jarvisIcon from './assets/jarvis-icon.png'
import { DeveloperHelpButton } from './components/DeveloperDocuments'
import {
  CHAT_PANEL_WIDTH_STORAGE_KEY,
  DEFAULT_CHAT_PANEL_WIDTH,
  MIN_CHAT_PANEL_WIDTH,
  chatPanelWidthFromPointer,
  clampChatPanelWidth,
  maxChatPanelWidth,
} from './chatPanelSizing'

const { Sider, Content } = Layout
const { Title } = Typography

const Delegations = lazy(() => import('./Delegations'))
const Tasks = lazy(() => import('./Tasks'))
const Progress = lazy(() => import('./Progress'))
const Background = lazy(() => import('./Background'))
const Settings = lazy(() => import('./Background').then((module) => ({ default: module.Settings })))
const Todos = lazy(() => import('./Todos'))
const ScheduledTasks = lazy(() => import('./ScheduledTasks'))
const Debug = lazy(() => import('./Debug'))
const Chat = lazy(() => import('./Chat'))
const Plugins = lazy(() => import('./Plugins'))
const SecuritySettings = lazy(() => import('./SecuritySettings'))

const DEFAULT_KEY = 'overview'

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
  overview: '工作台',
  tasks: '任务',
  progress: '工作台',
  background: '世界',
  todos: '线索',
  'scheduled-tasks': '任务',
  plugins: '插件',
  security: '安全保护',
  settings: '系统设置',
  debug: '运行状态',
  ...Object.fromEntries(appModuleRegistry.map((module) => [module.key, module.label])),
}

function AppShell() {
  const { name: agentName, rename: renameAgent } = useAgentIdentity()
  const { enabled: authEnabled, user, logout } = useAuth()
  const { context, navigate } = usePageContext()
  const weeklyShare = context.active_key === 'biz-okr' && isWeeklyShareViewState(context.view_state)
  const runtimeFailures = useRuntimeFailureCount()
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOverlayOpen', false)
  const [storedChatWidth, setStoredChatWidth] = useLocalStorage(CHAT_PANEL_WIDTH_STORAGE_KEY, DEFAULT_CHAT_PANEL_WIDTH)
  const [chatLoaded, setChatLoaded] = useState(chatOpen)
  const [chatExpanded, setChatExpanded] = useState(false)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH
  const [openMenuKeys, setOpenMenuKeys] = useState<string[]>(['management', 'plugin-group'])
  const [pluginsLoaded, setPluginsLoaded] = useState(false)
  const [mobileSystemOpen, setMobileSystemOpen] = useState(false)
  const [mobileModuleKey, setMobileModuleKey] = useState<string>()
  const [moduleEnablement, setModuleEnablement] = useState<Record<string, boolean>>()
  const [okrManagementAccess, setOKRManagementAccess] = useState(false)
  const [moduleLoadError, setModuleLoadError] = useState<string>()
  const [enabledPlugins, setEnabledPlugins] = useState<Array<Pick<Plugin, 'id' | 'name' | 'kind' | 'enabled'>>>([])
  const [shuttingDown, setShuttingDown] = useState(false)
  const [editingName, setEditingName] = useState(false)
  const [nameDraft, setNameDraft] = useState(agentName)
  const [savingName, setSavingName] = useState(false)
  const [modal, modalContext] = Modal.useModal()
  const [messageApi, messageContext] = message.useMessage()
  const chatRef = useRef<HTMLElement>(null)
  const chatToggleRef = useRef<HTMLButtonElement>(null)
  const chatWasOpen = useRef(chatOpen)
  const chatResizeCleanupRef = useRef<(() => void) | null>(null)
  const chatWidth = Number.isFinite(storedChatWidth) ? storedChatWidth : DEFAULT_CHAT_PANEL_WIDTH

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
      setOpenMenuKeys((keys) => keys.includes('plugin-group') ? keys : [...keys, 'plugin-group'])
      void refreshPlugins()
    }
    window.addEventListener('jarvis:plugins-changed', onChanged)
    return () => window.removeEventListener('jarvis:plugins-changed', onChanged)
  }, [refreshPlugins])

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

  const menuProps: MenuProps['items'] = [
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
    { key: 'security', label: '安全保护', icon: <SafetyCertificateOutlined /> },
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
    tasks: context.view_state.mode === 'delegated' && context.selection?.kind !== 'task' && delegationsEnabled !== false
      ? delegationsEnabled === null ? <Spin /> : <Delegations />
      : <Tasks delegationsEnabled={delegationsEnabled === true} />,
    'scheduled-tasks': <ScheduledTasks delegationsEnabled={delegationsEnabled === true} />,
    plugins: <Plugins />,
    background: <Background />,
    security: <SecuritySettings />,
    settings: <Settings />,
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
        .then((auth) => setOKRManagementAccess(auth.managementAccess))
        .catch(() => setOKRManagementAccess(false))
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

  useEffect(() => {
    if (!activeModule || activeModuleChildren.length === 0) return
    setOpenMenuKeys((keys) => keys.includes(activeModule.key) ? keys : [...keys, activeModule.key])
  }, [activeModule, activeModuleChildren.length])

  useEffect(() => {
    if (chatOpen) setChatLoaded(true)
  }, [chatOpen])

  useEffect(() => () => chatResizeCleanupRef.current?.(), [])

  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      if (chatExpanded) {
        setChatExpanded(false)
      } else {
        setChatOpen(false)
      }
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [chatExpanded, setChatOpen])

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

  const resizeChatTo = useCallback((width: number) => {
    setStoredChatWidth(clampChatPanelWidth(width, window.innerWidth, siderWidth))
  }, [setStoredChatWidth, siderWidth])

  const startChatResize = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    if (chatExpanded || window.innerWidth < 768) return
    event.preventDefault()
    chatResizeCleanupRef.current?.()
    document.body.classList.add('is-resizing-chat')

    const onPointerMove = (moveEvent: PointerEvent) => {
      setStoredChatWidth(chatPanelWidthFromPointer(moveEvent.clientX, window.innerWidth, siderWidth))
    }
    const finish = () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', finish)
      window.removeEventListener('pointercancel', finish)
      document.body.classList.remove('is-resizing-chat')
      chatResizeCleanupRef.current = null
    }
    chatResizeCleanupRef.current = finish
    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', finish)
    window.addEventListener('pointercancel', finish)
  }, [chatExpanded, setStoredChatWidth, siderWidth])

  const handleChatResizeKeyDown = useCallback((event: React.KeyboardEvent<HTMLDivElement>) => {
    if (chatExpanded) return
    if (event.key === 'ArrowLeft') {
      event.preventDefault()
      resizeChatTo(chatWidth + 24)
    } else if (event.key === 'ArrowRight') {
      event.preventDefault()
      resizeChatTo(chatWidth - 24)
    } else if (event.key === 'Home') {
      event.preventDefault()
      resizeChatTo(MIN_CHAT_PANEL_WIDTH)
    } else if (event.key === 'End') {
      event.preventDefault()
      resizeChatTo(maxChatPanelWidth(window.innerWidth, siderWidth))
    }
  }, [chatExpanded, chatWidth, resizeChatTo, siderWidth])

  const goTo = (key: string) => {
    setMobileSystemOpen(false)
    setMobileModuleKey(undefined)
    const target = moduleNavigationTargets.find((item) => item.menuKey === key)
    if (target) {
      navigate(target.module.key, target.child.viewState)
      return
    }
    if (key.startsWith('plugin:')) {
      navigate('plugins', { plugin: key.slice('plugin:'.length) })
      return
    }
    navigate(key)
  }

  const confirmShutdown = () => {
    modal.confirm({
      title: `退出 ${agentName}？`,
      content: '这会停止当前 Jarvis 实例的主服务和 Chat sidecar，正在执行的任务也会被中断。',
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
      className={`app-shell ${chatOpen ? 'chat-is-open' : ''} ${chatOpen && chatExpanded ? 'chat-is-expanded' : ''}`}
      style={{
        '--sider-width': weeklyShare ? '0px' : `${siderWidth}px`,
        '--chat-width': `${chatWidth}px`,
      } as React.CSSProperties}
    >
      {modalContext}
      {messageContext}
      {!weeklyShare && <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className={`sider-brand ${siderCollapsed ? 'is-collapsed' : ''}`}>
          <img className="sider-brand-icon" src={jarvisIcon} alt={`${agentName} 图标`} />
          {!siderCollapsed && (
            <div className="sider-brand-copy">
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
          openKeys={openMenuKeys}
          onOpenChange={(keys) => setOpenMenuKeys(keys.map(String))}
          items={menuProps}
          onClick={({ key }) => goTo(key)}
          className="app-menu"
        />
        <div className={`sider-help ${siderCollapsed ? 'is-collapsed' : ''}`}>
          <DeveloperHelpButton />
        </div>
        <div className={`sider-footer ${siderCollapsed ? 'is-collapsed' : ''}`}>
          {authEnabled && <div className="sider-account">
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
          </div>}
          <Tooltip title="退出当前实例" placement="right">
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
      </Sider>}
      {!weeklyShare && <header className="mobile-topbar">
        <strong>{currentPageLabel}</strong>
        <Button type="text" icon={<MoreOutlined />} aria-label="打开系统导航" onClick={() => setMobileSystemOpen(true)} />
      </header>}
      <Layout>
        <div className="app-main">
          <Content className="app-content">
            {moduleLoadError && <Alert className="app-module-load-error" type="error" showIcon title="功能模块配置读取失败" description={moduleLoadError} />}
            <Suspense fallback={<div className="page-loading"><Spin size="small" /><span>正在加载…</span></div>}>
              {pages[context.active_key]}
            </Suspense>
          </Content>
          {chatOpen && chatExpanded && (
            <button
              type="button"
              className="chat-modal-backdrop"
              tabIndex={-1}
              aria-label="缩小对话"
              onClick={() => setChatExpanded(false)}
            />
          )}
          <aside
            ref={chatRef}
            className={`chat-overlay ${chatOpen ? 'is-open' : ''} ${chatOpen && chatExpanded ? 'is-expanded' : ''}`}
            aria-hidden={!chatOpen}
            inert={chatOpen ? undefined : true}
            onKeyDown={handleChatKeyDown}
          >
            <div
              className="chat-resize-handle"
              role="separator"
              aria-label="调整对话框宽度"
              aria-orientation="vertical"
              aria-valuemin={MIN_CHAT_PANEL_WIDTH}
              aria-valuenow={Math.round(chatWidth)}
              tabIndex={chatOpen && !chatExpanded ? 0 : -1}
              onPointerDown={startChatResize}
              onKeyDown={handleChatResizeKeyDown}
            />
            {chatLoaded && (
              <Suspense fallback={<div className="page-loading"><Spin size="small" /><span>正在打开对话…</span></div>}>
                <Chat
                  open={chatOpen}
                  expanded={chatExpanded}
                  onToggleExpanded={() => setChatExpanded((expanded) => !expanded)}
                  onClose={() => {
                    setChatExpanded(false)
                    setChatOpen(false)
                  }}
                />
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
          onClick={() => {
            if (chatOpen) setChatExpanded(false)
            setChatOpen((open) => !open)
          }}
        />
      </Tooltip>
      {!weeklyShare && <nav className="mobile-bottom-nav" aria-label="主要导航">
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
          {authEnabled && <div className="mobile-account">
            <UserOutlined />
            <div>
              <strong>{user?.username}</strong>
              <span>{user?.email}</span>
            </div>
          </div>}
          {[
            { key: 'security', label: '安全保护', icon: <SafetyCertificateOutlined /> },
            { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
            { key: 'settings', label: '系统设置', icon: <SettingOutlined /> },
            { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
          ].map((item) => (
            <Button key={item.key} type={context.active_key === item.key ? 'primary' : 'text'} icon={item.icon} onClick={() => goTo(item.key)}>
              {item.label}
            </Button>
          ))}
          {authEnabled && <Button icon={<LogoutOutlined />} onClick={() => void handleLogout()}>退出登录</Button>}
          <DeveloperHelpButton showLabel />
          <Button danger icon={<PoweroffOutlined />} onClick={confirmShutdown}>退出并停止服务</Button>
        </div>
      </Drawer>}
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
      <OnboardingGate>
        <PageContextProvider initialKey={DEFAULT_KEY}>
          <AppShell />
        </PageContextProvider>
      </OnboardingGate>
    </AuthGate>
  )
}
