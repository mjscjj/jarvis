import { useCallback, useState } from 'react'
import { Button, Layout, Menu, Tooltip, Typography } from 'antd'
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
} from '@ant-design/icons'
import Confirmations from './Confirmations'
import Tasks from './Tasks'
import Background from './Background'
import Overview from './Overview'
import Progress from './Progress'
import Debug from './Debug'
import Todos from './Todos'
import Chat from './Chat'
import { PageContextProvider, usePageContext } from './pageContext'
import { useLocalStorage } from './hooks/useLocalStorage'

const { Sider, Content } = Layout
const { Title, Text } = Typography

const DEFAULT_KEY = 'overview'

interface MenuItem {
  key: string
  label: string
  icon: React.ReactNode
}

const menuItems: MenuItem[] = [
  { key: 'overview', label: '工作台', icon: <DashboardOutlined /> },
  { key: 'todos', label: '待办', icon: <CheckCircleOutlined /> },
  { key: 'confirmations', label: '待确认', icon: <CheckCircleOutlined /> },
  { key: 'tasks', label: '任务', icon: <PlayCircleOutlined /> },
  { key: 'background', label: '背景', icon: <SettingOutlined /> },
  { key: 'progress', label: '进度', icon: <BarChartOutlined /> },
  { key: 'debug', label: '调试', icon: <ToolOutlined /> },
]

const menuProps: MenuProps['items'] = menuItems.map((item) => ({
  key: item.key,
  label: item.label,
  icon: item.icon,
}))

function AppShell() {
  const { context, navigate } = usePageContext()
  const [refreshKey, setRefreshKey] = useState(0)
  const [chatOpen, setChatOpen] = useLocalStorage('jarvis.chatOpen', true)

  const pages: Record<string, React.ReactNode> = {
    overview: <Overview />,
    todos: <Todos refreshKey={refreshKey} />,
    confirmations: <Confirmations />,
    tasks: <Tasks />,
    background: <Background />,
    progress: <Progress />,
    debug: <Debug />,
  }

  const handleRefresh = useCallback(() => {
    setRefreshKey((value) => value + 1)
  }, [])

  return (
    <Layout className="app-shell">
      <Sider className="app-sider" width={220} theme="light">
        <div className="sider-brand">
          <div className="sider-tagline">Local Work Intelligence</div>
          <Title level={4}>Jarvis</Title>
        </div>
        <Menu
          mode="inline"
          selectedKeys={[context.active_key]}
          items={menuProps}
          onClick={({ key }) => navigate(key)}
          className="app-menu"
        />
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
          <aside className={`chat-dock ${chatOpen ? '' : 'collapsed'}`}>
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
          className="chat-toggle"
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
