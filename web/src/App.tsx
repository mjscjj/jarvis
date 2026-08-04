import { useEffect, useState } from 'react'
import { Badge, Button, Layout, Menu, Tooltip, Typography } from 'antd'
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
} from '@ant-design/icons'
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

const SIDER_WIDTH = 184
const SIDER_COLLAPSED_WIDTH = 64
const CHAT_WIDTH = 420

function AppShell() {
  const { context, navigate } = usePageContext()
  const runtimeFailures = useRuntimeFailureCount()
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOverlayOpen', false)
  const [siderCollapsed, setSiderCollapsed] = useLocalStorage('jarvis.siderCollapsed', false)
  const [managementOpen, setManagementOpen] = useState(true)

  let managementIcon: React.ReactNode = <SettingOutlined />
  if (runtimeFailures.count && runtimeFailures.count > 0) {
    managementIcon = <Badge dot status="error">{managementIcon}</Badge>
  } else if (runtimeFailures.error) {
    managementIcon = <Tooltip title={`运行状态读取失败：${runtimeFailures.error}`}><Badge status="error" dot>{managementIcon}</Badge></Tooltip>
  }

  const menuProps: MenuProps['items'] = [
    { key: 'overview', label: '今日', icon: <HomeOutlined /> },
    { key: 'tasks', label: '工作台', icon: <PlayCircleOutlined /> },
    { key: 'progress', label: '回顾', icon: <ReadOutlined /> },
    { key: 'background', label: '记忆', icon: <DatabaseOutlined /> },
    { type: 'divider' },
    {
      key: 'management',
      label: '管理',
      icon: managementIcon,
      children: [
        { key: 'todos', label: '线索', icon: <CheckCircleOutlined /> },
        { key: 'scheduled-tasks', label: '自动化', icon: <CalendarOutlined /> },
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
    settings: <Settings />,
    progress: <Progress />,
    debug: <Debug />,
  }

  useEffect(() => {
    const onEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setChatOpen(false)
    }
    window.addEventListener('keydown', onEscape)
    return () => window.removeEventListener('keydown', onEscape)
  }, [setChatOpen])

  const siderWidth = siderCollapsed ? SIDER_COLLAPSED_WIDTH : SIDER_WIDTH

  return (
    <Layout
      className="app-shell"
      style={{ '--sider-width': `${siderWidth}px`, '--chat-width': `${CHAT_WIDTH}px` } as React.CSSProperties}
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
            {pages[context.active_key]}
          </Content>
          <aside className={`chat-overlay ${chatOpen ? 'is-open' : ''}`} aria-hidden={!chatOpen}>
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
