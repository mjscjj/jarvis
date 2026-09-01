import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Alert, Badge, Button, Drawer, Layout, Menu, Spin, Tooltip, Typography } from 'antd'
import type { MenuProps } from 'antd'
import {
  HomeOutlined,
  CheckCircleOutlined,
  PlayCircleOutlined,
  SettingOutlined,
  ReadOutlined,
  ToolOutlined,
  MessageOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  DatabaseOutlined,
  CalendarOutlined,
  MoreOutlined,
  RobotOutlined,
} from '@ant-design/icons'
import Overview from './Overview'
import { PageContextProvider, usePageContext } from './pageContext'
import { useLocalStorage } from './hooks/useLocalStorage'
import { useRuntimeFailureCount } from './hooks/useRuntimeFailureCount'
import { listAppModules } from './api'
import { appModuleRegistry } from './modules/registry'
import type { AppModuleChildDefinition, AppModuleDefinition } from './modules/registry'
import { isWeeklyShareViewState } from './okr/emily/share'

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
): readonly AppModuleChildDefinition[] {
  return module.children?.filter((child) => !child.requiresModule || moduleEnablement[child.requiresModule] === true) ?? []
}

const pageLabels: Record<string, string> = {
  overview: '今日',
  tasks: '任务',
  progress: '回顾',
  background: '世界',
  agents: 'Agent 设置',
  todos: '线索',
  'scheduled-tasks': '自动化',
  settings: '系统设置',
  debug: '运行状态',
  ...Object.fromEntries(appModuleRegistry.map((module) => [module.key, module.label])),
}

function AppShell() {
  const { context, navigate } = usePageContext()
  const weeklyShare = context.active_key === 'okr' && isWeeklyShareViewState(context.view_state)
  const runtimeFailures = useRuntimeFailureCount()
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOverlayOpen', false)
  const [chatLoaded, setChatLoaded] = useState(chatOpen)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const [openMenuKeys, setOpenMenuKeys] = useState<string[]>(['management'])
  const [mobileSystemOpen, setMobileSystemOpen] = useState(false)
  const [mobileModuleKey, setMobileModuleKey] = useState<string>()
  const [moduleEnablement, setModuleEnablement] = useState<Record<string, boolean>>()
  const [moduleLoadError, setModuleLoadError] = useState<string>()
  const chatRef = useRef<HTMLElement>(null)
  const chatToggleRef = useRef<HTMLButtonElement>(null)
  const chatWasOpen = useRef(chatOpen)

  let managementIcon: React.ReactNode = <SettingOutlined />
  if (runtimeFailures.count && runtimeFailures.count > 0) {
    managementIcon = <Badge dot status="error">{managementIcon}</Badge>
  } else if (runtimeFailures.error) {
    managementIcon = <Tooltip title={`运行状态读取失败：${runtimeFailures.error}`}><Badge status="error" dot>{managementIcon}</Badge></Tooltip>
  }

  const enabledModules = appModuleRegistry.filter((module) => moduleEnablement?.[module.key])
  const resolvedModuleEnablement = moduleEnablement ?? EMPTY_MODULE_ENABLEMENT
  const moduleNavigationTargets = enabledModules.flatMap((module) => (
    enabledModuleChildren(module, resolvedModuleEnablement).map((child) => ({
      menuKey: moduleChildMenuKey(module.key, child.key),
      module,
      child,
    }))
  ))
  const activeModule = enabledModules.find((module) => module.key === context.active_key)
  const activeModuleChildren = activeModule ? enabledModuleChildren(activeModule, resolvedModuleEnablement) : []
  const activeModuleChild = activeModuleChildren.find((child) => (
    Object.entries(child.viewState).every(([key, value]) => context.view_state[key] === value)
  )) ?? activeModuleChildren[0]
  const selectedMenuKey = activeModule && activeModuleChild
    ? moduleChildMenuKey(activeModule.key, activeModuleChild.key)
    : context.active_key
  const currentPageLabel = activeModule && activeModuleChild
    ? `${activeModule.label} · ${activeModuleChild.label}`
    : pageLabels[context.active_key] || 'Jarvis'

  const menuProps: MenuProps['items'] = [
    { key: 'overview', label: '今日', icon: <HomeOutlined /> },
    { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
    { key: 'progress', label: '回顾', icon: <ReadOutlined /> },
    ...enabledModules.map((module) => {
      const children = enabledModuleChildren(module, resolvedModuleEnablement)
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
    { key: 'scheduled-tasks', label: '自动化', icon: <CalendarOutlined /> },
    { key: 'agents', label: 'Agent 设置', icon: <RobotOutlined /> },
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
    overview: <Overview />,
    todos: <Todos refreshKey={0} />,
    tasks: <Tasks />,
    'scheduled-tasks': <ScheduledTasks />,
    background: <Background />,
    agents: <AgentSettings />,
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
    setMobileModuleKey(undefined)
    const target = moduleNavigationTargets.find((item) => item.menuKey === key)
    if (target) navigate(target.module.key, target.child.viewState)
    else navigate(key)
  }

  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH
  const mobileModule = enabledModules.find((module) => module.key === mobileModuleKey)
  const mobileModuleChildren = mobileModule ? enabledModuleChildren(mobileModule, resolvedModuleEnablement) : []
  const mobileNavItems: Array<{
    key: string
    label: string
    icon: React.ReactNode
    children?: readonly AppModuleChildDefinition[]
  }> = [
    { key: 'overview', label: '今日', icon: <HomeOutlined /> },
    { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
    { key: 'progress', label: '回顾', icon: <ReadOutlined /> },
    ...enabledModules.map((module) => ({
      key: module.key,
      label: module.label,
      icon: module.icon,
      children: enabledModuleChildren(module, resolvedModuleEnablement),
    })),
    { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
    { key: 'scheduled-tasks', label: '自动化', icon: <CalendarOutlined /> },
    { key: 'agents', label: 'Agent', icon: <RobotOutlined /> },
  ]

  return (
    <Layout
      className={`app-shell ${chatOpen ? 'chat-is-open' : ''}`}
      style={{ '--sider-width': weeklyShare ? '0px' : `${siderWidth}px` } as React.CSSProperties}
    >
      {!weeklyShare && <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className="sider-brand">
          {!siderCollapsed && <div className="sider-tagline">主动式任务分身</div>}
          <Title level={4}>{siderCollapsed ? 'J' : 'Jarvis'}</Title>
        </div>
        <Menu
          mode="inline"
          inlineCollapsed={siderCollapsed}
          selectedKeys={[selectedMenuKey]}
          openKeys={openMenuKeys}
          onOpenChange={(keys) => setOpenMenuKeys(keys.map(String))}
          items={menuProps}
          onClick={({ key }) => goTo(key)}
          className="app-menu"
        />
        <Tooltip title={siderCollapsed ? '展开侧边栏' : '收起侧边栏'} placement="right">
          <Button
            type="text"
            className="sider-collapse-btn"
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
        <div className="app-main">
          <Content className="app-content">
            {moduleLoadError && <Alert className="app-module-load-error" type="error" showIcon title="功能模块配置读取失败" description={moduleLoadError} />}
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
          aria-label={chatOpen ? '关闭 Jarvis 对话' : '打开 Jarvis 对话'}
          onClick={() => setChatOpen((open) => !open)}
        />
      </Tooltip>
      {!weeklyShare && <nav className="mobile-bottom-nav" aria-label="主要导航">
        {mobileNavItems.map((item) => (
          <button
            key={item.key}
            type="button"
            className={context.active_key === item.key ? 'is-active' : ''}
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
          {[
            { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
            { key: 'settings', label: '系统设置', icon: <SettingOutlined /> },
            { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
          ].map((item) => (
            <Button key={item.key} type={context.active_key === item.key ? 'primary' : 'text'} icon={item.icon} onClick={() => goTo(item.key)}>
              {item.label}
            </Button>
          ))}
        </div>
      </Drawer>}
    </Layout>
  )
}

export default function App() {
  return (
    <PageContextProvider initialKey={DEFAULT_KEY}>
      <AppShell />
    </PageContextProvider>
  )
}
