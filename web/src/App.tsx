import { useCallback, useEffect, useRef, useState } from 'react'
import { Badge, Button, Layout, Menu, Tooltip, Typography } from 'antd'
import type { MenuProps } from 'antd'
import {
  DashboardOutlined,
  CheckCircleOutlined,
  PlayCircleOutlined,
  SettingOutlined,
  BarChartOutlined,
  ToolOutlined,
  MessageOutlined,
  ReloadOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  DatabaseOutlined,
  CalendarOutlined,
} from '@ant-design/icons'
import Confirmations from './Confirmations'
import Tasks from './Tasks'
import Background, { Settings } from './Background'
import Overview from './Overview'
import Progress from './Progress'
import Debug from './Debug'
import Todos from './Todos'
import Chat from './Chat'
import ScheduledTasks from './ScheduledTasks'
import { PageContextProvider, usePageContext } from './pageContext'
import { useLocalStorage } from './hooks/useLocalStorage'
import { useRuntimeFailureCount } from './hooks/useRuntimeFailureCount'

const { Sider, Content } = Layout
const { Title } = Typography

const DEFAULT_KEY = 'overview'

const SIDER_WIDTH = 140
const SIDER_COLLAPSED_WIDTH = 64
const CHAT_MIN_WIDTH = 300
const CHAT_MAX_WIDTH = 720
const CHAT_DEFAULT_WIDTH = 380

interface MenuItem {
  key: string
  label: string
  icon: React.ReactNode
}

const menuItems: MenuItem[] = [
  { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
  { key: 'scheduled-tasks', label: '定时任务', icon: <CalendarOutlined /> },
  { key: 'overview', label: 'Overview', icon: <DashboardOutlined /> },
  { key: 'todos', label: '待办', icon: <CheckCircleOutlined /> },
  { key: 'confirmations', label: '待确认', icon: <CheckCircleOutlined /> },
  { key: 'background', label: '背景', icon: <DatabaseOutlined /> },
  { key: 'settings', label: '设置', icon: <SettingOutlined /> },
  { key: 'progress', label: '进度', icon: <BarChartOutlined /> },
  { key: 'debug', label: '运行状态', icon: <ToolOutlined /> },
]

function AppShell() {
  const { context, navigate } = usePageContext()
  const runtimeFailures = useRuntimeFailureCount()
  const [refreshKey, setRefreshKey] = useState(0)
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOpen', true)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const [chatWidth, setChatWidth] = useLocalStorage('jarvis.chatWidth', CHAT_DEFAULT_WIDTH)
  const [resizing, setResizing] = useState(false)
  const resizingRef = useRef(false)
  const menuProps: MenuProps['items'] = menuItems.map((item) => {
    if (item.key !== 'debug') {
      return { key: item.key, label: item.label, icon: item.icon }
    }
    let icon = item.icon
    if (runtimeFailures.count && runtimeFailures.count > 0) {
      icon = <Badge count={runtimeFailures.count} overflowCount={99} size="small" offset={[6, -4]}>{icon}</Badge>
    } else if (runtimeFailures.error) {
      icon = <Tooltip title={`运行错误读取失败：${runtimeFailures.error}`}><Badge status="error" dot>{icon}</Badge></Tooltip>
    }
    return { key: item.key, label: item.label, icon }
  })

  const pages: Record<string, React.ReactNode> = {
    overview: <Overview />,
    todos: <Todos refreshKey={refreshKey} />,
    confirmations: <Confirmations onDetailOpen={() => setChatOpen(false)} />,
    tasks: <Tasks onDetailOpen={() => setChatOpen(false)} />,
    'scheduled-tasks': <ScheduledTasks />,
    background: <Background />,
    settings: <Settings />,
    progress: <Progress />,
    debug: <Debug />,
  }

  const handleRefresh = useCallback(() => {
    setRefreshKey((value) => value + 1)
  }, [])

  // 拖拽调整对话框宽度：手柄在 chat-dock 左边缘，向左拖变宽。
  // 用全局 mousemove/up 监听，保证拖到手柄外也能持续；宽度夹在 [MIN, MAX]。
  const startResize = useCallback((event: React.MouseEvent) => {
    event.preventDefault()
    resizingRef.current = true
    setResizing(true)

    const onMove = (moveEvent: MouseEvent) => {
      if (!resizingRef.current) return
      const next = window.innerWidth - moveEvent.clientX
      const clamped = Math.min(CHAT_MAX_WIDTH, Math.max(CHAT_MIN_WIDTH, next))
      setChatWidth(clamped)
    }
    const onUp = () => {
      resizingRef.current = false
      setResizing(false)
      window.removeEventListener('mousemove', onMove)
      window.removeEventListener('mouseup', onUp)
    }
    window.addEventListener('mousemove', onMove)
    window.addEventListener('mouseup', onUp)
  }, [setChatWidth])

  // 拖拽期间禁用文本选中，避免选中页面文字。
  useEffect(() => {
    document.body.style.userSelect = resizing ? 'none' : ''
    return () => { document.body.style.userSelect = '' }
  }, [resizing])

  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH

  return (
    <Layout
      className="app-shell"
      style={{ '--sider-width': `${siderWidth}px`, '--chat-width': `${chatWidth}px` } as React.CSSProperties}
    >
      <Sider className="app-sider" width={SIDER_WIDTH} collapsedWidth={SIDER_COLLAPSED_WIDTH} collapsed={siderCollapsed} theme="light">
        <div className="sider-brand">
          {!siderCollapsed && <div className="sider-tagline">Local Work Intelligence</div>}
          <Title level={4}>{siderCollapsed ? 'J' : 'Jarvis'}</Title>
        </div>
        <Menu
          mode="inline"
          inlineCollapsed={siderCollapsed}
          selectedKeys={[context.active_key]}
          items={menuProps}
          onClick={({ key }) => navigate(key)}
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
      <Layout>
        <div className="app-main">
          <Content className="app-content">
            <div className="app-toolbar">
              <Tooltip title="刷新当前页">
                <Button icon={<ReloadOutlined />} onClick={handleRefresh}>刷新</Button>
              </Tooltip>
            </div>
            {pages[context.active_key]}
          </Content>
          <aside className={`chat-dock ${chatOpen ? '' : 'collapsed'} ${resizing ? 'resizing' : ''}`}>
            <div className="chat-resize-handle" onMouseDown={startResize} title="拖拽调整宽度" />
            <Chat />
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
          onClick={() => setChatOpen((open) => !open)}
        />
      </Tooltip>
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
