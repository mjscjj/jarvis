import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import { Badge, Button, Drawer, Layout, Menu, Spin, Tooltip, Typography } from 'antd'
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
}

function AppShell() {
  const { context, navigate } = usePageContext()
  const runtimeFailures = useRuntimeFailureCount()
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOverlayOpen', false)
  const [chatLoaded, setChatLoaded] = useState(chatOpen)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const [managementOpen, setManagementOpen] = useState(true)
  const [mobileSystemOpen, setMobileSystemOpen] = useState(false)
  const chatRef = useRef<HTMLElement>(null)
  const chatToggleRef = useRef<HTMLButtonElement>(null)
  const chatWasOpen = useRef(chatOpen)

  let managementIcon: React.ReactNode = <SettingOutlined />
  if (runtimeFailures.count && runtimeFailures.count > 0) {
    managementIcon = <Badge dot status="error">{managementIcon}</Badge>
  } else if (runtimeFailures.error) {
    managementIcon = <Tooltip title={`运行状态读取失败：${runtimeFailures.error}`}><Badge status="error" dot>{managementIcon}</Badge></Tooltip>
  }

  const menuProps: MenuProps['items'] = [
    { key: 'overview', label: '今日', icon: <HomeOutlined /> },
    { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
    { key: 'progress', label: '回顾', icon: <ReadOutlined /> },
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
    navigate(key)
  }

  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH

  return (
    <Layout
      className={`app-shell ${chatOpen ? 'chat-is-open' : ''}`}
      style={{ '--sider-width': `${siderWidth}px` } as React.CSSProperties}
    >
      <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className="sider-brand">
          {!siderCollapsed && <div className="sider-tagline">主动式任务分身</div>}
          <Title level={4}>{siderCollapsed ? 'J' : 'Jarvis'}</Title>
        </div>
        <Menu
          mode="inline"
          inlineCollapsed={siderCollapsed}
          selectedKeys={[context.active_key]}
          openKeys={siderCollapsed || !managementOpen ? [] : ['management']}
          onOpenChange={(keys) => setManagementOpen(keys.includes('management'))}
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
      </Sider>
      <header className="mobile-topbar">
        <strong>{pageLabels[context.active_key] || 'Jarvis'}</strong>
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
          aria-label={chatOpen ? '关闭 Jarvis 对话' : '打开 Jarvis 对话'}
          onClick={() => setChatOpen((open) => !open)}
        />
      </Tooltip>
      <nav className="mobile-bottom-nav" aria-label="主要导航">
        {[
          { key: 'overview', label: '今日', icon: <HomeOutlined /> },
          { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
          { key: 'progress', label: '回顾', icon: <ReadOutlined /> },
          { key: 'background', label: '世界', icon: <DatabaseOutlined /> },
          { key: 'scheduled-tasks', label: '自动化', icon: <CalendarOutlined /> },
          { key: 'agents', label: 'Agent', icon: <RobotOutlined /> },
        ].map((item) => (
          <button key={item.key} type="button" className={context.active_key === item.key ? 'is-active' : ''} onClick={() => goTo(item.key)}>
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
      </Drawer>
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
